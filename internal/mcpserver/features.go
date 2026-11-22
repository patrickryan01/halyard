package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/ical"
	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// add registers a tool whose result is any value; a value that is not a JSON object is returned under "result".
func add[I any](srv *mcp.Server, name, desc string, f func(ctx context.Context, in I) (any, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc}, func(ctx context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, obj, error) {
		v, err := f(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(v)
		var o obj
		if json.Unmarshal(b, &o) != nil || o == nil {
			o = obj{"result": v}
		}
		return nil, o, nil
	})
}

type acct struct {
	AccountID string `json:"accountId" jsonschema:"the account"`
}

type id struct {
	ID string `json:"id"`
}

func (s *Server) features(srv *mcp.Server) {
	e := s.Eng
	// ---- rules, templates, saved searches ----
	add(srv, "list_rules", "List the filter rules, in the order they run on new mail.", func(ctx context.Context, in struct{}) (any, error) {
		r, err := e.St.Rules()
		return obj{"rules": r}, err
	})
	add(srv, "save_rule", "Create a filter rule (no id) or replace one (with id). match is all or any. A condition has field (from, to, cc, subject, body, has_attachment, has_unsubscribe, size_over, size_under), op (contains, not_contains, equals, starts, ends, matches) and value. An action has kind (move, label, archive, read, star, trash, spam, forward) and folderId or to.",
		func(ctx context.Context, in mailtypes.Rule) (any, error) { return e.SaveRule(in) })
	add(srv, "delete_rule", "Delete a filter rule.", func(ctx context.Context, in id) (any, error) { return obj{"ok": true}, e.St.DeleteRule(in.ID) })
	add(srv, "move_rule", "Change when a rule runs: index 0 runs first.", func(ctx context.Context, in struct {
		ID    string `json:"id"`
		Index int    `json:"index"`
	}) (any, error) {
		return obj{"ok": true}, e.St.MoveRule(in.ID, in.Index)
	})
	add(srv, "run_rules", "Run the filter rules now on what is already in an account's inbox.", func(ctx context.Context, in acct) (any, error) {
		n, err := e.RunRulesNow(in.AccountID)
		return obj{"matched": n}, err
	})
	add(srv, "list_templates", "List canned replies.", func(ctx context.Context, in struct{}) (any, error) {
		t, err := e.St.Templates()
		return obj{"templates": t}, err
	})
	add(srv, "save_template", "Create a canned reply (no id) or replace one.", func(ctx context.Context, in mailtypes.Template) (any, error) { return e.St.PutTemplate(in) })
	add(srv, "delete_template", "Delete a canned reply.", func(ctx context.Context, in id) (any, error) { return obj{"ok": true}, e.St.DeleteTemplate(in.ID) })
	add(srv, "list_saved_searches", "List saved searches.", func(ctx context.Context, in struct{}) (any, error) {
		t, err := e.St.SavedSearches()
		return obj{"searches": t}, err
	})
	add(srv, "save_search", "Save a search under a name.", func(ctx context.Context, in mailtypes.SavedSearch) (any, error) { return e.St.PutSavedSearch(in) })
	add(srv, "delete_saved_search", "Delete a saved search.", func(ctx context.Context, in id) (any, error) { return obj{"ok": true}, e.St.DeleteSavedSearch(in.ID) })
	// ---- identities, signatures, vacation ----
	add(srv, "list_identities", "List the addresses a person sends from, with their signatures. Aliases the server reports are included.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId,omitempty"`
	}) (any, error) {
		i, err := e.St.Identities(in.AccountID)
		return obj{"identities": i}, err
	})
	add(srv, "refresh_identities", "Ask the account's server which addresses it may send as (Gmail send-as, JMAP identities, Microsoft aliases).", func(ctx context.Context, in acct) (any, error) {
		if err := e.RefreshIdentities(ctx, in.AccountID); err != nil {
			return nil, err
		}
		i, err := e.St.Identities(in.AccountID)
		return obj{"identities": i}, err
	})
	add(srv, "save_identity", "Add an identity (no id) or change one: name, email, replyTo, signature, default.", func(ctx context.Context, in mailtypes.Identity) (any, error) { return e.St.PutIdentity(in) })
	add(srv, "delete_identity", "Remove an identity.", func(ctx context.Context, in id) (any, error) { return obj{"ok": true}, e.St.DeleteIdentity(in.ID) })
	add(srv, "get_vacation", "Read the account's automatic reply as the server holds it.", func(ctx context.Context, in acct) (any, error) { return e.GetVacation(ctx, in.AccountID) })
	add(srv, "set_vacation", "Turn the automatic reply on or off on the server, with subject, body and optional start and end (RFC 3339). It answers while the app is closed.", func(ctx context.Context, in struct {
		AccountID    string `json:"accountId"`
		Enabled      bool   `json:"enabled"`
		Subject      string `json:"subject,omitempty"`
		Body         string `json:"body,omitempty"`
		Start        string `json:"start,omitempty"`
		End          string `json:"end,omitempty"`
		ContactsOnly bool   `json:"contactsOnly,omitempty"`
	}) (any, error) {
		v := mailtypes.Vacation{Enabled: in.Enabled, Subject: in.Subject, Body: in.Body, ContactsOnly: in.ContactsOnly}
		var err error
		if in.Start != "" {
			if v.Start, err = time.Parse(time.RFC3339, in.Start); err != nil {
				return nil, errors.New("start must be RFC 3339")
			}
		}
		if in.End != "" {
			if v.End, err = time.Parse(time.RFC3339, in.End); err != nil {
				return nil, errors.New("end must be RFC 3339")
			}
		}
		return obj{"ok": true}, e.SetVacation(ctx, in.AccountID, v)
	})
	// ---- invitations, unsubscribe, receipts ----
	add(srv, "get_invite", "Read the calendar invitation in a message: events, organizer, attendees and this person's answer so far.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		return e.Invite(ctx, in.MessageID)
	})
	add(srv, "respond_invite", "Answer an invitation: answer is accept, decline or tentative. The reply goes to the organizer by email.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
		Answer    string `json:"answer"`
	}) (any, error) {
		st := map[string]string{"accept": "ACCEPTED", "decline": "DECLINED", "tentative": "TENTATIVE"}[strings.ToLower(in.Answer)]
		if st == "" {
			return nil, errors.New("answer must be accept, decline or tentative")
		}
		o, err := e.RespondInvite(ctx, in.MessageID, st)
		return obj{"outboxId": o}, err
	})
	add(srv, "add_invite_to_calendar", "Save an invitation's event in a calendar on a calendar server (the message's own account unless calendarAccountId says another).", func(ctx context.Context, in struct {
		MessageID         string `json:"messageId"`
		CalendarAccountID string `json:"calendarAccountId,omitempty"`
	}) (any, error) {
		return e.AddInviteToCalendar(ctx, in.MessageID, in.CalendarAccountID)
	})
	add(srv, "unsubscribe", "Leave a mailing list the way the sender says: one click, or by email. If the sender only offers a web page, its address comes back to open.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		return e.Unsubscribe(ctx, in.MessageID)
	})
	add(srv, "send_read_receipt", "Answer a sender's read receipt request for a message.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		o, err := e.SendReceipt(ctx, in.MessageID)
		return obj{"outboxId": o}, err
	})
	// ---- folders and bulk ----
	add(srv, "rename_folder", "Rename a folder or label on the server.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		RemoteID  string `json:"remoteId"`
		Name      string `json:"name"`
	}) (any, error) {
		return obj{"ok": true}, e.RenameFolder(ctx, in.AccountID, in.RemoteID, in.Name)
	})
	add(srv, "delete_folder", "Delete a folder or label on the server.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		RemoteID  string `json:"remoteId"`
	}) (any, error) {
		return obj{"ok": true}, e.DeleteFolder(ctx, in.AccountID, in.RemoteID)
	})
	add(srv, "mark_folder_read", "Mark every unread message in a folder read.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		RemoteID  string `json:"remoteId"`
	}) (any, error) {
		n, err := e.MarkAllRead(in.AccountID, in.RemoteID)
		return obj{"count": n}, err
	})
	add(srv, "empty_folder", "Delete for good everything in an account's trash or spam (role trash or spam).", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Role      string `json:"role"`
	}) (any, error) {
		n, err := e.EmptyFolder(in.AccountID, in.Role)
		return obj{"count": n}, err
	})
	// ---- export, import, source ----
	add(srv, "export_message", "Save a message as an .eml file at path.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
		Path      string `json:"path"`
	}) (any, error) {
		b, _, err := e.ExportEML(ctx, in.MessageID)
		if err != nil {
			return nil, err
		}
		return obj{"path": in.Path, "size": len(b)}, os.WriteFile(in.Path, b, 0o644)
	})
	add(srv, "export_threads", "Save conversations as one mbox file at path.", func(ctx context.Context, in struct {
		ThreadIDs []string `json:"threadIds"`
		Path      string   `json:"path"`
	}) (any, error) {
		b, err := e.ExportMbox(ctx, in.ThreadIDs)
		if err != nil {
			return nil, err
		}
		return obj{"path": in.Path, "size": len(b)}, os.WriteFile(in.Path, b, 0o644)
	})
	add(srv, "import_mbox", "Import an mbox file into a folder of the 'On this computer' account.", func(ctx context.Context, in struct {
		Path   string `json:"path"`
		Folder string `json:"folder,omitempty"`
	}) (any, error) {
		f, err := os.Open(in.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return e.ImportMbox(ctx, in.Folder, f)
	})
	add(srv, "import_thunderbird", "Import every folder of a Thunderbird profile directory.", func(ctx context.Context, in struct {
		Path string `json:"path"`
	}) (any, error) {
		r, err := e.ImportThunderbird(ctx, in.Path)
		return obj{"folders": r}, err
	})
	add(srv, "get_message_source", "The message exactly as it was sent: all headers and parts.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		b, _, err := e.RawMessage(ctx, in.MessageID)
		return obj{"source": string(b)}, err
	})
	// ---- keys, signing, encryption ----
	add(srv, "list_keys", "List OpenPGP keys and S/MIME certificates held (never any private key).", func(ctx context.Context, in struct{}) (any, error) { return e.Keyring() })
	add(srv, "make_key", "Make a key pair (kind pgp) or a self signed certificate (kind smime) for an address.", func(ctx context.Context, in struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}) (any, error) {
		return obj{"ok": true}, e.MakeKey(in.Kind, in.Name, in.Email)
	})
	add(srv, "import_key", "Import an OpenPGP key (armored text) or an S/MIME certificate (PEM text, or a .p12 file by path with its password).", func(ctx context.Context, in struct {
		Kind     string `json:"kind"`
		Text     string `json:"text,omitempty"`
		Path     string `json:"path,omitempty"`
		Password string `json:"password,omitempty"`
	}) (any, error) {
		data := []byte(in.Text)
		if in.Path != "" {
			b, err := os.ReadFile(in.Path)
			if err != nil {
				return nil, err
			}
			data = b
		}
		n, err := e.ImportKey(in.Kind, data, in.Password)
		return obj{"imported": n}, err
	})
	add(srv, "remove_key", "Remove a key or certificate.", func(ctx context.Context, in id) (any, error) { return obj{"ok": true}, e.RemoveKey(in.ID) })
	add(srv, "export_key", "The public half of a key or certificate, as text to share.", func(ctx context.Context, in id) (any, error) {
		t, _, err := e.ExportKey(in.ID)
		return obj{"text": t}, err
	})
	add(srv, "open_secure_message", "Check the signature of a signed message and decrypt an encrypted one: returns the verdict, who signed, and the readable text.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		return e.Security(ctx, in.MessageID)
	})
	add(srv, "keep_signer", "Keep the key or certificate that signed a message, so the sender is trusted and can be written to encrypted.", func(ctx context.Context, in struct {
		MessageID string `json:"messageId"`
	}) (any, error) {
		return obj{"ok": true}, e.KeepSigner(ctx, in.MessageID)
	})
	// ---- calendars and contacts servers (CalDAV, CardDAV) ----
	add(srv, "set_calendar_server", "Point an account at its CalDAV and CardDAV server (calendars and contacts). Leave url empty to remove it.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		URL       string `json:"url,omitempty"`
		User      string `json:"user,omitempty"`
		Password  string `json:"password,omitempty"`
	}) (any, error) {
		return obj{"ok": true}, e.SetDAV(ctx, in.AccountID, in.URL, in.User, in.Password)
	})
	add(srv, "list_calendars", "List the calendars of an account's calendar server.", func(ctx context.Context, in acct) (any, error) {
		c, err := e.Calendars(ctx, in.AccountID)
		return obj{"calendars": c}, err
	})
	add(srv, "list_events", "List calendar events between from and to (RFC 3339; default the next 45 days).", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		From      string `json:"from,omitempty"`
		To        string `json:"to,omitempty"`
	}) (any, error) {
		from, err := time.Parse(time.RFC3339, in.From)
		if err != nil {
			from = time.Now()
		}
		to, err := time.Parse(time.RFC3339, in.To)
		if err != nil {
			to = from.AddDate(0, 0, 45)
		}
		ev, err := e.Events(ctx, in.AccountID, from, to)
		return obj{"events": ev}, err
	})
	add(srv, "save_event", "Create a calendar event, or change one (pass url and etag from list_events). start and end are RFC 3339.", func(ctx context.Context, in struct {
		AccountID   string `json:"accountId"`
		CalendarURL string `json:"calendarUrl,omitempty"`
		URL         string `json:"url,omitempty"`
		ETag        string `json:"etag,omitempty"`
		UID         string `json:"uid,omitempty"`
		Title       string `json:"title"`
		Start       string `json:"start"`
		End         string `json:"end"`
		Location    string `json:"location,omitempty"`
		Description string `json:"description,omitempty"`
		AllDay      bool   `json:"allDay,omitempty"`
	}) (any, error) {
		st, err := time.Parse(time.RFC3339, in.Start)
		if err != nil {
			return nil, errors.New("start must be RFC 3339")
		}
		en, err := time.Parse(time.RFC3339, in.End)
		if err != nil {
			return nil, errors.New("end must be RFC 3339")
		}
		ev := engine.CalEvent{Event: ical.Event{UID: in.UID, Summary: in.Title, Start: st, End: en, Location: in.Location, Description: in.Description, AllDay: in.AllDay}, URL: in.URL, ETag: in.ETag}
		return e.SaveEvent(ctx, in.AccountID, in.CalendarURL, ev)
	})
	add(srv, "delete_event", "Delete a calendar event by url.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		URL       string `json:"url"`
		ETag      string `json:"etag,omitempty"`
	}) (any, error) {
		return obj{"ok": true}, e.DeleteEvent(ctx, in.AccountID, in.URL, in.ETag)
	})
	add(srv, "list_address_book", "List the contacts on an account's CardDAV server.", func(ctx context.Context, in acct) (any, error) {
		c, err := e.AddressBook(ctx, in.AccountID)
		return obj{"contacts": c}, err
	})
	// ---- server side filters ----
	add(srv, "list_sieve_scripts", "List the Sieve filter scripts on an account's server (ManageSieve).", func(ctx context.Context, in acct) (any, error) {
		l, err := e.SieveScripts(ctx, in.AccountID)
		return obj{"scripts": l}, err
	})
	add(srv, "get_sieve_script", "Read a Sieve script.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Name      string `json:"name"`
	}) (any, error) {
		t, err := e.SieveScript(ctx, in.AccountID, in.Name)
		return obj{"name": in.Name, "script": t}, err
	})
	add(srv, "put_sieve_script", "Store a Sieve script; the server compiles it and refuses one with an error. activate makes it the running script.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Name      string `json:"name"`
		Script    string `json:"script"`
		Activate  bool   `json:"activate,omitempty"`
	}) (any, error) {
		return obj{"ok": true}, e.SievePut(ctx, in.AccountID, in.Name, in.Script, in.Activate)
	})
	add(srv, "check_sieve_script", "Ask the server whether a Sieve script compiles, without storing it.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Script    string `json:"script"`
	}) (any, error) {
		return obj{"ok": true}, e.SieveCheck(ctx, in.AccountID, in.Script)
	})
	add(srv, "activate_sieve_script", "Make a Sieve script the one that runs; an empty name turns server filtering off.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Name      string `json:"name"`
	}) (any, error) {
		return obj{"ok": true}, e.SieveActivate(ctx, in.AccountID, in.Name)
	})
	add(srv, "delete_sieve_script", "Delete a Sieve script.", func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		Name      string `json:"name"`
	}) (any, error) {
		return obj{"ok": true}, e.SieveDelete(ctx, in.AccountID, in.Name)
	})
}
