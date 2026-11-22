package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/store"
)

// ActThreads applies an action to every message of the given conversations,
// per account. For move and label, accountID names the account that owns
// folderID.
func (e *Engine) ActThreads(kind string, threadIDs []string, folderID, accountID string) error {
	byAcct := map[string][]string{}
	for _, tid := range threadIDs {
		ms, err := e.St.Thread(tid)
		if err != nil {
			return err
		}
		for _, m := range ms {
			switch kind {
			case mailtypes.ActRead:
				if m.Read {
					continue
				}
			case mailtypes.ActUnread:
				if !m.Read {
					continue
				}
			}
			byAcct[m.AccountID] = append(byAcct[m.AccountID], m.RemoteID)
		}
	}
	for acct, ids := range byAcct {
		if folderID != "" && accountID != "" && accountID != acct {
			continue
		}
		if err := e.Act(acct, mailtypes.Action{Kind: kind, IDs: ids, FolderID: folderID}); err != nil {
			return err
		}
	}
	return nil
}

// CreateFolder makes a label or folder on the account's server.
func (e *Engine) CreateFolder(ctx context.Context, accountID, name string) (string, error) {
	acct, err := e.St.Account(accountID)
	if err != nil {
		return "", errors.New("unknown account")
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err != nil {
		return "", err
	}
	defer p.Close()
	fc, ok := p.(plugin.FolderCreator)
	if !ok {
		return "", fmt.Errorf("%s accounts cannot create folders", acct.Kind)
	}
	id, err := fc.CreateFolder(ctx, name, mailtypes.RoleCustom)
	if err != nil {
		return "", err
	}
	e.SyncNow(accountID)
	return id, nil
}

// ---- drafts ----

// DraftData is a message being written. It is kept on this computer until
// it is sent; attachments ride along as base64 so a draft is self contained.
type DraftData struct {
	ID          string        `json:"id"`
	AccountID   string        `json:"accountId"`
	To          []string      `json:"to"`
	Cc          []string      `json:"cc"`
	Bcc         []string      `json:"bcc"`
	Subject     string        `json:"subject"`
	BodyText    string        `json:"bodyText"`
	BodyHTML    string        `json:"bodyHtml"`
	InReplyTo   string        `json:"inReplyTo"`
	References  []string      `json:"references"`
	ThreadHint  string        `json:"threadHint"`
	Attachments []DraftAttach `json:"attachments"`
	// RemoteID is the draft\'s ID in the provider\'s Drafts folder, when the provider keeps drafts.
	RemoteID string `json:"remoteId,omitempty"`
	// FromEmail is the identity the message is sent as; empty means the account's own address.
	FromEmail        string `json:"fromEmail,omitempty"`
	ReceiptRequested bool   `json:"receiptRequested,omitempty"`
	Protect          string `json:"protect,omitempty"`
	Sign             bool   `json:"sign,omitempty"`
	Encrypt          bool   `json:"encrypt,omitempty"`
}

type DraftAttach struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Base64      string `json:"base64"`
	Path        string `json:"path,omitempty"` // read at send time when no Base64
}

func (e *Engine) SaveDraft(d DraftData) (string, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	// a draft saved before keeps its server copy: carry the remote ID over
	if old, err := e.GetDraft(d.ID); err == nil && d.RemoteID == "" {
		d.RemoteID = old.RemoteID
	}
	// the copy in the provider's Drafts folder, so the draft shows up on every device
	if acct, err := e.St.Account(d.AccountID); err == nil {
		if p, err := e.Factory(acct, e.Sec.For(acct.ID)); err == nil {
			if ds, ok := p.(plugin.DraftSaver); ok {
				if o, err := d.Outgoing(); err == nil {
					o.From = mailtypes.Address{Name: acct.Name, Email: acct.Email}
					ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
					id, err := ds.SaveDraft(ctx, o, d.RemoteID)
					cancel()
					if err == nil {
						d.RemoteID = id
					} else {
						log.Printf("draft %s: server copy: %v", d.ID, err)
					}
				}
			}
			p.Close()
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	if err := e.St.PutDraft(store.Draft{ID: d.ID, AccountID: d.AccountID, Data: b, Subject: d.Subject}); err != nil {
		return "", err
	}
	e.SyncNow(d.AccountID)
	return d.ID, nil
}

// DiscardDraft removes a draft here and in the provider's Drafts folder.
func (e *Engine) DiscardDraft(id string) error {
	if d, err := e.GetDraft(id); err == nil && d.RemoteID != "" {
		e.deleteRemoteDraft(d)
	}
	return e.St.DeleteDraft(id)
}

func (e *Engine) deleteRemoteDraft(d DraftData) {
	acct, err := e.St.Account(d.AccountID)
	if err != nil {
		return
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err != nil {
		return
	}
	defer p.Close()
	if ds, ok := p.(plugin.DraftSaver); ok {
		ctx, cancel := context.WithTimeout(e.ctx, 60*time.Second)
		defer cancel()
		if err := ds.DeleteDraft(ctx, d.RemoteID); err != nil {
			log.Printf("draft %s: remove server copy: %v", d.ID, err)
		}
		e.SyncNow(d.AccountID)
	}
}

// OpenProviderDraft turns a message in a provider's Drafts folder into an
// editable draft, with its attachments, so saving it again replaces the
// server copy.
func (e *Engine) OpenProviderDraft(ctx context.Context, messageID string) (DraftData, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return DraftData{}, errors.New("no such message")
	}
	acct, err := e.St.Account(m.AccountID)
	if err != nil {
		return DraftData{}, err
	}
	d := DraftData{AccountID: m.AccountID, Subject: m.Subject, BodyText: m.BodyText, BodyHTML: m.BodyHTML, InReplyTo: m.InReplyTo, References: m.References}
	if i := strings.Index(m.ThreadID, "/"); i >= 0 {
		d.ThreadHint = m.ThreadID[i+1:]
	}
	addrs := func(in []mailtypes.Address) []string {
		var out []string
		for _, a := range in {
			if a.Name == "" {
				out = append(out, a.Email)
			} else {
				out = append(out, fmt.Sprintf("%q <%s>", a.Name, a.Email))
			}
		}
		return out
	}
	d.To, d.Cc, d.Bcc = addrs(m.To), addrs(m.Cc), addrs(m.Bcc)
	for _, a := range m.Attach {
		if a.Inline {
			continue
		}
		b, _, err := e.Attachment(ctx, m.AccountID, m.RemoteID, a.ID)
		if err != nil {
			return d, fmt.Errorf("could not fetch attachment %s: %w", a.Filename, err)
		}
		d.Attachments = append(d.Attachments, DraftAttach{Filename: a.Filename, ContentType: a.ContentType, Base64: base64.StdEncoding.EncodeToString(b)})
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err != nil {
		return d, err
	}
	defer p.Close()
	if ds, ok := p.(plugin.DraftSaver); ok {
		if id, err := ds.DraftID(ctx, m.RemoteID); err == nil {
			d.RemoteID = id
		}
	}
	d.ID = newID()
	return d, nil
}

func (e *Engine) GetDraft(id string) (DraftData, error) {
	st, err := e.St.Draft(id)
	if err != nil {
		return DraftData{}, errors.New("no such draft")
	}
	var d DraftData
	return d, json.Unmarshal(st.Data, &d)
}

// Outgoing turns a draft into a sendable message.
func (d DraftData) Outgoing() (mailtypes.Outgoing, error) {
	o := mailtypes.Outgoing{AccountID: d.AccountID, Subject: d.Subject, BodyText: d.BodyText, BodyHTML: d.BodyHTML, InReplyTo: d.InReplyTo, References: d.References, ThreadHint: d.ThreadHint,
		ReceiptRequested: d.ReceiptRequested, Protect: d.Protect, Sign: d.Sign, Encrypt: d.Encrypt}
	if d.FromEmail != "" {
		o.From = mailtypes.Address{Email: d.FromEmail}
	}
	list := func(in []string) []mailtypes.Address {
		var out []mailtypes.Address
		for _, s := range in {
			out = append(out, mime.ParseList(s)...)
		}
		return out
	}
	o.To, o.Cc, o.Bcc = list(d.To), list(d.Cc), list(d.Bcc)
	for _, a := range d.Attachments {
		var data []byte
		switch {
		case a.Base64 != "":
			b, err := base64.StdEncoding.DecodeString(a.Base64)
			if err != nil {
				return o, fmt.Errorf("attachment %s is not valid base64", a.Filename)
			}
			data = b
		case a.Path != "":
			b, err := os.ReadFile(a.Path)
			if err != nil {
				return o, fmt.Errorf("attachment %s: %w", a.Path, err)
			}
			data = b
			if a.Filename == "" {
				a.Filename = a.Path[strings.LastIndexAny(a.Path, "/\\")+1:]
			}
		default:
			return o, fmt.Errorf("attachment %s has no content", a.Filename)
		}
		o.Attachments = append(o.Attachments, mailtypes.OutAttachment{Filename: a.Filename, ContentType: a.ContentType, Data: data})
	}
	return o, nil
}

// SendDraft sends a saved draft through the outbox, then removes the draft.
func (e *Engine) SendDraft(id string) (string, error) {
	d, err := e.GetDraft(id)
	if err != nil {
		return "", err
	}
	o, err := d.Outgoing()
	if err != nil {
		return "", err
	}
	out, err := e.Send(o)
	if err != nil {
		return "", err
	}
	if d.RemoteID != "" {
		e.deleteRemoteDraft(d)
	}
	_ = e.St.DeleteDraft(id)
	return out, nil
}

// BuildReply prepares a draft that answers a message: recipients, subject,
// threading headers and the quoted original. mode is reply, replyAll or forward.
func (e *Engine) BuildReply(ctx context.Context, messageID, mode, bodyText string) (DraftData, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return DraftData{}, errors.New("no such message")
	}
	acct, err := e.St.Account(m.AccountID)
	if err != nil {
		return DraftData{}, err
	}
	d := DraftData{AccountID: m.AccountID, InReplyTo: m.MessageID, References: append(append([]string{}, m.References...), m.MessageID), BodyText: bodyText}
	if i := strings.Index(m.ThreadID, "/"); i >= 0 {
		d.ThreadHint = m.ThreadID[i+1:]
	}
	addr := func(a mailtypes.Address) string {
		if a.Name == "" {
			return a.Email
		}
		return fmt.Sprintf("%q <%s>", a.Name, a.Email)
	}
	quote := "<br><div>On " + m.Date.Format("Mon, Jan 2, 2006 at 3:04 PM") + ", " + html.EscapeString(addr(m.From)) + " wrote:</div><blockquote>"
	if m.BodyHTML != "" {
		quote += m.BodyHTML
	} else {
		quote += "<div>" + strings.ReplaceAll(html.EscapeString(m.BodyText), "\n", "<br>") + "</div>"
	}
	quote += "</blockquote>"
	body := "<div>" + strings.ReplaceAll(html.EscapeString(bodyText), "\n", "<br>") + "</div>"
	switch mode {
	case "forward":
		d.Subject = "Fwd: " + strings.TrimPrefix(strings.TrimPrefix(m.Subject, "Fwd: "), "FW: ")
		d.InReplyTo, d.References, d.ThreadHint = "", nil, ""
		d.BodyHTML = body + "<br><div>---------- Forwarded message ----------<br>From: " + html.EscapeString(addr(m.From)) + "<br>Subject: " + html.EscapeString(m.Subject) + "</div>" +
			func() string {
				if m.BodyHTML != "" {
					return m.BodyHTML
				}
				return "<div>" + strings.ReplaceAll(html.EscapeString(m.BodyText), "\n", "<br>") + "</div>"
			}()
		for _, a := range m.Attach {
			if a.Inline {
				continue
			}
			b, _, err := e.Attachment(ctx, m.AccountID, m.RemoteID, a.ID)
			if err != nil {
				return d, fmt.Errorf("could not fetch attachment %s to forward: %w", a.Filename, err)
			}
			d.Attachments = append(d.Attachments, DraftAttach{Filename: a.Filename, ContentType: a.ContentType, Base64: base64.StdEncoding.EncodeToString(b)})
		}
	default:
		d.Subject = m.Subject
		if !strings.HasPrefix(strings.ToLower(m.Subject), "re:") {
			d.Subject = "Re: " + m.Subject
		}
		me := strings.EqualFold(m.From.Email, acct.Email)
		base := []mailtypes.Address{m.From}
		if len(m.ReplyTo) > 0 {
			base = m.ReplyTo
		}
		if me {
			base = m.To
		}
		for _, a := range base {
			d.To = append(d.To, addr(a))
		}
		if mode == "replyAll" {
			seen := map[string]bool{strings.ToLower(acct.Email): true}
			for _, a := range base {
				seen[strings.ToLower(a.Email)] = true
			}
			for _, a := range append(append([]mailtypes.Address{}, m.To...), m.Cc...) {
				if !seen[strings.ToLower(a.Email)] {
					seen[strings.ToLower(a.Email)] = true
					d.Cc = append(d.Cc, addr(a))
				}
			}
		}
		d.BodyHTML = body + quote
	}
	return d, nil
}

// ---- contacts ----

func (e *Engine) writer(accountID string) (plugin.Provider, plugin.ContactWriter, error) {
	acct, err := e.St.Account(accountID)
	if err != nil {
		return nil, nil, errors.New("unknown account")
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err != nil {
		return nil, nil, err
	}
	cw, _ := p.(plugin.ContactWriter)
	if cw == nil && acct.Settings[SDAVURL] != "" {
		if c, err := e.davFor(acct); err == nil {
			cw = davContacts{e: e, c: c, accountID: acct.ID}
		}
	}
	return p, cw, nil
}

// CreateContact adds a contact to the account's own address book (Google
// contacts, Microsoft contacts) and to the local book for accounts that have none.
func (e *Engine) CreateContact(ctx context.Context, accountID string, c mailtypes.Contact) (mailtypes.Contact, error) {
	p, cw, err := e.writer(accountID)
	if err != nil {
		return c, err
	}
	defer p.Close()
	if cw != nil {
		c, err = cw.CreateContact(ctx, c)
		if err != nil {
			return c, err
		}
	} else if c, err = e.St.PutLocalContact(accountID, c); err != nil {
		return c, err
	}
	_ = e.St.PutContacts(accountID, []mailtypes.Contact{c})
	return c, nil
}

func (e *Engine) UpdateContact(ctx context.Context, accountID string, c mailtypes.Contact) (mailtypes.Contact, error) {
	if c.ID == "" {
		return c, errors.New("contact id required")
	}
	if strings.HasPrefix(c.ID, "local:") {
		c, err := e.St.PutLocalContact(accountID, c)
		if err == nil {
			_ = e.St.PutContacts(accountID, []mailtypes.Contact{c})
		}
		return c, err
	}
	p, cw, err := e.writer(accountID)
	if err != nil {
		return c, err
	}
	defer p.Close()
	if cw == nil {
		return c, errors.New("this account's address book cannot be edited")
	}
	c, err = cw.UpdateContact(ctx, c)
	if err == nil {
		_ = e.St.PutContacts(accountID, []mailtypes.Contact{c})
	}
	return c, err
}

func (e *Engine) DeleteContact(ctx context.Context, accountID, id string) error {
	if strings.HasPrefix(id, "local:") {
		return e.St.DeleteLocalContact(id)
	}
	p, cw, err := e.writer(accountID)
	if err != nil {
		return err
	}
	defer p.Close()
	if cw == nil {
		return errors.New("this account's address book cannot be edited")
	}
	return cw.DeleteContact(ctx, id)
}

// SearchContacts looks in the cache (learned from mail and synced from the
// provider) and the local address book. Providers with a live directory are
// asked too.
func (e *Engine) SearchContacts(ctx context.Context, q string) []mailtypes.Contact {
	out, _ := e.St.SuggestContacts(q, 25)
	loc, _ := e.St.LocalContacts(q)
	out = append(out, loc...)
	// a group is offered like a person, and stands for all its members
	if gs, err := e.St.Groups(); err == nil {
		for _, g := range gs {
			if q == "" || strings.Contains(strings.ToLower(g.Name), strings.ToLower(q)) {
				out = append([]mailtypes.Contact{{Name: g.Name + " (group)", Email: "", Members: g.Members}}, out...)
			}
		}
	}
	seen := map[string]bool{}
	var dedup []mailtypes.Contact
	for _, c := range out {
		k := strings.ToLower(c.Email)
		if k != "" && seen[k] {
			continue
		}
		seen[k] = true
		dedup = append(dedup, c)
	}
	return dedup
}

// WaitSent is for callers that want to block until the outbox has gone out:
// tools that promise a result rather than a queue position. It returns the
// failure text if the send failed.
func (e *Engine) WaitSent(ctx context.Context, outboxID string, d time.Duration) (sent bool, failure string) {
	end := time.Now().Add(d)
	for time.Now().Before(end) && ctx.Err() == nil {
		items, _ := e.St.Outbox()
		found := false
		for _, it := range items {
			if it.ID == outboxID {
				found = true
				if it.State == "failed" {
					return false, it.Error
				}
			}
		}
		if !found {
			return true, ""
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false, ""
}
