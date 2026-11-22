// Package google is the Gmail plugin: the Gmail API with OAuth, labels,
// threads, history based incremental sync, and People for contacts.
package google

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickryan01/halyard/internal/httpx"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() { plugin.Register(mailtypes.KindGoogle, New) }

// Scopes are the full read and write set: mail, contacts and the calendar and
// drive scopes a company install grants alongside.
var Scopes = []string{
	"https://mail.google.com/",
	"https://www.googleapis.com/auth/contacts",
	"https://www.googleapis.com/auth/contacts.other.readonly",
	"openid", "email", "profile",
}

// Client credentials come from the build (ldflags) or the account settings.
var (
	ClientID     = ""
	ClientSecret = ""
)

const (
	AuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL = "https://oauth2.googleapis.com/token"
	allMail  = "__all"
)

func OAuthConfig(settings map[string]string) oauth.Config {
	id, sec := ClientID, ClientSecret
	if v := settings["client_id"]; v != "" {
		id, sec = v, settings["client_secret"]
	}
	return oauth.Config{ClientID: id, ClientSecret: sec, AuthURL: AuthURL, TokenURL: TokenURL, Scopes: Scopes,
		Extra: url.Values{"access_type": {"offline"}, "prompt": {"consent"}}}
}

type Provider struct {
	acct   mailtypes.Account
	api    *httpx.Client
	base   string
	people string
	limit  int

	mu        sync.Mutex
	lastHist  string
	labelRole map[string]string
}

func New(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
	var ts oauth.TokenSource
	switch a.Settings["auth"] {
	case "helper":
		// a command that prints an access token, for managed installs where
		// something else holds the credentials
		cmd := a.Settings["token_command"]
		if cmd == "" {
			return nil, errors.New("google: token_command missing")
		}
		ts = helperSource{cmd}
	default:
		ts = &oauth.Source{Cfg: OAuthConfig(a.Settings), Sec: s}
	}
	return NewWithSource(a, ts), nil
}

// NewWithSource is used by New and by tests with a token from elsewhere.
func NewWithSource(a mailtypes.Account, ts oauth.TokenSource) *Provider {
	p := &Provider{acct: a, api: httpx.New(ts.Token), base: "https://gmail.googleapis.com/gmail/v1/users/me", people: "https://people.googleapis.com/v1", limit: 1500}
	if b := a.Settings["api_base"]; b != "" {
		p.base = b
	}
	if n, err := strconv.Atoi(a.Settings["sync_limit"]); err == nil && n > 0 {
		p.limit = n
	}
	return p
}

type helperSource struct{ cmd string }

func (h helperSource) Token(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "sh", "-c", h.cmd).Output()
	if err != nil {
		return "", fmt.Errorf("token helper failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *Provider) Kind() string { return mailtypes.KindGoogle }
func (p *Provider) Close() error { return nil }

func (p *Provider) Profile(ctx context.Context) (plugin.Profile, error) {
	var r struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/profile", nil, nil, &r); err != nil {
		return plugin.Profile{}, err
	}
	return plugin.Profile{Email: r.EmailAddress}, nil
}

// ---- labels ----

var sysRoles = map[string]string{"INBOX": mailtypes.RoleInbox, "SENT": mailtypes.RoleSent, "DRAFT": mailtypes.RoleDrafts, "TRASH": mailtypes.RoleTrash,
	"SPAM": mailtypes.RoleSpam, "STARRED": mailtypes.RoleStarred}

func (p *Provider) labels(ctx context.Context) ([]mailtypes.Folder, error) {
	var r struct {
		Labels []struct {
			ID, Name, Type string
		} `json:"labels"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/labels", nil, nil, &r); err != nil {
		return nil, err
	}
	var fs []mailtypes.Folder
	for _, l := range r.Labels {
		if role, ok := sysRoles[l.ID]; ok {
			fs = append(fs, mailtypes.Folder{RemoteID: l.ID, Name: l.Name, Role: role, IsLabel: true})
			continue
		}
		if l.Type == "user" {
			fs = append(fs, mailtypes.Folder{RemoteID: l.ID, Name: l.Name, Role: mailtypes.RoleCustom, IsLabel: true})
		}
	}
	fs = append(fs, mailtypes.Folder{RemoteID: allMail, Name: "All Mail", Role: mailtypes.RoleAll, IsLabel: true})
	return fs, nil
}

// ---- messages ----

type gmsg struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	LabelIDs []string `json:"labelIds"`
	Snippet  string   `json:"snippet"`
	History  string   `json:"historyId"`
	Internal string   `json:"internalDate"`
	Payload  *gpart   `json:"payload"`
}

type gpart struct {
	PartID   string                         `json:"partId"`
	MimeType string                         `json:"mimeType"`
	Filename string                         `json:"filename"`
	Headers  []struct{ Name, Value string } `json:"headers"`
	Body     struct {
		AttachmentID string `json:"attachmentId"`
		Size         int64  `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []gpart `json:"parts"`
}

func (g *gmsg) header(n string) string {
	if g.Payload == nil {
		return ""
	}
	for _, h := range g.Payload.Headers {
		if strings.EqualFold(h.Name, n) {
			return h.Value
		}
	}
	return ""
}

func decode(s string) string {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return ""
	}
	return string(b)
}

func walk(pt *gpart, m *mailtypes.Message) {
	switch {
	case pt.Filename != "" || (pt.Body.AttachmentID != "" && !strings.HasPrefix(pt.MimeType, "text/")):
		cid := ""
		inline := false
		for _, h := range pt.Headers {
			if strings.EqualFold(h.Name, "Content-ID") {
				cid = strings.Trim(h.Value, "<> ")
			}
			if strings.EqualFold(h.Name, "Content-Disposition") && strings.HasPrefix(strings.ToLower(h.Value), "inline") {
				inline = true
			}
		}
		name := pt.Filename
		if name == "" {
			name = "attachment"
		}
		m.Attach = append(m.Attach, mailtypes.Attachment{ID: pt.Body.AttachmentID, Filename: name, ContentType: pt.MimeType, Size: pt.Body.Size, Inline: inline && cid != "", ContentID: cid})
	case pt.MimeType == "text/plain" && pt.Body.Data != "":
		m.BodyText += decode(pt.Body.Data)
	case pt.MimeType == "text/html" && pt.Body.Data != "":
		m.BodyHTML += decode(pt.Body.Data)
	}
	for i := range pt.Parts {
		walk(&pt.Parts[i], m)
	}
}

func (p *Provider) toMessage(g *gmsg) mailtypes.Message {
	m := mailtypes.Message{AccountID: p.acct.ID, RemoteID: g.ID, ThreadID: p.acct.ID + "/" + g.ThreadID, Snippet: g.Snippet,
		Subject: g.header("Subject"), MessageID: g.header("Message-ID"), InReplyTo: g.header("In-Reply-To"),
		References: strings.Fields(g.header("References")), Read: true}
	if a, ok := mime.ParseAddress(g.header("From")); ok {
		m.From = a
	}
	m.To = mime.ParseList(g.header("To"))
	m.Cc = mime.ParseList(g.header("Cc"))
	m.Bcc = mime.ParseList(g.header("Bcc"))
	if ms, err := strconv.ParseInt(g.Internal, 10, 64); err == nil {
		m.Date = time.UnixMilli(ms)
	}
	if g.Payload != nil {
		walk(g.Payload, &m)
	}
	m.Folders, m.Read, m.Starred, m.Draft = labelState(g.LabelIDs)
	return m
}

// labelState derives the flags and the folder set from Gmail label IDs. Every
// message not in trash or spam is also in All Mail.
func labelState(ids []string) (folders []string, read, starred, draft bool) {
	read = true
	inTrash := false
	for _, l := range ids {
		switch l {
		case "UNREAD":
			read = false
			continue
		case "STARRED":
			starred = true
		case "DRAFT":
			draft = true
		case "TRASH", "SPAM":
			inTrash = true
		}
		if strings.HasPrefix(l, "CATEGORY_") || l == "IMPORTANT" || l == "CHAT" {
			continue
		}
		folders = append(folders, l)
	}
	if !inTrash {
		folders = append(folders, allMail)
	}
	return
}

func (p *Provider) get(ctx context.Context, id, format string) (*gmsg, error) {
	var g gmsg
	err := p.api.Do(ctx, "GET", p.base+"/messages/"+id+"?format="+format, nil, nil, &g)
	return &g, err
}

// fetchMany gets full messages in parallel, within Gmail's per-user rate.
func (p *Provider) fetchMany(ctx context.Context, ids []string) ([]mailtypes.Message, error) {
	out := make([]*mailtypes.Message, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	var mu sync.Mutex
	var firstErr error
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			g, err := p.get(ctx, id, "full")
			if err != nil {
				if httpx.IsStatus(err, 404) {
					return // deleted while we synced
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			m := p.toMessage(g)
			out[i] = &m
		}(i, id)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	var ms []mailtypes.Message
	for _, m := range out {
		if m != nil {
			ms = append(ms, *m)
		}
	}
	return ms, nil
}

func (p *Provider) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	var res plugin.SyncResult
	fs, err := p.labels(ctx)
	if err != nil {
		return res, err
	}
	res.Change.Folders = fs
	if state != "" {
		ok, err := p.incremental(ctx, state, &res)
		if err != nil {
			return res, err
		}
		if ok {
			return res, nil
		}
		res = plugin.SyncResult{Change: mailtypes.Change{Folders: fs}}
	}
	// full: remember the history ID before listing so nothing is missed between
	var prof struct {
		HistoryID string `json:"historyId"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/profile", nil, nil, &prof); err != nil {
		return res, err
	}
	var ids []string
	page := ""
	for len(ids) < p.limit {
		u := fmt.Sprintf("%s/messages?maxResults=500&includeSpamTrash=true", p.base)
		if page != "" {
			u += "&pageToken=" + url.QueryEscape(page)
		}
		var l struct {
			Messages      []struct{ ID string } `json:"messages"`
			NextPageToken string                `json:"nextPageToken"`
		}
		if err := p.api.Do(ctx, "GET", u, nil, nil, &l); err != nil {
			return res, err
		}
		for _, m := range l.Messages {
			ids = append(ids, m.ID)
		}
		if l.NextPageToken == "" {
			break
		}
		page = l.NextPageToken
	}
	if len(ids) > p.limit {
		ids = ids[:p.limit]
	}
	ms, err := p.fetchMany(ctx, ids)
	if err != nil {
		return res, err
	}
	res.Change.Upsert = ms
	res.State = prof.HistoryID
	res.Full = true
	p.setHist(prof.HistoryID)
	return res, nil
}

func (p *Provider) setHist(h string) {
	p.mu.Lock()
	p.lastHist = h
	p.mu.Unlock()
}

// incremental reads the history since state. It returns false when Gmail no
// longer has that history (404), which means a full resync.
func (p *Provider) incremental(ctx context.Context, state string, res *plugin.SyncResult) (bool, error) {
	changed := map[string]bool{}
	removed := map[string]bool{}
	newest := state
	page := ""
	for {
		u := fmt.Sprintf("%s/history?startHistoryId=%s&maxResults=500", p.base, url.QueryEscape(state))
		if page != "" {
			u += "&pageToken=" + url.QueryEscape(page)
		}
		var h struct {
			History []struct {
				ID              string
				MessagesAdded   []struct{ Message struct{ ID string } } `json:"messagesAdded"`
				MessagesDeleted []struct{ Message struct{ ID string } } `json:"messagesDeleted"`
				LabelsAdded     []struct{ Message struct{ ID string } } `json:"labelsAdded"`
				LabelsRemoved   []struct{ Message struct{ ID string } } `json:"labelsRemoved"`
			} `json:"history"`
			NextPageToken string `json:"nextPageToken"`
			HistoryID     string `json:"historyId"`
		}
		if err := p.api.Do(ctx, "GET", u, nil, nil, &h); err != nil {
			if httpx.IsStatus(err, 404) {
				return false, nil
			}
			return false, err
		}
		for _, e := range h.History {
			for _, a := range e.MessagesAdded {
				changed[a.Message.ID] = true
				delete(removed, a.Message.ID)
			}
			for _, d := range e.MessagesDeleted {
				removed[d.Message.ID] = true
				delete(changed, d.Message.ID)
			}
			for _, a := range e.LabelsAdded {
				if !removed[a.Message.ID] {
					changed[a.Message.ID] = true
				}
			}
			for _, a := range e.LabelsRemoved {
				if !removed[a.Message.ID] {
					changed[a.Message.ID] = true
				}
			}
		}
		if h.HistoryID != "" {
			newest = h.HistoryID
		}
		if h.NextPageToken == "" {
			break
		}
		page = h.NextPageToken
	}
	for id := range removed {
		res.Change.Delete = append(res.Change.Delete, id)
	}
	var ids []string
	for id := range changed {
		ids = append(ids, id)
	}
	// new messages need bodies; label and flag changes only need the labels,
	// but one full fetch per changed message keeps the cache exact and is
	// bounded by what actually changed
	ms, err := p.fetchMany(ctx, ids)
	if err != nil {
		return false, err
	}
	res.Change.Upsert = ms
	res.State = newest
	p.setHist(newest)
	return true, nil
}

// ---- actions ----

func (p *Provider) Apply(ctx context.Context, a mailtypes.Action) error {
	modify := func(add, remove []string) error {
		for i := 0; i < len(a.IDs); i += 1000 {
			end := i + 1000
			if end > len(a.IDs) {
				end = len(a.IDs)
			}
			body := map[string]any{"ids": a.IDs[i:end], "addLabelIds": add, "removeLabelIds": remove}
			if err := p.api.Do(ctx, "POST", p.base+"/messages/batchModify", body, nil, nil); err != nil {
				return err
			}
		}
		return nil
	}
	each := func(suffix, method string) error {
		for _, id := range a.IDs {
			if err := p.api.Do(ctx, method, p.base+"/messages/"+id+suffix, nil, nil, nil); err != nil {
				return err
			}
		}
		return nil
	}
	switch a.Kind {
	case mailtypes.ActRead:
		return modify(nil, []string{"UNREAD"})
	case mailtypes.ActUnread:
		return modify([]string{"UNREAD"}, nil)
	case mailtypes.ActStar:
		return modify([]string{"STARRED"}, nil)
	case mailtypes.ActUnstar:
		return modify(nil, []string{"STARRED"})
	case mailtypes.ActArchive:
		return modify(nil, []string{"INBOX"})
	case mailtypes.ActSpam:
		return modify([]string{"SPAM"}, []string{"INBOX"})
	case mailtypes.ActTrash:
		return each("/trash", "POST")
	case mailtypes.ActUntrash:
		if err := each("/untrash", "POST"); err != nil {
			return err
		}
		return modify([]string{"INBOX"}, nil)
	case mailtypes.ActLabel:
		return modify([]string{a.FolderID}, nil)
	case mailtypes.ActUnlabel:
		return modify(nil, []string{a.FolderID})
	case mailtypes.ActMove:
		return modify([]string{a.FolderID}, []string{"INBOX"})
	case mailtypes.ActDelete:
		return each("", "DELETE")
	}
	return fmt.Errorf("google: unknown action %q", a.Kind)
}

func (p *Provider) Send(ctx context.Context, o mailtypes.Outgoing) error {
	domain := "gmail.com"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.Build(o, mime.MessageID(domain, "g"))
	if err != nil {
		return err
	}
	body := map[string]any{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	if o.ThreadHint != "" {
		body["threadId"] = o.ThreadHint
	}
	return p.api.Do(ctx, "POST", p.base+"/messages/send", body, nil, nil)
}

func (p *Provider) Attachment(ctx context.Context, msgID, attID string) ([]byte, string, error) {
	var r struct {
		Data string `json:"data"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/messages/"+msgID+"/attachments/"+attID, nil, nil, &r); err != nil {
		return nil, "", err
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(r.Data, "="))
	return b, "", err
}

// ---- contacts ----

func (p *Provider) Contacts(ctx context.Context, q string) ([]mailtypes.Contact, error) {
	var out []mailtypes.Contact
	if q != "" {
		var r struct {
			Results []struct {
				Person person `json:"person"`
			} `json:"results"`
		}
		// People asks for an empty query first so its cache is warm
		_ = p.api.Do(ctx, "GET", p.people+"/people:searchContacts?query=&readMask=names,emailAddresses", nil, nil, nil)
		u := p.people + "/people:searchContacts?pageSize=10&readMask=names,emailAddresses&query=" + url.QueryEscape(q)
		if err := p.api.Do(ctx, "GET", u, nil, nil, &r); err != nil {
			return nil, err
		}
		for _, x := range r.Results {
			out = append(out, x.Person.contacts()...)
		}
		return out, nil
	}
	page := ""
	for len(out) < 3000 {
		u := p.people + "/people/me/connections?pageSize=500&personFields=names,emailAddresses"
		if page != "" {
			u += "&pageToken=" + url.QueryEscape(page)
		}
		var r struct {
			Connections   []person `json:"connections"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := p.api.Do(ctx, "GET", u, nil, nil, &r); err != nil {
			return out, err
		}
		for _, c := range r.Connections {
			out = append(out, c.contacts()...)
		}
		if r.NextPageToken == "" {
			break
		}
		page = r.NextPageToken
	}
	return out, nil
}

type person struct {
	ResourceName string `json:"resourceName"`
	Names        []struct {
		DisplayName string `json:"displayName"`
	} `json:"names"`
	Emails []struct {
		Value string `json:"value"`
	} `json:"emailAddresses"`
}

func (p person) contacts() []mailtypes.Contact {
	name := ""
	if len(p.Names) > 0 {
		name = p.Names[0].DisplayName
	}
	var out []mailtypes.Contact
	for _, e := range p.Emails {
		out = append(out, mailtypes.Contact{ID: p.ResourceName, Name: name, Email: e.Value})
	}
	return out
}

// ---- watch ----

// Watch polls the mailbox history ID every 15 seconds. Gmail's push channel
// is Cloud Pub/Sub, which needs a server reachable from Google, so a desktop
// client cannot hold one; the history ID check is one cheap call and notices
// new mail within seconds.
func (p *Provider) Watch(ctx context.Context, notify func()) error {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			var prof struct {
				HistoryID string `json:"historyId"`
			}
			if err := p.api.Do(ctx, "GET", p.base+"/profile", nil, nil, &prof); err != nil {
				return err
			}
			p.mu.Lock()
			changed := prof.HistoryID != p.lastHist && p.lastHist != ""
			p.mu.Unlock()
			if changed {
				notify()
			}
		}
	}
}

// CreateFolder makes a user label.
func (p *Provider) CreateFolder(ctx context.Context, name, role string) (string, error) {
	var r struct {
		ID string `json:"id"`
	}
	err := p.api.Do(ctx, "POST", p.base+"/labels", map[string]any{"name": name, "labelListVisibility": "labelShow", "messageListVisibility": "show"}, nil, &r)
	return r.ID, err
}

func personBody(c mailtypes.Contact) map[string]any {
	b := map[string]any{"names": []map[string]string{{"givenName": c.Name}}}
	if c.Email != "" {
		b["emailAddresses"] = []map[string]string{{"value": c.Email}}
	}
	if c.Phone != "" {
		b["phoneNumbers"] = []map[string]string{{"value": c.Phone}}
	}
	if c.Org != "" {
		b["organizations"] = []map[string]string{{"name": c.Org}}
	}
	return b
}

type fullPerson struct {
	ResourceName string `json:"resourceName"`
	Etag         string `json:"etag"`
	person
}

// CreateContact adds to the person's Google contacts.
func (p *Provider) CreateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	var r fullPerson
	if err := p.api.Do(ctx, "POST", p.people+"/people:createContact?personFields=names,emailAddresses", personBody(c), nil, &r); err != nil {
		return c, err
	}
	c.ID = r.ResourceName
	return c, nil
}

// UpdateContact replaces a contact's name, email, phone and organization.
func (p *Provider) UpdateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	var cur fullPerson
	if err := p.api.Do(ctx, "GET", p.people+"/"+c.ID+"?personFields=names", nil, nil, &cur); err != nil {
		return c, err
	}
	b := personBody(c)
	b["etag"] = cur.Etag
	err := p.api.Do(ctx, "PATCH", p.people+"/"+c.ID+":updateContact?updatePersonFields=names,emailAddresses,phoneNumbers,organizations", b, nil, nil)
	return c, err
}

func (p *Provider) DeleteContact(ctx context.Context, id string) error {
	return p.api.Do(ctx, "DELETE", p.people+"/"+id+":deleteContact", nil, nil, nil)
}
