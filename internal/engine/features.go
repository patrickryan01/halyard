package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/ical"
	"github.com/patrickryan01/halyard/internal/importer"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/rules"
)

func (e *Engine) provider(accountID string) (plugin.Provider, mailtypes.Account, error) {
	acct, err := e.St.Account(accountID)
	if err != nil {
		return nil, acct, errors.New("unknown account")
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	return p, acct, err
}

// ---- rules ----

// ApplyRule runs one rule's actions on one message.
func (e *Engine) applyActions(r mailtypes.Rule, m mailtypes.Message) {
	// flags first: a move can give the message a new ID on the server, and a flag set after it would miss
	acts := make([]mailtypes.RuleAction, 0, len(r.Actions))
	for _, a := range r.Actions {
		switch a.Kind {
		case "move", "archive", "trash", "spam":
		default:
			acts = append(acts, a)
		}
	}
	for _, a := range r.Actions {
		switch a.Kind {
		case "move", "archive", "trash", "spam":
			acts = append(acts, a)
		}
	}
	for _, a := range acts {
		switch a.Kind {
		case "move":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActMove, FolderID: a.FolderID, IDs: []string{m.RemoteID}})
		case "label":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActLabel, FolderID: a.FolderID, IDs: []string{m.RemoteID}})
		case "archive":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActArchive, IDs: []string{m.RemoteID}})
		case "read":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActRead, IDs: []string{m.RemoteID}})
		case "star":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActStar, IDs: []string{m.RemoteID}})
		case "trash":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActTrash, IDs: []string{m.RemoteID}})
		case "spam":
			_ = e.Act(m.AccountID, mailtypes.Action{Kind: mailtypes.ActSpam, IDs: []string{m.RemoteID}})
		case "forward":
			o := mailtypes.Outgoing{AccountID: m.AccountID, To: mime.ParseList(a.To), Subject: "Fwd: " + m.Subject,
				BodyText: "Forwarded by a rule.\n\nFrom: " + m.From.Email + "\n\n" + m.BodyText, BodyHTML: ""}
			_, _ = e.SendAfter(o, 0)
		}
	}
}

// RunRules applies every enabled rule, in order, to the given messages. It returns the IDs that a rule took out of the
// inbox or marked read, so the caller does not announce them. A rule marked Stop ends the run for that message.
func (e *Engine) RunRules(ms []mailtypes.Message) map[string]bool {
	quiet := map[string]bool{}
	rs, err := e.St.Rules()
	if err != nil || len(rs) == 0 {
		return quiet
	}
	for _, m := range ms {
		for _, r := range rs {
			if !rules.Match(r, m) {
				continue
			}
			e.applyActions(r, m)
			for _, a := range r.Actions {
				switch a.Kind {
				case "move", "archive", "trash", "spam", "read":
					quiet[m.RemoteID] = true
				}
			}
			if r.Stop {
				break
			}
		}
	}
	return quiet
}

// RunRulesNow applies the rules to what is already in an account's inbox.
func (e *Engine) RunRulesNow(accountID string) (int, error) {
	inbox, ok := e.St.FolderByRole(accountID, mailtypes.RoleInbox)
	if !ok {
		return 0, errors.New("this account has no inbox yet")
	}
	ms, err := e.St.MessagesInFolder(accountID, inbox, false)
	if err != nil {
		return 0, err
	}
	n := len(e.RunRules(ms))
	return n, nil
}

// SaveRule validates and stores a rule.
func (e *Engine) SaveRule(r mailtypes.Rule) (mailtypes.Rule, error) {
	if msg := rules.Validate(r); msg != "" {
		return r, errors.New(msg)
	}
	return e.St.PutRule(r)
}

// ---- identities ----

// EnsureIdentity makes sure an account has its own address as an identity.
func (e *Engine) EnsureIdentity(a mailtypes.Account) {
	ids, _ := e.St.Identities(a.ID)
	if len(ids) > 0 {
		return
	}
	_, _ = e.St.PutIdentity(mailtypes.Identity{AccountID: a.ID, Name: a.Name, Email: a.Email, Signature: a.Signature, Default: true})
}

// RefreshIdentities asks the provider which addresses the person may send as and keeps them, with the ones typed in
// by hand.
func (e *Engine) RefreshIdentities(ctx context.Context, accountID string) error {
	p, acct, err := e.provider(accountID)
	if err != nil {
		return err
	}
	defer p.Close()
	e.EnsureIdentity(acct)
	il, ok := p.(plugin.IdentityLister)
	if !ok {
		return nil
	}
	ids, err := il.Identities(ctx)
	if err != nil {
		return err
	}
	return e.St.ReplaceProviderIdentities(accountID, ids)
}

// identityFor finds the identity a message is sent as.
func (e *Engine) identityFor(accountID, email string) (mailtypes.Identity, bool) {
	ids, _ := e.St.Identities(accountID)
	for _, i := range ids {
		if strings.EqualFold(i.Email, email) {
			return i, true
		}
	}
	for _, i := range ids {
		if i.Default {
			return i, email == ""
		}
	}
	return mailtypes.Identity{}, false
}

// ---- vacation ----

func (e *Engine) vacationProvider(accountID string) (plugin.VacationManager, plugin.Provider, error) {
	p, acct, err := e.provider(accountID)
	if err != nil {
		return nil, nil, err
	}
	vm, ok := p.(plugin.VacationManager)
	if !ok {
		p.Close()
		return nil, nil, fmt.Errorf("%s accounts keep no automatic reply on the server; set one up under Filters on the server (a Sieve vacation) if the server offers it", acct.Kind)
	}
	return vm, p, nil
}

func (e *Engine) GetVacation(ctx context.Context, accountID string) (mailtypes.Vacation, error) {
	vm, p, err := e.vacationProvider(accountID)
	if err != nil {
		return mailtypes.Vacation{}, err
	}
	defer p.Close()
	return vm.GetVacation(ctx)
}

func (e *Engine) SetVacation(ctx context.Context, accountID string, v mailtypes.Vacation) error {
	if v.Enabled && strings.TrimSpace(v.Body) == "" {
		return errors.New("write the reply first")
	}
	if !v.Start.IsZero() && !v.End.IsZero() && v.End.Before(v.Start) {
		return errors.New("the end is before the start")
	}
	vm, p, err := e.vacationProvider(accountID)
	if err != nil {
		return err
	}
	defer p.Close()
	return vm.SetVacation(ctx, v)
}

// ---- calendar invitations ----

// Invite is what a message's calendar invitation says, and what this person has answered.
type Invite struct {
	Events []ical.Event `json:"events"`
	// Me is the attendee line for this account, with the answer so far.
	Me       *ical.Attendee `json:"me,omitempty"`
	Method   string         `json:"method"`
	Conflict []string       `json:"conflicts,omitempty"`
}

func (e *Engine) inviteText(ctx context.Context, m mailtypes.Message) (string, error) {
	if m.ICS != "" {
		return m.ICS, nil
	}
	for _, a := range m.Attach {
		if mime.IsCalendar(a) {
			b, _, err := e.Attachment(ctx, m.AccountID, m.RemoteID, a.ID)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
	}
	return "", errors.New("this message has no invitation")
}

// Invite reads the invitation in a message.
func (e *Engine) Invite(ctx context.Context, messageID string) (Invite, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return Invite{}, errors.New("no such message")
	}
	txt, err := e.inviteText(ctx, m)
	if err != nil {
		return Invite{}, err
	}
	evs, err := ical.Parse(txt)
	if err != nil {
		return Invite{}, err
	}
	acct, _ := e.St.Account(m.AccountID)
	inv := Invite{Events: evs, Method: evs[0].Method}
	ids, _ := e.St.Identities(m.AccountID)
	mine := map[string]bool{strings.ToLower(acct.Email): true}
	for _, i := range ids {
		mine[strings.ToLower(i.Email)] = true
	}
	for i := range evs[0].Attendees {
		if mine[strings.ToLower(evs[0].Attendees[i].Email)] {
			a := evs[0].Attendees[i]
			inv.Me = &a
		}
	}
	return inv, nil
}

// RespondInvite answers an invitation: status is ACCEPTED, DECLINED or TENTATIVE. The answer goes to the organizer as
// an iTIP reply by email, which every calendar understands.
func (e *Engine) RespondInvite(ctx context.Context, messageID, status string) (string, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return "", errors.New("no such message")
	}
	inv, err := e.Invite(ctx, messageID)
	if err != nil {
		return "", err
	}
	if inv.Method == "CANCEL" || inv.Method == "REPLY" {
		return "", fmt.Errorf("a %s cannot be answered", strings.ToLower(inv.Method))
	}
	acct, _ := e.St.Account(m.AccountID)
	who := ical.Attendee{Name: acct.Name, Email: acct.Email}
	if inv.Me != nil {
		who = ical.Attendee{Name: inv.Me.Name, Email: inv.Me.Email}
	}
	if who.Name == "" {
		who.Name = acct.Name
	}
	text, err := ical.Reply(inv.Events[0], who, status)
	if err != nil {
		return "", err
	}
	verb := map[string]string{"ACCEPTED": "Accepted", "DECLINED": "Declined", "TENTATIVE": "Tentative"}[status]
	o := mailtypes.Outgoing{AccountID: m.AccountID, From: mailtypes.Address{Name: who.Name, Email: who.Email}, To: []mailtypes.Address{{Email: inv.Events[0].Organizer.Email, Name: inv.Events[0].Organizer.Name}},
		Subject: verb + ": " + inv.Events[0].Summary, BodyText: who.Name + " has " + strings.ToLower(verb) + " the invitation.\n",
		Attachments: []mailtypes.OutAttachment{{Filename: "reply.ics", ContentType: "text/calendar; method=REPLY; charset=utf-8", Data: []byte(text)}}}
	return e.SendAfter(o, 0)
}

// ---- unsubscribe ----

// UnsubscribeResult says what happened: Done when the sender was told, else OpenURL is a page the person must visit.
type UnsubscribeResult struct {
	Done    bool   `json:"done"`
	Method  string `json:"method,omitempty"` // one-click, mailto, page
	OpenURL string `json:"openUrl,omitempty"`
}

func unsubTargets(h string) (httpsURL, mailto string) {
	for _, part := range strings.Split(h, ",") {
		p := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "<>"))
		switch {
		case strings.HasPrefix(strings.ToLower(p), "https://") && httpsURL == "":
			httpsURL = p
		case strings.HasPrefix(strings.ToLower(p), "http://") && httpsURL == "":
			httpsURL = p
		case strings.HasPrefix(strings.ToLower(p), "mailto:") && mailto == "":
			mailto = p
		}
	}
	return
}

// HTTPClient is used for one click unsubscribe; tests replace it.
var HTTPClient = &http.Client{Timeout: 20 * time.Second}

// Unsubscribe leaves a mailing list the way the sender said to (RFC 2369 and RFC 8058).
func (e *Engine) Unsubscribe(ctx context.Context, messageID string) (UnsubscribeResult, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return UnsubscribeResult{}, errors.New("no such message")
	}
	if m.ListUnsubscribe == "" {
		return UnsubscribeResult{}, errors.New("this message has no unsubscribe link")
	}
	web, mailto := unsubTargets(m.ListUnsubscribe)
	if web != "" && m.ListUnsubscribePost && strings.HasPrefix(strings.ToLower(web), "https://") {
		req, _ := http.NewRequestWithContext(ctx, "POST", web, strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := HTTPClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return UnsubscribeResult{Done: true, Method: "one-click"}, nil
			}
		}
	}
	if mailto != "" {
		u, err := url.Parse(mailto)
		if err == nil {
			to := u.Opaque
			if to == "" {
				to = u.Path
			}
			subj := u.Query().Get("subject")
			if subj == "" {
				subj = "unsubscribe"
			}
			body := u.Query().Get("body")
			if body == "" {
				body = "unsubscribe"
			}
			o := mailtypes.Outgoing{AccountID: m.AccountID, To: mime.ParseList(to), Subject: subj, BodyText: body}
			if _, err := e.SendAfter(o, 0); err != nil {
				return UnsubscribeResult{}, err
			}
			return UnsubscribeResult{Done: true, Method: "mailto"}, nil
		}
	}
	if web != "" {
		return UnsubscribeResult{OpenURL: web, Method: "page"}, nil
	}
	return UnsubscribeResult{}, errors.New("the unsubscribe link in this message is not one this app can use")
}

// ---- read receipts ----

// SendReceipt answers a read receipt request (RFC 8098), once the person agreed.
func (e *Engine) SendReceipt(ctx context.Context, messageID string) (string, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return "", errors.New("no such message")
	}
	if m.ReceiptTo == "" {
		return "", errors.New("the sender did not ask for a receipt")
	}
	acct, _ := e.St.Account(m.AccountID)
	to := mime.ParseList(m.ReceiptTo)
	if len(to) == 0 {
		return "", errors.New("the receipt address is not valid")
	}
	body := "The message you sent to " + acct.Email + " with the subject \"" + m.Subject + "\" was displayed on " + time.Now().Format(time.RFC1123Z) + ".\n"
	mdn := "Reporting-UA: Halyard\r\nFinal-Recipient: rfc822;" + acct.Email + "\r\nOriginal-Message-ID: " + m.MessageID + "\r\nDisposition: manual-action/MDN-sent-manually; displayed\r\n"
	o := mailtypes.Outgoing{AccountID: m.AccountID, To: to[:1], Subject: "Read: " + m.Subject, BodyText: body, InReplyTo: m.MessageID,
		Attachments: []mailtypes.OutAttachment{{Filename: "receipt.txt", ContentType: "message/disposition-notification", Data: []byte(mdn)}}}
	return e.SendAfter(o, 0)
}

// ---- folders and bulk actions ----

func (e *Engine) folderManager(accountID string) (plugin.FolderManager, plugin.Provider, mailtypes.Account, error) {
	p, acct, err := e.provider(accountID)
	if err != nil {
		return nil, nil, acct, err
	}
	fm, ok := p.(plugin.FolderManager)
	if !ok {
		p.Close()
		return nil, nil, acct, fmt.Errorf("%s accounts cannot rename or delete folders", acct.Kind)
	}
	return fm, p, acct, nil
}

// RenameFolder renames a folder or label on the server.
func (e *Engine) RenameFolder(ctx context.Context, accountID, remoteID, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a folder needs a name")
	}
	if f, ok := e.folderByRemote(accountID, remoteID); ok && f.Role != mailtypes.RoleCustom && f.Role != "" {
		return errors.New("the " + f.Name + " folder is built in and keeps its name")
	}
	fm, p, _, err := e.folderManager(accountID)
	if err != nil {
		return err
	}
	defer p.Close()
	if err := fm.RenameFolder(ctx, remoteID, name); err != nil {
		return err
	}
	e.SyncNow(accountID)
	return nil
}

// DeleteFolder deletes a folder or label on the server.
func (e *Engine) DeleteFolder(ctx context.Context, accountID, remoteID string) error {
	if f, ok := e.folderByRemote(accountID, remoteID); ok && f.Role != mailtypes.RoleCustom && f.Role != "" {
		return errors.New("the " + f.Name + " folder is built in and cannot be deleted")
	}
	fm, p, _, err := e.folderManager(accountID)
	if err != nil {
		return err
	}
	defer p.Close()
	if err := fm.DeleteFolder(ctx, remoteID); err != nil {
		return err
	}
	e.SyncNow(accountID)
	return nil
}

func (e *Engine) folderByRemote(accountID, remoteID string) (mailtypes.Folder, bool) {
	fs, _ := e.St.Folders(accountID)
	for _, f := range fs {
		if f.RemoteID == remoteID {
			return f, true
		}
	}
	return mailtypes.Folder{}, false
}

// MarkAllRead marks every unread message in a folder read.
func (e *Engine) MarkAllRead(accountID, folderRemote string) (int, error) {
	ms, err := e.St.MessagesInFolder(accountID, folderRemote, true)
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.RemoteID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return len(ids), e.Act(accountID, mailtypes.Action{Kind: mailtypes.ActRead, IDs: ids})
}

// EmptyFolder deletes for good everything in the account's trash or spam folder.
func (e *Engine) EmptyFolder(accountID, role string) (int, error) {
	if role != mailtypes.RoleTrash && role != mailtypes.RoleSpam {
		return 0, errors.New("only trash and spam can be emptied")
	}
	f, ok := e.St.FolderByRole(accountID, role)
	if !ok {
		return 0, errors.New("this account has no such folder")
	}
	ms, err := e.St.MessagesInFolder(accountID, f, false)
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.RemoteID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return len(ids), e.Act(accountID, mailtypes.Action{Kind: mailtypes.ActDelete, IDs: ids})
}

// ---- export and import ----

// ExportEML rebuilds one message as an .eml file from what is cached.
func (e *Engine) ExportEML(ctx context.Context, messageID string) ([]byte, string, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return nil, "", errors.New("no such message")
	}
	o := mailtypes.Outgoing{From: m.From, To: m.To, Cc: m.Cc, Bcc: m.Bcc, Subject: m.Subject, BodyText: m.BodyText, BodyHTML: m.BodyHTML, InReplyTo: m.InReplyTo, References: m.References, Date: m.Date, ReplyTo: m.ReplyTo}
	for _, a := range m.Attach {
		b, _, err := e.Attachment(ctx, m.AccountID, m.RemoteID, a.ID)
		if err != nil {
			continue // an attachment the server no longer serves is left out, the rest is still worth saving
		}
		o.Attachments = append(o.Attachments, mailtypes.OutAttachment{Filename: a.Filename, ContentType: a.ContentType, Data: b})
	}
	id := m.MessageID
	if id == "" {
		id = mime.MessageID("export.invalid", messageID)
	}
	raw, err := mime.BuildDraft(o, id)
	if err != nil {
		return nil, "", err
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, m.Subject)
	if name == "" {
		name = "message"
	}
	return raw, name + ".eml", nil
}

// ExportMbox writes the messages of the given conversations as one mbox file.
func (e *Engine) ExportMbox(ctx context.Context, threadIDs []string) ([]byte, error) {
	var buf bytes.Buffer
	for _, tid := range threadIDs {
		ms, err := e.St.Thread(tid)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			raw, _, err := e.ExportEML(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			importer.WriteMbox(&buf, m.From.Email, m.Date, raw)
		}
	}
	return buf.Bytes(), nil
}

// ImportAccount returns the local account mail is imported into, making it the first time.
func (e *Engine) ImportAccount(ctx context.Context) (mailtypes.Account, error) {
	all, _ := e.St.Accounts()
	for _, a := range all {
		if a.Kind == mailtypes.KindLocal {
			return a, nil
		}
	}
	a, err := e.AddAccount(ctx, mailtypes.Account{Kind: mailtypes.KindLocal, Email: "local@this.computer", Name: "On this computer", Settings: map[string]string{}}, []byte("local"))
	if err != nil {
		return a, err
	}
	return a, e.ensureLocalFolders(a.ID)
}

// ImportResult says how many messages came in.
type ImportResult struct {
	Folder   string `json:"folder"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"` // already there
}

// ImportMbox reads an mbox stream into a folder of the local account, made if it is not there.
func (e *Engine) ImportMbox(ctx context.Context, folder string, r io.Reader) (ImportResult, error) {
	res := ImportResult{Folder: folder}
	acct, err := e.ImportAccount(ctx)
	if err != nil {
		return res, err
	}
	if err := e.ensureLocalFolders(acct.ID); err != nil {
		return res, err
	}
	if folder == "" {
		folder = "Imported"
		res.Folder = folder
	}
	remote, err := e.localFolder(acct.ID, folder)
	if err != nil {
		return res, err
	}
	var batch []mailtypes.Message
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := e.St.UpsertMessages(batch)
		batch = batch[:0]
		return err
	}
	seen := map[string]bool{}
	err = importer.Mbox(r, func(raw []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		rid := hex.EncodeToString(sum[:10])
		if seen[rid] || len(e.St.KnownMessages(acct.ID, []string{rid})) > 0 {
			res.Skipped++
			return nil
		}
		seen[rid] = true
		pm, _ := mime.Parse(raw)
		m := mailtypes.Message{AccountID: acct.ID, RemoteID: rid, Subject: pm.Subject, From: pm.From, To: pm.To, Cc: pm.Cc, ReplyTo: pm.ReplyTo, Date: pm.Date,
			BodyText: pm.Text, BodyHTML: pm.HTML, MessageID: pm.MessageID, InReplyTo: pm.InReplyTo, References: pm.References, Attach: pm.Attach,
			Folders: []string{remote}, Snippet: mime.Snippet(pm.Text, pm.HTML), Read: true, Size: pm.Size, ICS: pm.ICS,
			ListUnsubscribe: pm.ListUnsubscribe, ListUnsubscribePost: pm.ListUnsubscribePost, ReceiptTo: pm.ReceiptTo}
		if m.Date.IsZero() {
			m.Date = time.Now()
		}
		if err := e.St.PutRaw(acct.ID, rid, raw); err != nil {
			return err
		}
		batch = append(batch, m)
		res.Imported++
		if len(batch) >= 200 {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err == nil {
		e.emit(Event{Type: "synced", AccountID: acct.ID})
	}
	return res, err
}

func (e *Engine) ensureLocalFolders(accountID string) error {
	fs, _ := e.St.Folders(accountID)
	if len(fs) > 0 {
		return nil
	}
	return e.St.ReplaceFolders(accountID, []mailtypes.Folder{
		{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox}, {RemoteID: "SENT", Name: "Sent", Role: mailtypes.RoleSent},
		{RemoteID: "ARCHIVE", Name: "Archive", Role: mailtypes.RoleArchive}, {RemoteID: "TRASH", Name: "Trash", Role: mailtypes.RoleTrash},
	})
}

// localFolder returns the remote ID of the local folder with that name, adding it when needed.
func (e *Engine) localFolder(accountID, name string) (string, error) {
	fs, _ := e.St.Folders(accountID)
	for _, f := range fs {
		if strings.EqualFold(f.Name, name) || strings.EqualFold(f.RemoteID, name) {
			return f.RemoteID, nil
		}
	}
	fs = append(fs, mailtypes.Folder{AccountID: accountID, RemoteID: name, Name: name, Role: mailtypes.RoleCustom})
	return name, e.St.ReplaceFolders(accountID, fs)
}

// ImportThunderbird imports every folder of a Thunderbird profile directory.
func (e *Engine) ImportThunderbird(ctx context.Context, profile string) ([]ImportResult, error) {
	fs, err := importer.Thunderbird(profile)
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return nil, errors.New("no mail folders found in that profile")
	}
	var out []ImportResult
	for _, f := range fs {
		fh, err := os.Open(f.Path)
		if err != nil {
			return out, err
		}
		r, err := e.ImportMbox(ctx, f.Name, fh)
		fh.Close()
		out = append(out, r)
		if err != nil {
			return out, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return out, nil
}
