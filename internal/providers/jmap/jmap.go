// Package jmap is the JMAP plugin (RFC 8620 and 8621) for self-hosted servers
// that have it, such as Stalwart, Cyrus and Fastmail. It syncs by state
// string, so a pass costs one round trip when nothing changed, and it listens
// on the server's EventSource stream instead of polling.
package jmap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() { plugin.Register(mailtypes.KindJMAP, New) }

const (
	capCore = "urn:ietf:params:jmap:core"
	capMail = "urn:ietf:params:jmap:mail"
	capSub  = "urn:ietf:params:jmap:submission"
)

type Provider struct {
	acct  mailtypes.Account
	sec   plugin.Secrets
	hc    *http.Client
	base  string
	sess  *session
	limit int
	boxes map[string]mailtypes.Folder
}

type session struct {
	APIURL          string            `json:"apiUrl"`
	DownloadURL     string            `json:"downloadUrl"`
	UploadURL       string            `json:"uploadUrl"`
	EventSourceURL  string            `json:"eventSourceUrl"`
	PrimaryAccounts map[string]string `json:"primaryAccounts"`
}

func New(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
	u := a.Settings["jmap_url"]
	if u == "" {
		return nil, errors.New("jmap: server URL missing")
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: a.Settings["insecure_skip_verify"] == "true", MinVersion: tls.VersionTLS12}}
	return &Provider{acct: a, sec: s, base: strings.TrimRight(u, "/"), limit: 3000,
		hc: &http.Client{Transport: tr, Timeout: 60 * time.Second}}, nil
}

func (p *Provider) Kind() string { return mailtypes.KindJMAP }
func (p *Provider) Close() error { return nil }

func (p *Provider) auth(r *http.Request) error {
	b, err := p.sec.Get()
	if err != nil {
		return err
	}
	if p.acct.Settings["auth"] == "bearer" {
		r.Header.Set("Authorization", "Bearer "+string(b))
	} else {
		user := p.acct.Settings["username"]
		if user == "" {
			user = p.acct.Email
		}
		r.SetBasicAuth(user, string(b))
	}
	return nil
}

func (p *Provider) session(ctx context.Context) (*session, error) {
	if p.sess != nil {
		return p.sess, nil
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", p.base+"/.well-known/jmap", nil)
	if err := p.auth(req); err != nil {
		return nil, err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jmap session: http %d", resp.StatusCode)
	}
	var s session
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	if s.PrimaryAccounts[capMail] == "" {
		return nil, errors.New("jmap: server has no mail capability")
	}
	p.pinHost(&s)
	p.sess = &s
	return &s, nil
}

func (p *Provider) accountID(ctx context.Context) (string, error) {
	s, err := p.session(ctx)
	if err != nil {
		return "", err
	}
	return s.PrimaryAccounts[capMail], nil
}

type call = []any

type response struct {
	MethodResponses [][]json.RawMessage `json:"methodResponses"`
}

// do sends method calls and returns each response's arguments by call ID.
func (p *Provider) do(ctx context.Context, using []string, calls ...call) (map[string]json.RawMessage, error) {
	s, err := p.session(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"using": using, "methodCalls": calls})
	req, _ := http.NewRequestWithContext(ctx, "POST", s.APIURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if err := p.auth(req); err != nil {
		return nil, err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jmap: http %d: %s", resp.StatusCode, trunc(string(rb), 200))
	}
	var r response
	if err := json.Unmarshal(rb, &r); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, mr := range r.MethodResponses {
		if len(mr) != 3 {
			continue
		}
		var name, id string
		_ = json.Unmarshal(mr[0], &name)
		_ = json.Unmarshal(mr[2], &id)
		if name == "error" {
			return nil, fmt.Errorf("jmap error: %s", trunc(string(mr[1]), 300))
		}
		out[id] = mr[1]
	}
	return out, nil
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var mailUse = []string{capCore, capMail}

func (p *Provider) Profile(ctx context.Context) (plugin.Profile, error) {
	_, err := p.session(ctx)
	return plugin.Profile{Email: p.acct.Email, Name: p.acct.Name}, err
}

// ---- mailboxes ----

var roleMap = map[string]string{"inbox": mailtypes.RoleInbox, "sent": mailtypes.RoleSent, "drafts": mailtypes.RoleDrafts, "trash": mailtypes.RoleTrash,
	"junk": mailtypes.RoleSpam, "archive": mailtypes.RoleArchive, "all": mailtypes.RoleAll, "flagged": mailtypes.RoleStarred}

type mailbox struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
	Role     *string `json:"role"`
}

func (p *Provider) mailboxes(ctx context.Context, acct string) ([]mailtypes.Folder, error) {
	r, err := p.do(ctx, mailUse, call{"Mailbox/get", map[string]any{"accountId": acct}, "m"})
	if err != nil {
		return nil, err
	}
	var g struct {
		List []mailbox `json:"list"`
	}
	if err := json.Unmarshal(r["m"], &g); err != nil {
		return nil, err
	}
	byID := map[string]mailbox{}
	for _, m := range g.List {
		byID[m.ID] = m
	}
	var fs []mailtypes.Folder
	p.boxes = map[string]mailtypes.Folder{}
	for _, m := range g.List {
		name := m.Name
		for pid := m.ParentID; pid != nil; {
			par := byID[*pid]
			name = par.Name + "/" + name
			pid = par.ParentID
		}
		role := mailtypes.RoleCustom
		if m.Role != nil {
			if r, ok := roleMap[*m.Role]; ok {
				role = r
			}
		}
		f := mailtypes.Folder{RemoteID: m.ID, Name: name, Role: role}
		fs = append(fs, f)
		p.boxes[m.ID] = f
	}
	return fs, nil
}

func (p *Provider) roleID(role string) string {
	for id, f := range p.boxes {
		if f.Role == role {
			return id
		}
	}
	return ""
}

// ---- email ----

var emailProps = []string{"id", "threadId", "mailboxIds", "keywords", "receivedAt", "subject", "from", "to", "cc", "bcc", "replyTo",
	"messageId", "inReplyTo", "references", "preview", "textBody", "htmlBody", "bodyValues", "attachments", "hasAttachment", "size",
	"header:List-Unsubscribe:asURLs", "header:List-Unsubscribe-Post:asRaw", "header:Disposition-Notification-To:asAddresses"}

type jaddr struct {
	Name  *string
	Email string
}

type jemail struct {
	ID                         string          `json:"id"`
	ThreadID                   string          `json:"threadId"`
	MailboxIDs                 map[string]bool `json:"mailboxIds"`
	Keywords                   map[string]bool `json:"keywords"`
	ReceivedAt                 time.Time       `json:"receivedAt"`
	Subject                    string          `json:"subject"`
	From, To, Cc, Bcc, ReplyTo []jaddr
	MessageID                  []string `json:"messageId"`
	InReplyTo                  []string `json:"inReplyTo"`
	References                 []string `json:"references"`
	Preview                    string   `json:"preview"`
	Size                       int64    `json:"size"`
	Unsub                      []string `json:"header:List-Unsubscribe:asURLs"`
	UnsubPost                  *string  `json:"header:List-Unsubscribe-Post:asRaw"`
	Receipt                    []jaddr  `json:"header:Disposition-Notification-To:asAddresses"`
	TextBody                   []struct {
		PartID string `json:"partId"`
	} `json:"textBody"`
	HTMLBody []struct {
		PartID string `json:"partId"`
	} `json:"htmlBody"`
	BodyValues map[string]struct {
		Value string `json:"value"`
	} `json:"bodyValues"`
	Attachments []struct {
		BlobID, Name, Type, CID string
		Size                    int64
		Disposition             string
	} `json:"attachments"`
}

func conv(a []jaddr) []mailtypes.Address {
	var o []mailtypes.Address
	for _, x := range a {
		n := ""
		if x.Name != nil {
			n = *x.Name
		}
		o = append(o, mailtypes.Address{Name: n, Email: x.Email})
	}
	return o
}

func (p *Provider) toMessage(e jemail) mailtypes.Message {
	m := mailtypes.Message{AccountID: p.acct.ID, RemoteID: e.ID, ThreadID: p.acct.ID + "/" + e.ThreadID, Subject: e.Subject,
		From: mailtypes.Address{}, To: conv(e.To), Cc: conv(e.Cc), Bcc: conv(e.Bcc), ReplyTo: conv(e.ReplyTo), Date: e.ReceivedAt, Snippet: e.Preview,
		Read: e.Keywords["$seen"], Starred: e.Keywords["$flagged"], Draft: e.Keywords["$draft"]}
	if f := conv(e.From); len(f) > 0 {
		m.From = f[0]
	}
	m.Size = e.Size
	if len(e.Unsub) > 0 {
		var parts []string
		for _, u := range e.Unsub {
			parts = append(parts, "<"+strings.Trim(strings.TrimSpace(u), "<>")+">")
		}
		m.ListUnsubscribe = strings.Join(parts, ", ")
	}
	if e.UnsubPost != nil {
		m.ListUnsubscribePost = strings.Contains(strings.ToLower(*e.UnsubPost), "one-click")
	}
	if r := conv(e.Receipt); len(r) > 0 {
		m.ReceiptTo = r[0].Email
	}
	if len(e.MessageID) > 0 {
		m.MessageID = "<" + strings.Trim(e.MessageID[0], "<>") + ">"
	}
	if len(e.InReplyTo) > 0 {
		m.InReplyTo = "<" + strings.Trim(e.InReplyTo[0], "<>") + ">"
	}
	for _, r := range e.References {
		m.References = append(m.References, "<"+strings.Trim(r, "<>")+">")
	}
	for _, b := range e.TextBody {
		m.BodyText += e.BodyValues[b.PartID].Value
	}
	for _, b := range e.HTMLBody {
		m.BodyHTML += e.BodyValues[b.PartID].Value
	}
	// when the only html part is the text part, there is no separate html
	if len(e.HTMLBody) == 1 && len(e.TextBody) == 1 && e.HTMLBody[0].PartID == e.TextBody[0].PartID {
		m.BodyHTML = ""
	}
	for _, a := range e.Attachments {
		name := a.Name
		if name == "" {
			name = "attachment"
		}
		m.Attach = append(m.Attach, mailtypes.Attachment{ID: a.BlobID, Filename: name, ContentType: a.Type, Size: a.Size, Inline: a.Disposition == "inline" && a.CID != "", ContentID: a.CID})
	}
	for id := range e.MailboxIDs {
		m.Folders = append(m.Folders, id)
	}
	return m
}

func (p *Provider) getEmails(ctx context.Context, acct string, ids []string) ([]mailtypes.Message, error) {
	var out []mailtypes.Message
	for i := 0; i < len(ids); i += 50 {
		end := i + 50
		if end > len(ids) {
			end = len(ids)
		}
		r, err := p.do(ctx, mailUse, call{"Email/get", map[string]any{"accountId": acct, "ids": ids[i:end], "properties": emailProps,
			"fetchTextBodyValues": true, "fetchHTMLBodyValues": true, "maxBodyValueBytes": 4 << 20}, "g"})
		if err != nil {
			return nil, err
		}
		var g struct {
			List []jemail `json:"list"`
		}
		if err := json.Unmarshal(r["g"], &g); err != nil {
			return nil, err
		}
		for _, e := range g.List {
			out = append(out, p.toMessage(e))
		}
	}
	return out, nil
}

func (p *Provider) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	var res plugin.SyncResult
	acct, err := p.accountID(ctx)
	if err != nil {
		return res, err
	}
	fs, err := p.mailboxes(ctx, acct)
	if err != nil {
		return res, err
	}
	res.Change.Folders = fs
	if state != "" {
		r, err := p.do(ctx, mailUse, call{"Email/changes", map[string]any{"accountId": acct, "sinceState": state, "maxChanges": 500}, "c"})
		if err == nil {
			var c struct {
				NewState                    string `json:"newState"`
				HasMore                     bool   `json:"hasMoreChanges"`
				Created, Updated, Destroyed []string
			}
			if err := json.Unmarshal(r["c"], &c); err != nil {
				return res, err
			}
			ms, err := p.getEmails(ctx, acct, append(c.Created, c.Updated...))
			if err != nil {
				return res, err
			}
			res.Change.Upsert = ms
			res.Change.Delete = c.Destroyed
			res.State = c.NewState
			if c.HasMore {
				// the engine calls Sync again at once because State moved on
				return res, nil
			}
			return res, nil
		}
		if !strings.Contains(err.Error(), "cannotCalculateChanges") {
			return res, err
		}
		// state too old: fall through to a full pass
	}
	r, err := p.do(ctx, mailUse, call{"Email/query", map[string]any{"accountId": acct, "sort": []map[string]any{{"property": "receivedAt", "isAscending": false}}, "limit": p.limit}, "q"})
	if err != nil {
		return res, err
	}
	var q struct {
		IDs        []string `json:"ids"`
		QueryState string   `json:"queryState"`
	}
	if err := json.Unmarshal(r["q"], &q); err != nil {
		return res, err
	}
	// the Email state to continue from, taken before the fetch
	sr, err := p.do(ctx, mailUse, call{"Email/get", map[string]any{"accountId": acct, "ids": []string{}, "properties": []string{"id"}}, "s"})
	if err != nil {
		return res, err
	}
	var st struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(sr["s"], &st)
	ms, err := p.getEmails(ctx, acct, q.IDs)
	if err != nil {
		return res, err
	}
	res.Change.Upsert = ms
	res.State = st.State
	res.Full = true
	return res, nil
}

func (p *Provider) Apply(ctx context.Context, a mailtypes.Action) error {
	acct, err := p.accountID(ctx)
	if err != nil {
		return err
	}
	if p.boxes == nil {
		if _, err := p.mailboxes(ctx, acct); err != nil {
			return err
		}
	}
	update := map[string]any{}
	destroy := []string{}
	for _, id := range a.IDs {
		u := map[string]any{}
		moveTo := func(role string) error {
			dest := p.roleID(role)
			if dest == "" {
				return fmt.Errorf("jmap: no %s mailbox", role)
			}
			u["mailboxIds"] = map[string]bool{dest: true}
			return nil
		}
		switch a.Kind {
		case mailtypes.ActRead:
			u["keywords/$seen"] = true
		case mailtypes.ActUnread:
			u["keywords/$seen"] = nil
		case mailtypes.ActStar:
			u["keywords/$flagged"] = true
		case mailtypes.ActUnstar:
			u["keywords/$flagged"] = nil
		case mailtypes.ActArchive:
			dest, err := p.archiveBox(ctx)
			if err != nil {
				return err
			}
			u["mailboxIds"] = map[string]bool{dest: true}
		case mailtypes.ActTrash:
			if err := moveTo(mailtypes.RoleTrash); err != nil {
				return err
			}
		case mailtypes.ActSpam:
			if err := moveTo(mailtypes.RoleSpam); err != nil {
				return err
			}
		case mailtypes.ActUntrash:
			if err := moveTo(mailtypes.RoleInbox); err != nil {
				return err
			}
		case mailtypes.ActMove:
			u["mailboxIds"] = map[string]bool{a.FolderID: true}
		case mailtypes.ActLabel:
			u["mailboxIds/"+a.FolderID] = true
		case mailtypes.ActUnlabel:
			u["mailboxIds/"+a.FolderID] = nil
		case mailtypes.ActDelete:
			destroy = append(destroy, id)
			continue
		default:
			return fmt.Errorf("jmap: unknown action %q", a.Kind)
		}
		update[id] = u
	}
	args := map[string]any{"accountId": acct}
	if len(update) > 0 {
		args["update"] = update
	}
	if len(destroy) > 0 {
		args["destroy"] = destroy
	}
	r, err := p.do(ctx, mailUse, call{"Email/set", args, "s"})
	if err != nil {
		return err
	}
	var s struct {
		NotUpdated   map[string]json.RawMessage `json:"notUpdated"`
		NotDestroyed map[string]json.RawMessage `json:"notDestroyed"`
	}
	_ = json.Unmarshal(r["s"], &s)
	if len(s.NotUpdated)+len(s.NotDestroyed) > 0 {
		return fmt.Errorf("jmap: server refused %d changes", len(s.NotUpdated)+len(s.NotDestroyed))
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, o mailtypes.Outgoing) error {
	acct, err := p.accountID(ctx)
	if err != nil {
		return err
	}
	s, _ := p.session(ctx)
	if p.boxes == nil {
		if _, err := p.mailboxes(ctx, acct); err != nil {
			return err
		}
	}
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.Build(o, mime.MessageID(domain, "j"))
	if err != nil {
		return err
	}
	// upload the message as a blob
	up := strings.ReplaceAll(s.UploadURL, "{accountId}", url.PathEscape(acct))
	req, _ := http.NewRequestWithContext(ctx, "POST", up, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "message/rfc822")
	if err := p.auth(req); err != nil {
		return err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	var blob struct {
		BlobID string `json:"blobId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&blob)
	resp.Body.Close()
	if resp.StatusCode >= 300 || blob.BlobID == "" {
		return fmt.Errorf("jmap upload: http %d", resp.StatusCode)
	}
	drafts, sent := p.roleID(mailtypes.RoleDrafts), p.roleID(mailtypes.RoleSent)
	if drafts == "" {
		return errors.New("jmap: no drafts mailbox")
	}
	// identity for the submission
	ir, err := p.do(ctx, []string{capCore, capMail, capSub}, call{"Identity/get", map[string]any{"accountId": acct}, "i"})
	if err != nil {
		return err
	}
	var ids struct {
		List []struct{ ID, Email string } `json:"list"`
	}
	_ = json.Unmarshal(ir["i"], &ids)
	if len(ids.List) == 0 {
		return errors.New("jmap: no sending identity")
	}
	ident := ids.List[0].ID
	for _, i := range ids.List {
		if strings.EqualFold(i.Email, o.From.Email) {
			ident = i.ID
		}
	}
	var rcpt []map[string]string
	for _, e := range mime.Envelope(o) {
		rcpt = append(rcpt, map[string]string{"email": e})
	}
	upd := map[string]any{"keywords/$draft": nil, "keywords/$seen": true, "mailboxIds/" + drafts: nil}
	if sent != "" {
		upd["mailboxIds/"+sent] = true
	}
	r, err := p.do(ctx, []string{capCore, capMail, capSub},
		call{"Email/import", map[string]any{"accountId": acct, "emails": map[string]any{"e1": map[string]any{"blobId": blob.BlobID, "mailboxIds": map[string]bool{drafts: true},
			"keywords": map[string]bool{"$draft": true, "$seen": true}}}}, "imp"},
		call{"EmailSubmission/set", map[string]any{"accountId": acct, "create": map[string]any{"s1": map[string]any{"identityId": ident, "emailId": "#e1",
			"envelope": map[string]any{"mailFrom": map[string]string{"email": o.From.Email}, "rcptTo": rcpt}}},
			"onSuccessUpdateEmail": map[string]any{"#s1": upd}}, "sub"})
	if err != nil {
		return err
	}
	var sub struct {
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	_ = json.Unmarshal(r["sub"], &sub)
	if len(sub.NotCreated) > 0 {
		return fmt.Errorf("jmap submission refused: %s", trunc(string(sub.NotCreated["s1"]), 300))
	}
	var imp struct {
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	_ = json.Unmarshal(r["imp"], &imp)
	if len(imp.NotCreated) > 0 {
		return fmt.Errorf("jmap import refused: %s", trunc(string(imp.NotCreated["e1"]), 300))
	}
	return nil
}

func (p *Provider) Attachment(ctx context.Context, msgID, blobID string) ([]byte, string, error) {
	acct, err := p.accountID(ctx)
	if err != nil {
		return nil, "", err
	}
	s, _ := p.session(ctx)
	u := s.DownloadURL
	u = strings.NewReplacer("{accountId}", url.PathEscape(acct), "{blobId}", url.PathEscape(blobID), "{name}", "attachment", "{type}", url.QueryEscape("application/octet-stream")).Replace(u)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err := p.auth(req); err != nil {
		return nil, "", err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("jmap download: http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header.Get("Content-Type"), err
}

func (p *Provider) Contacts(ctx context.Context, q string) ([]mailtypes.Contact, error) {
	return nil, nil
}

// Watch holds the EventSource stream the server offers and calls notify on
// each state change.
func (p *Provider) Watch(ctx context.Context, notify func()) error {
	s, err := p.session(ctx)
	if err != nil {
		return err
	}
	if s.EventSourceURL == "" {
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				notify()
			}
		}
	}
	u := strings.NewReplacer("{types}", "Email,Mailbox", "{closeafter}", "no", "{ping}", "30").Replace(s.EventSourceURL)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Accept", "text/event-stream")
	if err := p.auth(req); err != nil {
		return err
	}
	hc := &http.Client{Transport: p.hc.Transport} // no overall timeout on a stream
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("jmap eventsource: http %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case line == "":
			if event == "state" {
				notify()
			}
			event = ""
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}

// pinHost keeps the session's URLs on the address the person typed. Servers
// behind a proxy, a port forward or a lab network often advertise their own
// internal name in the session, which this machine cannot reach. The paths are
// the server's; the host and port are the ones that just answered. Set
// trust_session_urls to "true" to use the advertised URLs as they are.
func (p *Provider) pinHost(s *session) {
	if p.acct.Settings["trust_session_urls"] == "true" {
		return
	}
	b, err := url.Parse(p.base)
	if err != nil {
		return
	}
	fix := func(u string) string {
		x, err := url.Parse(strings.NewReplacer("{", "%7B", "}", "%7D").Replace(u))
		if err != nil || x.Host == "" {
			return u
		}
		x.Scheme, x.Host = b.Scheme, b.Host
		return strings.NewReplacer("%7B", "{", "%7D", "}").Replace(x.String())
	}
	s.APIURL, s.DownloadURL, s.UploadURL, s.EventSourceURL = fix(s.APIURL), fix(s.DownloadURL), fix(s.UploadURL), fix(s.EventSourceURL)
}

// CreateFolder makes a mailbox. role is one of the standard roles or custom.
func (p *Provider) CreateFolder(ctx context.Context, name, role string) (string, error) {
	acct, err := p.accountID(ctx)
	if err != nil {
		return "", err
	}
	c := map[string]any{"name": name}
	for k, v := range roleMap {
		if v == role && role != mailtypes.RoleCustom {
			c["role"] = k
		}
	}
	r, err := p.do(ctx, mailUse, call{"Mailbox/set", map[string]any{"accountId": acct, "create": map[string]any{"n": c}}, "c"})
	if err != nil {
		return "", err
	}
	var out struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	_ = json.Unmarshal(r["c"], &out)
	if out.Created["n"].ID == "" {
		var ne struct {
			ExistingID string `json:"existingId"`
		}
		if json.Unmarshal(out.NotCreated["n"], &ne) == nil && ne.ExistingID != "" {
			return ne.ExistingID, nil
		}
		return "", fmt.Errorf("jmap: could not create mailbox: %s", trunc(string(out.NotCreated["n"]), 200))
	}
	if _, err := p.mailboxes(ctx, acct); err != nil {
		return "", err
	}
	return out.Created["n"].ID, nil
}

// archiveBox finds the mailbox archived mail goes to: the one with the archive
// role, else one named Archive, else a new one. A message must stay in at
// least one mailbox, so there always has to be somewhere to move it.
func (p *Provider) archiveBox(ctx context.Context) (string, error) {
	if id := p.roleID(mailtypes.RoleArchive); id != "" {
		return id, nil
	}
	for id, f := range p.boxes {
		if strings.EqualFold(f.Name, "Archive") {
			return id, nil
		}
	}
	return p.CreateFolder(ctx, "Archive", mailtypes.RoleArchive)
}

// ---- drafts ----

// SaveDraft imports the draft into the Drafts mailbox with the $draft keyword
// and destroys the version it replaces.
func (p *Provider) SaveDraft(ctx context.Context, o mailtypes.Outgoing, replaceID string) (string, error) {
	acct, err := p.accountID(ctx)
	if err != nil {
		return "", err
	}
	s, _ := p.session(ctx)
	if p.boxes == nil {
		if _, err := p.mailboxes(ctx, acct); err != nil {
			return "", err
		}
	}
	drafts := p.roleID(mailtypes.RoleDrafts)
	if drafts == "" {
		if drafts, err = p.CreateFolder(ctx, "Drafts", mailtypes.RoleDrafts); err != nil {
			return "", err
		}
	}
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.BuildDraft(o, mime.MessageID(domain, "d"))
	if err != nil {
		return "", err
	}
	up := strings.ReplaceAll(s.UploadURL, "{accountId}", url.PathEscape(acct))
	req, _ := http.NewRequestWithContext(ctx, "POST", up, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "message/rfc822")
	if err := p.auth(req); err != nil {
		return "", err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return "", err
	}
	var blob struct {
		BlobID string `json:"blobId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&blob)
	resp.Body.Close()
	if resp.StatusCode >= 300 || blob.BlobID == "" {
		return "", fmt.Errorf("jmap upload: http %d", resp.StatusCode)
	}
	r, err := p.do(ctx, mailUse, call{"Email/import", map[string]any{"accountId": acct, "emails": map[string]any{"e1": map[string]any{"blobId": blob.BlobID,
		"mailboxIds": map[string]bool{drafts: true}, "keywords": map[string]bool{"$draft": true, "$seen": true}}}}, "imp"})
	if err != nil {
		return "", err
	}
	var imp struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	_ = json.Unmarshal(r["imp"], &imp)
	id := imp.Created["e1"].ID
	if id == "" {
		return "", fmt.Errorf("jmap draft refused: %s", trunc(string(imp.NotCreated["e1"]), 300))
	}
	if replaceID != "" {
		_ = p.DeleteDraft(ctx, replaceID)
	}
	return id, nil
}

func (p *Provider) DeleteDraft(ctx context.Context, id string) error {
	return p.Apply(ctx, mailtypes.Action{Kind: mailtypes.ActDelete, IDs: []string{id}})
}

func (p *Provider) DraftID(ctx context.Context, messageID string) (string, error) {
	return messageID, nil
}
