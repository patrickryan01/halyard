package e2e

// The MCP server tested the way a real client uses it: a real MCP client (the
// official Go SDK client) connects over streamable HTTP with the bearer token
// and drives every kind of tool against live backends: the Podman IMAP lab, a
// real Gmail mailbox, and a real Microsoft 365 mailbox.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/patrickryan01/halyard/internal/api"
	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/mcpserver"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/providers/google"
	"github.com/patrickryan01/halyard/internal/providers/microsoft"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

type rig struct {
	t   *testing.T
	eng *engine.Engine
	st  *store.Store
	cs  *mcp.ClientSession
	ctx context.Context
	url string
	tok string
}

func newRig(t *testing.T, factory func(mailtypes.Account, plugin.Secrets) (plugin.Provider, error)) *rig {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(st, &secret.Store{Dir: filepath.Join(dir, "s"), NoKeyring: true})
	if factory != nil {
		e.Factory = factory
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	a := api.New(e, "test", nil)
	srv := mcpserver.New(e, a, "halyard", "test")
	info, stop, err := mcpserver.Serve(dir, "127.0.0.1:0", srv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(); cancel(); e.Stop(); st.Close() })
	// unauthorized without the token
	resp, err := http.Post(info.URL, "application/json", strings.NewReader(`{}`))
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("endpoint must demand the token: %v %v", resp, err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: info.URL, HTTPClient: &http.Client{Transport: bearer{info.Token}}}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return &rig{t: t, eng: e, st: st, cs: cs, ctx: ctx, url: info.URL, tok: info.Token}
}

func (r *rig) call(name string, args map[string]any) map[string]any {
	r.t.Helper()
	res, err := r.cs.CallTool(r.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		r.t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg += tc.Text
			}
		}
		r.t.Fatalf("%s returned an error: %s", name, msg)
	}
	b, _ := json.Marshal(res.StructuredContent)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func (r *rig) callErr(name string, args map[string]any) bool {
	res, err := r.cs.CallTool(r.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	return err != nil || res.IsError
}

func list(m map[string]any, k string) []map[string]any {
	var out []map[string]any
	if a, ok := m[k].([]any); ok {
		for _, x := range a {
			if mm, ok := x.(map[string]any); ok {
				out = append(out, mm)
			}
		}
	}
	return out
}

func (r *rig) waitThread(account, query string, want int) map[string]any {
	r.t.Helper()
	var found map[string]any
	waitFor(r.t, 120*time.Second, "thread for "+query, func() bool {
		th := list(r.call("search_threads", map[string]any{"query": query, "accountId": account}), "threads")
		if len(th) > 0 && (want == 0 || int(th[0]["count"].(float64)) >= want) {
			found = th[0]
			return true
		}
		return false
	})
	return found
}

var wantTools = []string{"list_accounts", "add_account_password", "start_oauth_signin", "finish_oauth_signin", "update_account", "remove_account", "sync_now",
	"list_folders", "create_folder", "list_threads", "search_threads", "get_thread", "get_message", "get_attachment",
	"send_message", "reply_message", "forward_message", "cancel_send", "list_outbox", "retry_send",
	"create_draft", "update_draft", "list_drafts", "get_draft", "send_draft", "delete_draft",
	"thread_action", "move_threads", "label_threads", "unlabel_threads", "snooze_threads",
	"search_contacts", "create_contact", "update_contact", "delete_contact", "get_settings", "update_settings"}

func (r *rig) checkTools() {
	r.t.Helper()
	res, err := r.cs.ListTools(r.ctx, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range res.Tools {
		have[tl.Name] = true
		if tl.Annotations != nil && tl.Annotations.ReadOnlyHint {
			r.t.Fatalf("tool %s is marked read only", tl.Name)
		}
	}
	for _, n := range wantTools {
		if !have[n] {
			r.t.Fatalf("tool %s is missing", n)
		}
	}
	r.t.Logf("MCP tools: %d", len(res.Tools))
}

// fullFlow drives every tool class against one live account. self is an address
// that delivers into the same mailbox the account reads. label says whether the
// provider uses labels (Gmail) so the label step knows what to assert.
func (r *rig) fullFlow(acct, self, kind string) {
	t := r.t
	r.checkTools()
	accts := list(r.call("list_accounts", nil), "accounts")
	if len(accts) == 0 {
		t.Fatal("list_accounts is empty")
	}
	fs := list(r.call("list_folders", map[string]any{"accountId": acct}), "folders")
	if len(fs) < 3 {
		t.Fatalf("folders: %v", fs)
	}
	tag := fmt.Sprintf("mcp%d", time.Now().UnixNano()%1e9)
	payload := base64.StdEncoding.EncodeToString([]byte("mcp payload " + tag))

	// send with an attachment, immediately, and find it again by full text search
	sent := r.call("send_message", map[string]any{"accountId": acct, "to": []string{self}, "subject": "mcp proof " + tag, "bodyText": "invoice proof " + tag,
		"bodyHtml": "<p>invoice <b>proof</b> " + tag + "</p>", "immediate": true,
		"attachments": []map[string]any{{"filename": "mcp.txt", "contentType": "text/plain", "base64": payload}}})
	if sent["outboxId"] == nil {
		t.Fatalf("send: %v", sent)
	}
	th := r.waitThread("", "invoice "+tag, 0)
	tid := th["id"].(string)
	var mid string
	var attID string
	// a provider may report the message a moment before its attachment is final;
	// the next sync carries the update, so wait for it
	waitFor(t, 90*time.Second, "attachment on the received message", func() bool {
		r.call("sync_now", nil)
		for _, m := range list(r.call("get_thread", map[string]any{"threadId": tid}), "messages") {
			for _, a := range list(m, "attachments") {
				if a["filename"] == "mcp.txt" {
					mid, _ = m["id"].(string)
					attID, _ = a["id"].(string)
					return true
				}
			}
		}
		return false
	})
	msgs := list(r.call("get_thread", map[string]any{"threadId": tid}), "messages")
	if mid == "" {
		t.Fatalf("attachment not listed on the received message: %v", msgs)
	}
	att := r.call("get_attachment", map[string]any{"messageId": mid, "attachmentId": attID})
	if att["base64"] != payload {
		t.Fatalf("attachment bytes differ: %v", att)
	}
	saved := filepath.Join(t.TempDir(), "saved", "mcp.txt")
	r.call("get_attachment", map[string]any{"messageId": mid, "attachmentId": attID, "saveTo": saved})
	if b, _ := os.ReadFile(saved); string(b) != "mcp payload "+tag {
		t.Fatalf("saveTo wrote %q", b)
	}

	// flags: star and read, verified through get_thread
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "star"})
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "read"})
	for _, m := range list(r.call("get_thread", map[string]any{"threadId": tid}), "messages") {
		if m["starred"] != true || m["read"] != true {
			t.Fatalf("flags after star+read: %v", m)
		}
	}

	// reply keeps the thread; forward adds the attachment
	r.call("reply_message", map[string]any{"messageId": mid, "bodyText": "reply from the mcp test " + tag, "immediate": true})
	r.waitThread("", "invoice "+tag, 2)
	r.call("forward_message", map[string]any{"messageId": mid, "to": []string{self}, "bodyText": "forwarding " + tag, "immediate": true})
	fwd := r.waitThread("", "subject:Fwd "+tag, 0)
	if fwd["hasAttachments"] != true {
		t.Fatalf("forward lost its attachment: %v", fwd)
	}

	// drafts: create, list, update, send, and undo of a queued send
	d := r.call("create_draft", map[string]any{"accountId": acct, "to": []string{self}, "subject": "mcp draft " + tag, "bodyText": "draft body"})
	did := d["draftId"].(string)
	if len(list(r.call("list_drafts", nil), "drafts")) == 0 {
		t.Fatal("draft not listed")
	}
	r.call("update_draft", map[string]any{"draftId": did, "accountId": acct, "to": []string{self}, "subject": "mcp draft sent " + tag, "bodyText": "draft body 2"})
	q := r.call("send_draft", map[string]any{"draftId": did})
	r.waitThread("", "subject:\"mcp draft sent\" "+tag, 0)
	_ = q
	r.eng.UndoDelay = 20 * time.Second
	queued := r.call("send_message", map[string]any{"accountId": acct, "to": []string{self}, "subject": "mcp undone " + tag, "bodyText": "never sent"})
	if u := r.call("cancel_send", map[string]any{"outboxId": queued["outboxId"]}); u["undone"] != true {
		t.Fatalf("undo: %v", u)
	}

	// snooze hides from the inbox and shows under snoozed
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "untrash"})
	r.call("snooze_threads", map[string]any{"threadIds": []string{tid}, "until": time.Now().Add(2 * time.Hour).Format(time.RFC3339)})
	if !hasThread(r.call("list_threads", map[string]any{"folder": "snoozed"}), tid) {
		t.Fatal("snoozed thread not under snoozed")
	}
	if hasThread(r.call("list_threads", map[string]any{"folder": "role:inbox", "limit": 200}), tid) {
		t.Fatal("snoozed thread still in the inbox")
	}
	r.call("snooze_threads", map[string]any{"threadIds": []string{tid}})

	// folders: create one and put the conversation in it
	fname := "mcp-" + tag
	nf := r.call("create_folder", map[string]any{"accountId": acct, "name": fname})
	rid, _ := nf["remoteId"].(string)
	if rid == "" {
		t.Fatalf("create_folder: %v", nf)
	}
	if kind == mailtypes.KindGoogle {
		r.call("label_threads", map[string]any{"threadIds": []string{tid}, "accountId": acct, "folderId": rid})
		r.call("unlabel_threads", map[string]any{"threadIds": []string{tid}, "accountId": acct, "folderId": rid})
	} else {
		r.call("move_threads", map[string]any{"threadIds": []string{tid}, "accountId": acct, "folderId": rid})
		waitFor(t, 60*time.Second, "move to reach the cache", func() bool {
			return hasThread(r.call("list_threads", map[string]any{"folder": acct + "/" + rid}), tid)
		})
	}

	// archive, then trash, then delete for good
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "archive"})
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "trash"})

	// contacts: create, find, update, delete
	cr := r.call("create_contact", map[string]any{"accountId": acct, "name": "MCP Test " + tag, "email": "mcp." + tag + "@example.com", "phone": "555-0100"})
	c, _ := cr["contact"].(map[string]any)
	if c == nil || c["email"] == nil {
		t.Fatalf("create_contact: %v", cr)
	}
	if len(list(r.call("search_contacts", map[string]any{"query": "mcp." + tag}), "contacts")) == 0 {
		t.Fatal("search_contacts did not find the new contact")
	}
	cid, _ := c["id"].(string)
	r.call("update_contact", map[string]any{"accountId": acct, "contactId": cid, "name": "MCP Renamed " + tag, "email": "mcp." + tag + "@example.com"})
	r.call("delete_contact", map[string]any{"accountId": acct, "contactId": cid})

	// settings round trip
	r.call("update_settings", map[string]any{"undoSeconds": "7", "theme": "dark"})
	s := r.call("get_settings", nil)
	if s["undoSeconds"] != "7" || s["theme"] != "dark" {
		t.Fatalf("settings: %v", s)
	}

	// clean up what the test made
	for _, q := range []string{"invoice " + tag, "subject:Fwd " + tag, "subject:\"mcp draft sent\" " + tag} {
		for _, th := range list(r.call("search_threads", map[string]any{"query": q}), "threads") {
			_ = r.callErr("thread_action", map[string]any{"threadIds": []string{th["id"].(string)}, "kind": "delete"})
		}
	}
	r.call("thread_action", map[string]any{"threadIds": []string{tid}, "kind": "delete"})
}

func hasThread(m map[string]any, id string) bool {
	for _, th := range list(m, "threads") {
		if th["id"] == id {
			return true
		}
	}
	return false
}

func TestMCPIMAPLab(t *testing.T) {
	host := labHost(t)
	r := newRig(t, nil)
	// add accounts through the MCP tool itself, exactly as a client would
	for _, u := range []string{"alice", "bob"} {
		a := r.call("add_account_password", map[string]any{"kind": "imap", "email": u + "@lab.test", "password": "labpass-" + u, "imapHost": host, "imapPort": 18993, "imapSecurity": "ssl",
			"smtpHost": host, "smtpPort": 18465, "smtpSecurity": "ssl", "skipVerify": true})
		if a["account"] == nil {
			t.Fatalf("add %s: %v", u, a)
		}
	}
	waitFor(t, 60*time.Second, "both accounts synced", func() bool {
		as := list(r.call("list_accounts", nil), "accounts")
		n := 0
		for _, a := range as {
			if ls, _ := a["lastSync"].(string); ls != "" && !strings.HasPrefix(ls, "0001") {
				n++
			}
		}
		return n == 2
	})
	var alice string
	for _, a := range list(r.call("list_accounts", nil), "accounts") {
		if a["email"] == "alice@lab.test" {
			alice = a["id"].(string)
		}
	}
	r.fullFlow(alice, "alice@lab.test", mailtypes.KindIMAP)
}

func TestMCPGoogleLive(t *testing.T) {
	tok := os.Getenv("HEARTH_LIVE_GOOGLE_TOKEN")
	if tok == "" {
		t.Skip("HEARTH_LIVE_GOOGLE_TOKEN not set")
	}
	r := newRig(t, func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
		return google.NewWithSource(a, oauth.Static(tok)), nil
	})
	r.eng.St.PutAccount(mailtypes.Account{ID: "tmp", Kind: mailtypes.KindGoogle, Email: "x"})
	r.eng.St.DeleteAccount("tmp")
	acct, err := r.eng.AddAccount(r.ctx, mailtypes.Account{Kind: mailtypes.KindGoogle, Settings: map[string]string{"sync_limit": "40"}}, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 120*time.Second, "first sync", func() bool { a, _ := r.st.Account(acct.ID); return !a.LastSync.IsZero() })
	r.fullFlow(acct.ID, "account@fireballz.ai", mailtypes.KindGoogle)
}

func TestMCPMicrosoftLive(t *testing.T) {
	tok := os.Getenv("HEARTH_LIVE_MS_TOKEN")
	if tok == "" {
		t.Skip("HEARTH_LIVE_MS_TOKEN not set")
	}
	r := newRig(t, func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
		return microsoft.NewWithSource(a, oauth.Static(tok)), nil
	})
	acct, err := r.eng.AddAccount(r.ctx, mailtypes.Account{Kind: mailtypes.KindMicrosoft}, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 120*time.Second, "first sync", func() bool { a, _ := r.st.Account(acct.ID); return !a.LastSync.IsZero() && a.LastError == "" })
	r.fullFlow(acct.ID, os.Getenv("HEARTH_LIVE_MS_USER"), mailtypes.KindMicrosoft)
}
