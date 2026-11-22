// Package mcpserver is the client's MCP server. It is on by default and it
// reads and writes everything the app can: every account's mail, folders and
// labels, drafts and the outbox, snooze, attachments in and out, contacts and
// settings. It signs in as the person using the app, so every account the app
// has is reachable through it, and nothing in it is read only.
//
// It serves streamable HTTP on loopback behind a bearer token that lives in
// the data directory, and the same tools over stdio for clients that launch it.
package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/patrickryan01/halyard/internal/api"
	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/store"
)

type Server struct {
	Eng     *engine.Engine
	API     *api.Server
	Version string
	Name    string
}

type obj = map[string]any

func ptr[T any](v T) *T { return &v }

// New builds the MCP server with every tool registered.
func New(eng *engine.Engine, a *api.Server, name, version string) *mcp.Server {
	s := &Server{Eng: eng, API: a, Version: version, Name: name}
	srv := mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, &mcp.ServerOptions{
		Instructions: "This is the user's mail client. Every tool reads and writes real mail for the accounts signed in to the app. " +
			"Threads are conversations: use search_threads or list_threads to find them, get_thread to read one, and thread_action to act on whole conversations. " +
			"send_message, reply_message and forward_message go through the outbox: by default they leave after the user's undo window, and cancel_send undoes one before then.",
	})
	s.accounts(srv)
	s.folders(srv)
	s.read(srv)
	s.write(srv)
	s.actions(srv)
	s.contacts(srv)
	s.settings(srv)
	return srv
}

func text(format string, a ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, a...)}}}
}

// ---------------- accounts ----------------

type none struct{}

func (s *Server) accounts(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "list_accounts", Description: "List the mail accounts signed in to the app: id, kind (google, microsoft, imap, jmap), email, name, signature, last sync and any sync error."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, obj, error) {
			as, err := s.Eng.St.Accounts()
			if as == nil {
				as = []mailtypes.Account{}
			}
			return nil, obj{"accounts": as}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "add_account_password", Description: "Add an IMAP or JMAP account that signs in with a password or app password. kind is imap or jmap. For imap give imapHost (and optional ports and security ssl, starttls or none, smtpHost). For jmap give jmapUrl."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in api.PasswordIn) (*mcp.CallToolResult, obj, error) {
			a, err := s.API.AddPassword(ctx, in)
			return nil, obj{"account": a}, err
		})
	type oauthIn struct {
		Kind         string `json:"kind" jsonschema:"google or microsoft"`
		ClientID     string `json:"clientId,omitempty" jsonschema:"only if this build has no sign-in registered"`
		ClientSecret string `json:"clientSecret,omitempty"`
		Tenant       string `json:"tenant,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "start_oauth_signin", Description: "Start a browser sign-in for a Google or Microsoft account. Returns the sign-in URL (the app also opens it) and an id; call finish_oauth_signin with that id after the person approves."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in oauthIn) (*mcp.CallToolResult, obj, error) {
			id, u, err := s.API.StartOAuth(in.Kind, in.ClientID, in.ClientSecret, in.Tenant, true)
			return nil, obj{"id": id, "url": u}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "finish_oauth_signin", Description: "Wait up to 60 seconds for a sign-in started with start_oauth_signin. Returns the new account, or pending=true to call again."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			ID string `json:"id"`
		}) (*mcp.CallToolResult, obj, error) {
			a, pending, err := s.API.WaitOAuth(ctx, in.ID, 60*time.Second)
			if err != nil {
				return nil, nil, err
			}
			if pending {
				return nil, obj{"pending": true}, nil
			}
			return nil, obj{"account": a}, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "update_account", Description: "Change an account's display name or signature (the signature is appended to messages sent from it)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			AccountID string `json:"accountId"`
			Name      string `json:"name,omitempty"`
			Signature string `json:"signature,omitempty"`
		}) (*mcp.CallToolResult, obj, error) {
			a, err := s.Eng.St.Account(in.AccountID)
			if err != nil {
				return nil, nil, errors.New("unknown account")
			}
			if in.Name != "" {
				a.Name = in.Name
			}
			a.Signature = in.Signature
			return nil, obj{"account": a}, s.Eng.St.PutAccount(a)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "remove_account", Description: "Sign an account out and delete its saved mail from this computer. Mail on the server is not touched."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			AccountID string `json:"accountId"`
		}) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"removed": in.AccountID}, s.Eng.RemoveAccount(in.AccountID)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "sync_now", Description: "Check for new mail now, for one account or all of them."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			AccountID string `json:"accountId,omitempty"`
		}) (*mcp.CallToolResult, obj, error) {
			s.Eng.SyncNow(in.AccountID)
			return nil, obj{"ok": true}, nil
		})
}

// ---------------- folders ----------------

func (s *Server) folders(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "list_folders", Description: "List folders and labels with their roles (inbox, sent, drafts, trash, spam, archive, all, custom) and unread counts. folderId values from here go into list_threads, move_threads and label_threads."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			AccountID string `json:"accountId,omitempty"`
		}) (*mcp.CallToolResult, obj, error) {
			fs, err := s.Eng.St.Folders(in.AccountID)
			if fs == nil {
				fs = []mailtypes.Folder{}
			}
			return nil, obj{"folders": fs}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "create_folder", Description: "Create a label (Gmail) or folder (Microsoft, IMAP, JMAP) on an account's server."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			AccountID string `json:"accountId"`
			Name      string `json:"name"`
		}) (*mcp.CallToolResult, obj, error) {
			id, err := s.Eng.CreateFolder(ctx, in.AccountID, in.Name)
			return nil, obj{"remoteId": id}, err
		})
}

// ---------------- reading ----------------

type listIn struct {
	AccountID string `json:"accountId,omitempty" jsonschema:"limit to one account; empty means all accounts (unified)"`
	Folder    string `json:"folder,omitempty" jsonschema:"role:inbox, role:sent, role:drafts, role:archive, role:trash, role:spam, role:all, starred, snoozed, or a folder id from list_folders. Default role:inbox"`
	Query     string `json:"query,omitempty" jsonschema:"full text search; supports from: to: subject: is:unread is:starred has:attachment"`
	Unread    bool   `json:"unreadOnly,omitempty"`
	Limit     int    `json:"limit,omitempty" jsonschema:"default 25, max 200"`
	Offset    int    `json:"offset,omitempty"`
}

func (s *Server) listThreads(in listIn) ([]mailtypes.Thread, error) {
	if in.Limit <= 0 {
		in.Limit = 25
	}
	if in.Limit > 200 {
		in.Limit = 200
	}
	q := store.ListQuery{AccountID: in.AccountID, Folder: in.Folder, Text: in.Query, Unread: in.Unread, Limit: in.Limit, Offset: in.Offset}
	var words []string
	for _, w := range strings.Fields(q.Text) {
		switch strings.ToLower(w) {
		case "is:unread":
			q.Unread = true
		case "has:attachment":
			q.HasAttach = true
		case "is:starred":
			q.Starred = true
		default:
			words = append(words, w)
		}
	}
	q.Text = strings.Join(words, " ")
	if q.Folder == "" && q.Text == "" && !q.HasAttach && !q.Starred {
		q.Folder = "role:inbox"
	}
	ts, err := s.Eng.St.ListThreads(q)
	if ts == nil {
		ts = []mailtypes.Thread{}
	}
	return ts, err
}

func (s *Server) read(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "list_threads", Description: "List conversations in a folder, newest first, across all accounts or one. Each has id, subject, snippet, participants, date, message count, unread count and flags."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, obj, error) {
			ts, err := s.listThreads(in)
			return nil, obj{"threads": ts}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "search_threads", Description: "Full text search over every synced message (subject, sender, recipients, body). Operators: from:, to:, subject:, is:unread, is:starred, has:attachment. Trash and spam are excluded."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, obj, error) {
			in.Folder = ""
			if strings.TrimSpace(in.Query) == "" {
				return nil, nil, errors.New("query is required")
			}
			ts, err := s.listThreads(in)
			return nil, obj{"threads": ts}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "get_thread", Description: "Read a whole conversation: every message with headers, text and HTML bodies, attachment list and flags. Does not mark anything read; use thread_action read for that."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			ThreadID string `json:"threadId"`
		}) (*mcp.CallToolResult, obj, error) {
			ms, err := s.Eng.St.Thread(in.ThreadID)
			if err == nil && len(ms) == 0 {
				err = errors.New("no such thread")
			}
			return nil, obj{"messages": ms}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "get_message", Description: "Read one message by its id."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			MessageID string `json:"messageId"`
		}) (*mcp.CallToolResult, obj, error) {
			m, err := s.Eng.St.Message(in.MessageID)
			if err != nil {
				return nil, nil, errors.New("no such message")
			}
			return nil, obj{"message": m}, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "get_attachment", Description: "Download an attachment. Returns its bytes as base64 (up to 25 MB). If saveTo is set, the file is written there instead and only the path and size come back."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			MessageID    string `json:"messageId"`
			AttachmentID string `json:"attachmentId"`
			SaveTo       string `json:"saveTo,omitempty"`
		}) (*mcp.CallToolResult, obj, error) {
			m, err := s.Eng.St.Message(in.MessageID)
			if err != nil {
				return nil, nil, errors.New("no such message")
			}
			var meta mailtypes.Attachment
			for _, a := range m.Attach {
				if a.ID == in.AttachmentID {
					meta = a
				}
			}
			b, ct, err := s.Eng.Attachment(ctx, m.AccountID, m.RemoteID, in.AttachmentID)
			if err != nil {
				return nil, nil, err
			}
			if meta.ContentType != "" {
				ct = meta.ContentType
			}
			out := obj{"filename": meta.Filename, "contentType": ct, "size": len(b)}
			if in.SaveTo != "" {
				if err := os.MkdirAll(filepath.Dir(in.SaveTo), 0o755); err != nil {
					return nil, nil, err
				}
				if err := os.WriteFile(in.SaveTo, b, 0o644); err != nil {
					return nil, nil, err
				}
				out["savedTo"] = in.SaveTo
				return nil, out, nil
			}
			if len(b) > 25<<20 {
				return nil, nil, errors.New("attachment is larger than 25 MB; pass saveTo to write it to disk")
			}
			out["base64"] = base64.StdEncoding.EncodeToString(b)
			return nil, out, nil
		})
}

// ---------------- writing ----------------

type sendIn struct {
	AccountID   string               `json:"accountId" jsonschema:"account to send from"`
	To          []string             `json:"to" jsonschema:"addresses, plain or 'Name <a@b>'"`
	Cc          []string             `json:"cc,omitempty"`
	Bcc         []string             `json:"bcc,omitempty"`
	Subject     string               `json:"subject"`
	BodyText    string               `json:"bodyText,omitempty"`
	BodyHTML    string               `json:"bodyHtml,omitempty"`
	Attachments []engine.DraftAttach `json:"attachments,omitempty" jsonschema:"each with filename and either base64 content or a local path"`
	Immediate   bool                 `json:"immediate,omitempty" jsonschema:"send now instead of after the undo window"`
}

func (s *Server) queue(d engine.DraftData, immediate bool) (obj, error) {
	o, err := d.Outgoing()
	if err != nil {
		return nil, err
	}
	delay := s.Eng.UndoDelay
	if n := s.Eng.St.Setting("undoSeconds", "10"); n != "" {
		var secs int
		if _, e := fmt.Sscan(n, &secs); e == nil && secs >= 0 && secs <= 30 {
			delay = time.Duration(secs) * time.Second
		}
	}
	if immediate {
		delay = 0
	}
	id, err := s.Eng.SendAfter(o, delay)
	if err != nil {
		return nil, err
	}
	return obj{"outboxId": id, "sendsInSeconds": int(delay.Seconds()), "note": "call cancel_send with outboxId before it leaves to undo"}, nil
}

func (s *Server) write(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "send_message", Description: "Send a new message. It goes through the outbox, so it leaves after the user's undo window unless immediate is true. The account's signature is appended."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, obj, error) {
			o, err := s.queue(engine.DraftData{AccountID: in.AccountID, To: in.To, Cc: in.Cc, Bcc: in.Bcc, Subject: in.Subject, BodyText: in.BodyText, BodyHTML: in.BodyHTML, Attachments: in.Attachments}, in.Immediate)
			return nil, o, err
		})
	type replyIn struct {
		MessageID   string               `json:"messageId" jsonschema:"the message being answered"`
		BodyText    string               `json:"bodyText" jsonschema:"your reply; the original is quoted below it"`
		ReplyAll    bool                 `json:"replyAll,omitempty"`
		To          []string             `json:"to,omitempty" jsonschema:"for forward_message: who to forward to"`
		Attachments []engine.DraftAttach `json:"attachments,omitempty"`
		Immediate   bool                 `json:"immediate,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "reply_message", Description: "Reply (or reply all) to a message, keeping the thread, with the original quoted."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in replyIn) (*mcp.CallToolResult, obj, error) {
			mode := "reply"
			if in.ReplyAll {
				mode = "replyAll"
			}
			d, err := s.Eng.BuildReply(ctx, in.MessageID, mode, in.BodyText)
			if err != nil {
				return nil, nil, err
			}
			d.Attachments = append(d.Attachments, in.Attachments...)
			o, err := s.queue(d, in.Immediate)
			return nil, o, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "forward_message", Description: "Forward a message, with its attachments, to the given recipients."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in replyIn) (*mcp.CallToolResult, obj, error) {
			if len(in.To) == 0 {
				return nil, nil, errors.New("to is required")
			}
			d, err := s.Eng.BuildReply(ctx, in.MessageID, "forward", in.BodyText)
			if err != nil {
				return nil, nil, err
			}
			d.To = in.To
			d.Attachments = append(d.Attachments, in.Attachments...)
			o, err := s.queue(d, in.Immediate)
			return nil, o, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "cancel_send", Description: "Undo a send that is still inside its undo window. Returns undone=false if it already left."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			OutboxID string `json:"outboxId"`
		}) (*mcp.CallToolResult, obj, error) {
			ok, err := s.Eng.Undo(in.OutboxID)
			return nil, obj{"undone": ok}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "list_outbox", Description: "List messages waiting to send or that failed, with the failure text."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, obj, error) {
			items, err := s.Eng.St.Outbox()
			out := []obj{}
			for _, it := range items {
				out = append(out, obj{"outboxId": it.ID, "accountId": it.AccountID, "subject": it.Subject, "state": it.State, "error": it.Error, "sendAt": it.SendAt})
			}
			return nil, obj{"outbox": out}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "retry_send", Description: "Put a failed message back in the outbox."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			OutboxID string `json:"outboxId"`
		}) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.RetryOutbox(in.OutboxID)
		})
	type draftIn struct {
		DraftID     string               `json:"draftId,omitempty" jsonschema:"set to update an existing draft"`
		AccountID   string               `json:"accountId"`
		To          []string             `json:"to,omitempty"`
		Cc          []string             `json:"cc,omitempty"`
		Bcc         []string             `json:"bcc,omitempty"`
		Subject     string               `json:"subject,omitempty"`
		BodyText    string               `json:"bodyText,omitempty"`
		BodyHTML    string               `json:"bodyHtml,omitempty"`
		Attachments []engine.DraftAttach `json:"attachments,omitempty"`
	}
	save := func(ctx context.Context, _ *mcp.CallToolRequest, in draftIn) (*mcp.CallToolResult, obj, error) {
		id, err := s.Eng.SaveDraft(engine.DraftData{ID: in.DraftID, AccountID: in.AccountID, To: in.To, Cc: in.Cc, Bcc: in.Bcc, Subject: in.Subject, BodyText: in.BodyText, BodyHTML: in.BodyHTML, Attachments: in.Attachments})
		return nil, obj{"draftId": id}, err
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "create_draft", Description: "Save a message as a draft on this computer. Drafts show in the app's Drafts view."}, save)
	mcp.AddTool(srv, &mcp.Tool{Name: "update_draft", Description: "Replace a draft's contents (pass draftId)."}, save)
	mcp.AddTool(srv, &mcp.Tool{Name: "list_drafts", Description: "List saved drafts."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, obj, error) {
			ds, err := s.Eng.St.Drafts()
			out := []obj{}
			for _, d := range ds {
				out = append(out, obj{"draftId": d.ID, "accountId": d.AccountID, "subject": d.Subject, "updated": d.Updated})
			}
			return nil, obj{"drafts": out}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "get_draft", Description: "Read a draft in full."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			DraftID string `json:"draftId"`
		}) (*mcp.CallToolResult, obj, error) {
			d, err := s.Eng.GetDraft(in.DraftID)
			return nil, obj{"draft": d}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "send_draft", Description: "Send a saved draft through the outbox, then remove it."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			DraftID string `json:"draftId"`
		}) (*mcp.CallToolResult, obj, error) {
			id, err := s.Eng.SendDraft(in.DraftID)
			return nil, obj{"outboxId": id}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "delete_draft", Description: "Discard a draft."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			DraftID string `json:"draftId"`
		}) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.DiscardDraft(in.DraftID)
		})
}

// ---------------- actions ----------------

func (s *Server) actions(srv *mcp.Server) {
	type actIn struct {
		ThreadIDs []string `json:"threadIds"`
		Kind      string   `json:"kind" jsonschema:"archive, trash, untrash (back to inbox), spam, read, unread, star, unstar, or delete (permanently)"`
	}
	valid := map[string]bool{"archive": true, "trash": true, "untrash": true, "spam": true, "read": true, "unread": true, "star": true, "unstar": true, "delete": true}
	mcp.AddTool(srv, &mcp.Tool{Name: "thread_action", Description: "Apply an action to whole conversations on every account involved: archive, trash, untrash, spam, read, unread, star, unstar, or delete forever. Changes reach the server."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in actIn) (*mcp.CallToolResult, obj, error) {
			if !valid[in.Kind] {
				return nil, nil, fmt.Errorf("unknown action %q", in.Kind)
			}
			return nil, obj{"ok": true, "threads": len(in.ThreadIDs)}, s.Eng.ActThreads(in.Kind, in.ThreadIDs, "", "")
		})
	type folderIn struct {
		ThreadIDs []string `json:"threadIds"`
		AccountID string   `json:"accountId" jsonschema:"account that owns the folder"`
		FolderID  string   `json:"folderId" jsonschema:"remoteId from list_folders"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "move_threads", Description: "Move conversations into a folder (folder accounts) or, on Gmail, into a label and out of the inbox."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in folderIn) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.ActThreads(mailtypes.ActMove, in.ThreadIDs, in.FolderID, in.AccountID)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "label_threads", Description: "Add a label (Gmail), or copy into a folder (others), keeping the conversation where it is."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in folderIn) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.ActThreads(mailtypes.ActLabel, in.ThreadIDs, in.FolderID, in.AccountID)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "unlabel_threads", Description: "Remove a label or folder from conversations."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in folderIn) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.ActThreads(mailtypes.ActUnlabel, in.ThreadIDs, in.FolderID, in.AccountID)
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "snooze_threads", Description: "Hide conversations from the inbox until a time, then bring them back. until is RFC 3339, for example 2026-12-01T08:00:00-05:00. An empty until clears the snooze."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			ThreadIDs []string `json:"threadIds"`
			Until     string   `json:"until,omitempty"`
		}) (*mcp.CallToolResult, obj, error) {
			var until time.Time
			if in.Until != "" {
				t, err := time.Parse(time.RFC3339, in.Until)
				if err != nil {
					return nil, nil, errors.New("until must be RFC 3339")
				}
				until = t
			}
			for _, id := range in.ThreadIDs {
				var err error
				if until.IsZero() {
					err = s.Eng.St.Unsnooze(id)
				} else {
					err = s.Eng.St.Snooze(id, until)
				}
				if err != nil {
					return nil, nil, err
				}
			}
			return nil, obj{"ok": true}, nil
		})
}

// ---------------- contacts and settings ----------------

func (s *Server) contacts(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "search_contacts", Description: "Find contacts and people the user has corresponded with, by name or address."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			Query string `json:"query"`
		}) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"contacts": s.Eng.SearchContacts(ctx, in.Query)}, nil
		})
	type cIn struct {
		AccountID string `json:"accountId"`
		ContactID string `json:"contactId,omitempty"`
		Name      string `json:"name,omitempty"`
		Email     string `json:"email,omitempty"`
		Phone     string `json:"phone,omitempty"`
		Org       string `json:"org,omitempty"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "create_contact", Description: "Add a contact. Google and Microsoft accounts write it to their own address book; other accounts keep it in the app's local address book."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cIn) (*mcp.CallToolResult, obj, error) {
			c, err := s.Eng.CreateContact(ctx, in.AccountID, mailtypes.Contact{Name: in.Name, Email: in.Email, Phone: in.Phone, Org: in.Org})
			return nil, obj{"contact": c}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "update_contact", Description: "Change a contact (pass contactId)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cIn) (*mcp.CallToolResult, obj, error) {
			c, err := s.Eng.UpdateContact(ctx, in.AccountID, mailtypes.Contact{ID: in.ContactID, Name: in.Name, Email: in.Email, Phone: in.Phone, Org: in.Org})
			return nil, obj{"contact": c}, err
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "delete_contact", Description: "Delete a contact."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cIn) (*mcp.CallToolResult, obj, error) {
			return nil, obj{"ok": true}, s.Eng.DeleteContact(ctx, in.AccountID, in.ContactID)
		})
}

func (s *Server) settings(srv *mcp.Server) {
	keys := []string{"theme", "density", "loadImages", "notifications", "undoSeconds"}
	mcp.AddTool(srv, &mcp.Tool{Name: "get_settings", Description: "Read the app's settings: theme (auto, light, dark), density, loadImages, notifications, undoSeconds."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, obj, error) {
			out := obj{}
			def := map[string]string{"theme": "auto", "density": "comfortable", "loadImages": "false", "notifications": "true", "undoSeconds": "10"}
			for _, k := range keys {
				out[k] = s.Eng.St.Setting(k, def[k])
			}
			return nil, out, nil
		})
	mcp.AddTool(srv, &mcp.Tool{Name: "update_settings", Description: "Change settings. Pass any of theme, density, loadImages, notifications, undoSeconds as strings."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]string) (*mcp.CallToolResult, obj, error) {
			for _, k := range keys {
				if v, ok := in[k]; ok {
					if err := s.Eng.St.SetSetting(k, v); err != nil {
						return nil, nil, err
					}
				}
			}
			return nil, obj{"ok": true}, nil
		})
}

// ---------------- transport ----------------

// Token returns the bearer token stored in dir, creating it on first use.
func Token(dir string) (string, error) {
	p := filepath.Join(dir, "mcp-token")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	return t, os.WriteFile(p, []byte(t+"\n"), 0o600)
}

// Handler serves the MCP server over streamable HTTP, requiring the token.
func Handler(srv *mcp.Server, token string) http.Handler {
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mail"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// Info is what a client needs to connect, written to mcp.json in the data directory.
type Info struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Serve starts the HTTP endpoint on addr (loopback only) at /mcp and writes
// mcp.json. It returns the info and a stop function.
func Serve(dir, addr string, srv *mcp.Server) (Info, func(), error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return Info{}, nil, errors.New("the MCP server only serves on loopback")
	}
	tok, err := Token(dir)
	if err != nil {
		return Info{}, nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// the usual port is taken (a second copy of the app): take any free one
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return Info{}, nil, err
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", Handler(srv, tok))
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go hs.Serve(ln)
	info := Info{URL: fmt.Sprintf("http://%s/mcp", ln.Addr()), Token: tok}
	b, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), b, 0o600); err != nil {
		hs.Close()
		return info, nil, err
	}
	return info, func() { hs.Close() }, nil
}

// RunStdio serves the tools over stdin and stdout, for clients that launch the app.
func RunStdio(ctx context.Context, srv *mcp.Server) error {
	return srv.Run(ctx, &mcp.StdioTransport{})
}
