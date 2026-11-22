// Package microsoft is the Microsoft 365 and Outlook.com plugin: Microsoft
// Graph with OAuth, mail folders, delta sync per folder, and contacts.
package microsoft

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/patrickryan01/halyard/internal/httpx"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() { plugin.Register(mailtypes.KindMicrosoft, New) }

// Scopes are the full read and write set for mail and contacts.
var Scopes = []string{
	"offline_access", "openid", "profile", "email",
	"https://graph.microsoft.com/Mail.ReadWrite",
	"https://graph.microsoft.com/Mail.Send",
	"https://graph.microsoft.com/Contacts.ReadWrite",
	"https://graph.microsoft.com/MailboxSettings.ReadWrite",
	"https://graph.microsoft.com/People.Read",
	"https://graph.microsoft.com/User.Read",
}

// ClientID is the Entra application (public client) registration, set at
// build time or in the account settings.
var ClientID = ""

const graph = "https://graph.microsoft.com/v1.0"

func OAuthConfig(settings map[string]string) oauth.Config {
	id := ClientID
	if v := settings["client_id"]; v != "" {
		id = v
	}
	tenant := settings["tenant"]
	if tenant == "" {
		tenant = "common"
	}
	base := "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0"
	return oauth.Config{ClientID: id, AuthURL: base + "/authorize", TokenURL: base + "/token", Scopes: Scopes,
		Extra: url.Values{"prompt": {"select_account"}}}
}

type Provider struct {
	acct  mailtypes.Account
	api   *httpx.Client
	base  string
	mu    sync.Mutex
	roles map[string]string // folder id -> role
}

func New(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
	return NewWithSource(a, &oauth.Source{Cfg: OAuthConfig(a.Settings), Sec: s}), nil
}

func NewWithSource(a mailtypes.Account, ts oauth.TokenSource) *Provider {
	p := &Provider{acct: a, api: httpx.New(ts.Token), base: graph}
	if b := a.Settings["api_base"]; b != "" {
		p.base = b
	}
	return p
}

func (p *Provider) Kind() string { return mailtypes.KindMicrosoft }
func (p *Provider) Close() error { return nil }

// hdr sets immutable IDs, which keep a message's ID the same when it moves
// between folders, and any extra Prefer values.
func hdr(extra ...string) map[string]string {
	h := map[string]string{"Prefer": `IdType="ImmutableId"`}
	for _, e := range extra {
		h["Prefer"] += ", " + e
	}
	return h
}

func (p *Provider) Profile(ctx context.Context) (plugin.Profile, error) {
	var r struct {
		Mail, UserPrincipalName, DisplayName string
	}
	if err := p.api.Do(ctx, "GET", p.base+"/me?$select=mail,userPrincipalName,displayName", nil, nil, &r); err != nil {
		return plugin.Profile{}, err
	}
	e := r.Mail
	if e == "" {
		e = r.UserPrincipalName
	}
	return plugin.Profile{Email: e, Name: r.DisplayName}, nil
}

// ---- folders ----

type gfolder struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	ChildCount  int    `json:"childFolderCount"`
}

var wellKnown = []struct{ name, role string }{
	{"inbox", mailtypes.RoleInbox}, {"sentitems", mailtypes.RoleSent}, {"drafts", mailtypes.RoleDrafts},
	{"deleteditems", mailtypes.RoleTrash}, {"junkemail", mailtypes.RoleSpam}, {"archive", mailtypes.RoleArchive},
}

func (p *Provider) folders(ctx context.Context) ([]mailtypes.Folder, error) {
	roles := map[string]string{}
	for _, w := range wellKnown {
		var f gfolder
		if err := p.api.Do(ctx, "GET", p.base+"/me/mailFolders/"+w.name+"?$select=id,displayName", nil, hdr(), &f); err != nil {
			if httpx.IsStatus(err, 404) {
				continue // archive folder not created yet
			}
			return nil, err
		}
		roles[f.ID] = w.role
	}
	var out []mailtypes.Folder
	var walk func(path, parent string) error
	walk = func(prefix, parent string) error {
		u := p.base + "/me/mailFolders?$top=100&$select=id,displayName,childFolderCount"
		if parent != "" {
			u = p.base + "/me/mailFolders/" + parent + "/childFolders?$top=100&$select=id,displayName,childFolderCount"
		}
		for u != "" {
			var r struct {
				Value    []gfolder `json:"value"`
				NextLink string    `json:"@odata.nextLink"`
			}
			if err := p.api.Do(ctx, "GET", u, nil, hdr(), &r); err != nil {
				return err
			}
			for _, f := range r.Value {
				role, ok := roles[f.ID]
				if !ok {
					role = mailtypes.RoleCustom
				}
				out = append(out, mailtypes.Folder{RemoteID: f.ID, Name: prefix + f.DisplayName, Role: role})
				if f.ChildCount > 0 {
					if err := walk(prefix+f.DisplayName+"/", f.ID); err != nil {
						return err
					}
				}
			}
			u = r.NextLink
		}
		return nil
	}
	if err := walk("", ""); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.roles = roles
	p.mu.Unlock()
	return out, nil
}

// ---- messages ----

type gaddr struct {
	EmailAddress struct{ Name, Address string } `json:"emailAddress"`
}

type gmsg struct {
	ID                string    `json:"id"`
	Removed           *struct{} `json:"@removed"`
	ConversationID    string    `json:"conversationId"`
	Subject           string    `json:"subject"`
	BodyPreview       string    `json:"bodyPreview"`
	From              *gaddr    `json:"from"`
	ToRecipients      []gaddr   `json:"toRecipients"`
	CcRecipients      []gaddr   `json:"ccRecipients"`
	BccRecipients     []gaddr   `json:"bccRecipients"`
	ReplyTo           []gaddr   `json:"replyTo"`
	ReceivedDateTime  time.Time `json:"receivedDateTime"`
	SentDateTime      time.Time `json:"sentDateTime"`
	IsRead            bool      `json:"isRead"`
	IsDraft           bool      `json:"isDraft"`
	HasAttachments    bool      `json:"hasAttachments"`
	ParentFolderID    string    `json:"parentFolderId"`
	InternetMessageID string    `json:"internetMessageId"`
	Flag              struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	InternetMessageHeaders []struct{ Name, Value string } `json:"internetMessageHeaders"`
}

const selectFields = "id,conversationId,subject,bodyPreview,from,toRecipients,ccRecipients,bccRecipients,replyTo,receivedDateTime,sentDateTime,isRead,isDraft,hasAttachments,parentFolderId,internetMessageId,flag,body,internetMessageHeaders"

func addrs(as []gaddr) []mailtypes.Address {
	var o []mailtypes.Address
	for _, a := range as {
		o = append(o, mailtypes.Address{Name: a.EmailAddress.Name, Email: a.EmailAddress.Address})
	}
	return o
}

func (p *Provider) toMessage(ctx context.Context, g *gmsg) mailtypes.Message {
	m := mailtypes.Message{AccountID: p.acct.ID, RemoteID: g.ID, ThreadID: p.acct.ID + "/" + g.ConversationID, Subject: g.Subject,
		Snippet: g.BodyPreview, To: addrs(g.ToRecipients), Cc: addrs(g.CcRecipients), Bcc: addrs(g.BccRecipients), ReplyTo: addrs(g.ReplyTo),
		Date: g.ReceivedDateTime, Read: g.IsRead, Starred: g.Flag.FlagStatus == "flagged", Draft: g.IsDraft,
		MessageID: g.InternetMessageID, Folders: []string{g.ParentFolderID}}
	if m.Date.IsZero() {
		m.Date = g.SentDateTime
	}
	if g.From != nil {
		m.From = mailtypes.Address{Name: g.From.EmailAddress.Name, Email: g.From.EmailAddress.Address}
	}
	if strings.EqualFold(g.Body.ContentType, "html") {
		m.BodyHTML = g.Body.Content
	} else {
		m.BodyText = g.Body.Content
	}
	for _, h := range g.InternetMessageHeaders {
		switch strings.ToLower(h.Name) {
		case "in-reply-to":
			m.InReplyTo = strings.TrimSpace(h.Value)
		case "references":
			m.References = strings.Fields(h.Value)
		}
	}
	if g.HasAttachments {
		var r struct {
			Value []struct {
				ID, Name, ContentType, ContentID string
				Size                             int64
				IsInline                         bool
			} `json:"value"`
		}
		if err := p.api.Do(ctx, "GET", p.base+"/me/messages/"+g.ID+"/attachments?$select=id,name,contentType,size,isInline,contentId", nil, hdr(), &r); err == nil {
			for _, a := range r.Value {
				m.Attach = append(m.Attach, mailtypes.Attachment{ID: a.ID, Filename: a.Name, ContentType: a.ContentType, Size: a.Size, Inline: a.IsInline && a.ContentID != "", ContentID: a.ContentID})
			}
		}
	}
	return m
}

type deltaState map[string]string // folder id -> delta link

func (p *Provider) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	var res plugin.SyncResult
	fs, err := p.folders(ctx)
	if err != nil {
		return res, err
	}
	res.Change.Folders = fs
	st := deltaState{}
	if state != "" {
		_ = json.Unmarshal([]byte(state), &st)
	}
	res.Full = state == ""
	next := deltaState{}
	upserted := map[string]bool{}
	var removedIDs []string
	for _, f := range fs {
		link := st[f.RemoteID]
		first := link == ""
		if first {
			// newest first would be nicer, but delta has no ordering; the
			// receivedDateTime filter bounds the first pass to what is useful
			since := time.Now().AddDate(0, 0, -365).UTC().Format("2006-01-02T15:04:05Z")
			link = fmt.Sprintf("%s/me/mailFolders/%s/messages/delta?$select=%s&$filter=receivedDateTime ge %s", p.base, f.RemoteID, selectFields, since)
			link = strings.ReplaceAll(link, " ", "%20")
		}
		for link != "" {
			var r struct {
				Value     []gmsg `json:"value"`
				NextLink  string `json:"@odata.nextLink"`
				DeltaLink string `json:"@odata.deltaLink"`
			}
			err := p.api.Do(ctx, "GET", link, nil, hdr("odata.maxpagesize=50"), &r)
			if err != nil {
				if httpx.IsStatus(err, 410) || (httpx.IsStatus(err, 400) && !first) {
					// delta token expired: restart this folder
					delete(st, f.RemoteID)
					link = ""
					res.Full = true
					st[f.RemoteID] = ""
					since := time.Now().AddDate(0, 0, -365).UTC().Format("2006-01-02T15:04:05Z")
					link = strings.ReplaceAll(fmt.Sprintf("%s/me/mailFolders/%s/messages/delta?$select=%s&$filter=receivedDateTime ge %s", p.base, f.RemoteID, selectFields, since), " ", "%20")
					first = true
					continue
				}
				return res, fmt.Errorf("graph delta %s: %w", f.Name, err)
			}
			for i := range r.Value {
				g := &r.Value[i]
				if g.Removed != nil {
					removedIDs = append(removedIDs, g.ID)
					continue
				}
				res.Change.Upsert = append(res.Change.Upsert, p.toMessage(ctx, g))
				upserted[g.ID] = true
			}
			if r.NextLink != "" {
				link = r.NextLink
				continue
			}
			next[f.RemoteID] = r.DeltaLink
			link = ""
		}
	}
	// a message that moved shows as removed from one folder and added to
	// another; only a message Graph no longer has at all is a deletion
	for _, id := range removedIDs {
		if upserted[id] {
			continue
		}
		var g gmsg
		err := p.api.Do(ctx, "GET", p.base+"/me/messages/"+id+"?$select="+selectFields, nil, hdr(), &g)
		switch {
		case err == nil:
			res.Change.Upsert = append(res.Change.Upsert, p.toMessage(ctx, &g))
		case httpx.IsStatus(err, 404):
			res.Change.Delete = append(res.Change.Delete, id)
		default:
			return res, err
		}
	}
	b, _ := json.Marshal(next)
	res.State = string(b)
	return res, nil
}

// ---- actions ----

func (p *Provider) Apply(ctx context.Context, a mailtypes.Action) error {
	patch := func(id string, body map[string]any) error {
		return p.api.Do(ctx, "PATCH", p.base+"/me/messages/"+id, body, hdr(), nil)
	}
	move := func(id, dest string) error {
		return p.api.Do(ctx, "POST", p.base+"/me/messages/"+id+"/move", map[string]string{"destinationId": dest}, hdr(), nil)
	}
	for _, id := range a.IDs {
		var err error
		switch a.Kind {
		case mailtypes.ActRead:
			err = patch(id, map[string]any{"isRead": true})
		case mailtypes.ActUnread:
			err = patch(id, map[string]any{"isRead": false})
		case mailtypes.ActStar:
			err = patch(id, map[string]any{"flag": map[string]string{"flagStatus": "flagged"}})
		case mailtypes.ActUnstar:
			err = patch(id, map[string]any{"flag": map[string]string{"flagStatus": "notFlagged"}})
		case mailtypes.ActArchive:
			err = move(id, "archive")
		case mailtypes.ActTrash:
			err = move(id, "deleteditems")
		case mailtypes.ActSpam:
			err = move(id, "junkemail")
		case mailtypes.ActUntrash:
			err = move(id, "inbox")
		case mailtypes.ActMove:
			err = move(id, a.FolderID)
		case mailtypes.ActLabel:
			err = p.api.Do(ctx, "POST", p.base+"/me/messages/"+id+"/copy", map[string]string{"destinationId": a.FolderID}, hdr(), nil)
		case mailtypes.ActUnlabel, mailtypes.ActDelete:
			err = p.api.Do(ctx, "DELETE", p.base+"/me/messages/"+id, nil, hdr(), nil)
		default:
			err = fmt.Errorf("microsoft: unknown action %q", a.Kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Send hands Graph the full MIME message, so attachments, Bcc through the
// envelope and threading headers all survive exactly as built.
func (p *Provider) Send(ctx context.Context, o mailtypes.Outgoing) error {
	domain := "outlook.com"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.Build(o, mime.MessageID(domain, "o"))
	if err != nil {
		return err
	}
	// Bcc is not in the MIME headers, so create the message from MIME, add
	// Bcc recipients, then send it.
	enc := base64.StdEncoding.EncodeToString(raw)
	var draft gmsg
	if err := p.api.Do(ctx, "POST", p.base+"/me/messages", []byte(enc), map[string]string{"Content-Type": "text/plain", "Prefer": `IdType="ImmutableId"`}, &draft); err != nil {
		return fmt.Errorf("graph create message: %w", err)
	}
	if len(o.Bcc) > 0 {
		var bcc []map[string]any
		for _, b := range o.Bcc {
			bcc = append(bcc, map[string]any{"emailAddress": map[string]string{"address": b.Email, "name": b.Name}})
		}
		if err := p.api.Do(ctx, "PATCH", p.base+"/me/messages/"+draft.ID, map[string]any{"bccRecipients": bcc}, hdr(), nil); err != nil {
			return err
		}
	}
	return p.api.Do(ctx, "POST", p.base+"/me/messages/"+draft.ID+"/send", nil, hdr(), nil)
}

func (p *Provider) Attachment(ctx context.Context, msgID, attID string) ([]byte, string, error) {
	var raw []byte
	if err := p.api.Do(ctx, "GET", p.base+"/me/messages/"+msgID+"/attachments/"+attID+"/$value", nil, hdr(), &raw); err != nil {
		return nil, "", err
	}
	return raw, "", nil
}

// ---- contacts ----

func (p *Provider) Contacts(ctx context.Context, q string) ([]mailtypes.Contact, error) {
	var out []mailtypes.Contact
	if q != "" {
		var r struct {
			Value []struct {
				DisplayName string                     `json:"displayName"`
				ScoredEmail []struct{ Address string } `json:"scoredEmailAddresses"`
			} `json:"value"`
		}
		u := p.base + "/me/people?$top=10&$search=" + url.QueryEscape(`"`+q+`"`)
		if err := p.api.Do(ctx, "GET", u, nil, nil, &r); err != nil {
			return nil, err
		}
		for _, x := range r.Value {
			for _, e := range x.ScoredEmail {
				out = append(out, mailtypes.Contact{Name: x.DisplayName, Email: e.Address})
			}
		}
		return out, nil
	}
	u := p.base + "/me/contacts?$top=200&$select=displayName,emailAddresses"
	for u != "" && len(out) < 3000 {
		var r struct {
			Value []struct {
				ID          string                     `json:"id"`
				DisplayName string                     `json:"displayName"`
				Emails      []struct{ Address string } `json:"emailAddresses"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if err := p.api.Do(ctx, "GET", u, nil, nil, &r); err != nil {
			return out, err
		}
		for _, c := range r.Value {
			for _, e := range c.Emails {
				out = append(out, mailtypes.Contact{ID: c.ID, Name: c.DisplayName, Email: e.Address})
			}
		}
		u = r.Next
	}
	return out, nil
}

// Watch polls. Graph change notifications are delivered to a public HTTPS
// endpoint, which a desktop app does not have, so a 30 second tick triggers
// the cheap delta pass instead.
func (p *Provider) Watch(ctx context.Context, notify func()) error {
	t := time.NewTicker(30 * time.Second)
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

// CreateFolder makes a mail folder under the mailbox root.
func (p *Provider) CreateFolder(ctx context.Context, name, role string) (string, error) {
	var f gfolder
	err := p.api.Do(ctx, "POST", p.base+"/me/mailFolders", map[string]string{"displayName": name}, hdr(), &f)
	return f.ID, err
}

func contactBody(c mailtypes.Contact) map[string]any {
	b := map[string]any{"displayName": c.Name, "givenName": c.Name}
	if c.Email != "" {
		b["emailAddresses"] = []map[string]string{{"address": c.Email, "name": c.Name}}
	}
	if c.Phone != "" {
		b["mobilePhone"] = c.Phone
	}
	if c.Org != "" {
		b["companyName"] = c.Org
	}
	return b
}

func (p *Provider) CreateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	var r struct {
		ID string `json:"id"`
	}
	if err := p.api.Do(ctx, "POST", p.base+"/me/contacts", contactBody(c), nil, &r); err != nil {
		return c, err
	}
	c.ID = r.ID
	return c, nil
}

func (p *Provider) UpdateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	return c, p.api.Do(ctx, "PATCH", p.base+"/me/contacts/"+c.ID, contactBody(c), nil, nil)
}

func (p *Provider) DeleteContact(ctx context.Context, id string) error {
	return p.api.Do(ctx, "DELETE", p.base+"/me/contacts/"+id, nil, nil, nil)
}
