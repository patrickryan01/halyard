package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	_ "github.com/patrickryan01/halyard/internal/providers/local"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/importer"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	e.Factory = func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
		if a.Kind == mailtypes.KindLocal {
			return plugin.New(a, s)
		}
		return &scripted{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Stop(); cancel() })
	return e
}

const mboxText = "From a@x.test Sat Nov 21 10:00:00 2026\r\nFrom: Ana <ana@x.test>\r\nTo: me@x.test\r\nSubject: first\r\nDate: Sat, 21 Nov 2026 10:00:00 +0000\r\nMessage-ID: <1@x.test>\r\n\r\nhello one\r\n>From the start\r\n\r\nFrom b@x.test Sat Nov 21 11:00:00 2026\r\nFrom: Bo <bo@x.test>\r\nSubject: second\r\nDate: Sat, 21 Nov 2026 11:00:00 +0000\r\nMessage-ID: <2@x.test>\r\nContent-Type: multipart/mixed; boundary=zz\r\n\r\n--zz\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n--zz\r\nContent-Type: text/plain; name=n.txt\r\nContent-Disposition: attachment; filename=n.txt\r\n\r\nattached text\r\n--zz--\r\n"

func TestImportMboxExportAndAttachment(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	res, err := e.ImportMbox(ctx, "Old mail", strings.NewReader(mboxText))
	if err != nil || res.Imported != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	again, _ := e.ImportMbox(ctx, "Old mail", strings.NewReader(mboxText))
	if again.Imported != 0 || again.Skipped != 2 {
		t.Fatalf("a second import added messages: %+v", again)
	}
	acct, _ := e.ImportAccount(ctx)
	fs, _ := e.St.Folders(acct.ID)
	var remote string
	for _, f := range fs {
		if f.Name == "Old mail" {
			remote = f.RemoteID
		}
	}
	th, err := e.St.ListThreads(store.ListQuery{AccountID: acct.ID, Folder: acct.ID + "/" + remote})
	if err != nil || len(th) != 2 {
		t.Fatalf("threads %v %v", th, err)
	}
	hits, _ := e.St.ListThreads(store.ListQuery{Text: "hello one"})
	if len(hits) != 1 {
		t.Fatalf("imported mail is not searchable: %v", hits)
	}
	// the attachment of an imported message is read from the stored original
	ms, _ := e.St.Thread(hits[0].ID)
	if !strings.Contains(ms[0].BodyText, "From the start") {
		t.Fatalf("the quoted From line was not restored: %q", ms[0].BodyText)
	}
	for _, th := range th {
		m, _ := e.St.Thread(th.ID)
		if len(m[0].Attach) == 1 {
			b, _, err := e.Attachment(ctx, acct.ID, m[0].RemoteID, m[0].Attach[0].ID)
			if err != nil || strings.TrimSpace(string(b)) != "attached text" {
				t.Fatalf("attachment %q %v", b, err)
			}
		}
	}
	// export as mbox and import it again into a new folder: the same messages come back
	mb, err := e.ExportMbox(ctx, []string{th[0].ID, th[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = importer.Mbox(bytes.NewReader(mb), func([]byte) error { n++; return nil })
	if n != 2 {
		t.Fatalf("exported %d messages", n)
	}
	eml, name, err := e.ExportEML(ctx, ms[0].ID)
	if err != nil || !strings.Contains(string(eml), "Subject: first") || !strings.HasSuffix(name, ".eml") {
		t.Fatalf("%s %s %v", name, eml, err)
	}
}

func TestRulesRunOnNewMail(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prov := &scripted{}
	e := New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	e.Factory = func(mailtypes.Account, plugin.Secrets) (plugin.Provider, error) { return prov, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	r, err := e.SaveRule(mailtypes.Rule{Name: "bills", Enabled: true, Conditions: []mailtypes.RuleCondition{{Field: "subject", Op: "contains", Value: "invoice"}}, Actions: []mailtypes.RuleAction{{Kind: "move", FolderID: "Bills"}, {Kind: "star"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveRule(mailtypes.Rule{Name: "bad"}); err == nil {
		t.Fatal("an empty rule was saved")
	}
	ch, unsub := e.Subscribe()
	defer unsub()
	a, err := e.AddAccount(ctx, mailtypes.Account{ID: "a1", Kind: "imap", Email: "me@x.test"}, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, ch, "synced") // the first sync is done: what comes next is new mail
	m := msg("9", "x@y.test", time.Second, false)
	m.Subject = "Your invoice"
	prov.add(m)
	e.SyncNow(a.ID)
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-ch:
			if ev.Type == "new" {
				t.Fatalf("a message the rule moved was announced: %+v", ev)
			}
		case <-deadline:
			done = true
		}
		if !done {
			th, _ := e.St.ListThreads(store.ListQuery{AccountID: a.ID})
			if len(th) == 1 {
				if got, _ := e.St.Thread(th[0].ID); len(got) == 1 && got[0].Starred {
					done = true
				}
			}
		}
	}
	th, _ := e.St.ListThreads(store.ListQuery{AccountID: a.ID})
	if len(th) != 1 {
		t.Fatalf("threads: %v", th)
	}
	got, _ := e.St.Thread(th[0].ID)
	if len(got[0].Folders) != 1 || got[0].Folders[0] != "Bills" || !got[0].Starred {
		t.Fatalf("the rule did not move and star it: %+v", got[0])
	}
	// order is kept and can change
	r2, _ := e.SaveRule(mailtypes.Rule{Name: "second", Enabled: true, Conditions: []mailtypes.RuleCondition{{Field: "subject", Value: "x"}}, Actions: []mailtypes.RuleAction{{Kind: "read"}}})
	_ = e.St.MoveRule(r2.ID, 0)
	rs, _ := e.St.Rules()
	if rs[0].ID != r2.ID || rs[1].ID != r.ID {
		t.Fatal("rule order did not change")
	}
}

func TestTemporaryFailuresAreRetried(t *testing.T) {
	for msg, want := range map[string]bool{
		"smtp send: SMTP error 452: Rate limit exceeded, try again later.": true,
		"dial tcp 1.2.3.4:25: i/o timeout":                                 true,
		"smtp send: SMTP error 550: no such user":                          false,
		"imap login: authentication failed":                                false,
	} {
		if temporary(errors.New(msg)) != want {
			t.Errorf("%q: want %v", msg, want)
		}
	}
	e := testEngine(t)
	if err := e.St.PutAccount(mailtypes.Account{ID: "a9", Kind: "imap", Email: "x@y.test"}); err != nil {
		t.Fatal(err)
	}
	if err := e.St.QueueOutbox(store.OutboxItem{ID: "o1", AccountID: "a9", Payload: []byte("{}"), SendAt: time.Now(), Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		ok, err := e.St.DeferOutbox("o1", errors.New("452 try later"), []time.Duration{time.Minute, time.Minute, time.Minute})
		if err != nil || !ok {
			t.Fatalf("attempt %d: %v %v", i, ok, err)
		}
	}
	if ok, _ := e.St.DeferOutbox("o1", errors.New("452 try later"), []time.Duration{time.Minute, time.Minute, time.Minute}); ok {
		t.Fatal("a message kept being retried forever")
	}
	items, _ := e.St.Outbox()
	if len(items) != 1 || items[0].State != "queued" || !items[0].SendAt.After(time.Now()) {
		t.Fatalf("%+v", items)
	}
}

// flaky is a provider whose server is unreachable until up is set.
type flaky struct {
	*scripted
	mu  sync.Mutex
	up  bool
	saw []mailtypes.Action
}

func (f *flaky) Apply(_ context.Context, a mailtypes.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.up {
		return errors.New("dial tcp: connection refused")
	}
	f.saw = append(f.saw, a)
	return nil
}

func (f *flaky) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	f.mu.Lock()
	up := f.up
	f.mu.Unlock()
	if !up {
		return plugin.SyncResult{}, errors.New("dial tcp: connection refused")
	}
	return f.scripted.Sync(ctx, state)
}

// Offline, an action takes effect in the cache at once and waits in a queue; when the server is back it is replayed once.
func TestOfflineQueueReplays(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prov := &flaky{scripted: &scripted{}}
	prov.add(msg("m1", "bob@x.test", time.Minute, false))
	e := New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	e.Factory = func(mailtypes.Account, plugin.Secrets) (plugin.Provider, error) { return prov, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	prov.mu.Lock()
	prov.up = true
	prov.mu.Unlock()
	if _, err := e.AddAccount(ctx, mailtypes.Account{ID: "a1", Kind: "imap", Email: "me@x.test"}, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	waitCond(t, 10*time.Second, "the first message", func() bool { ts, _ := st.ListThreads(store.ListQuery{AccountID: "a1"}); return len(ts) == 1 })
	prov.mu.Lock()
	prov.up = false // the network goes away
	prov.mu.Unlock()
	if err := e.Act("a1", mailtypes.Action{Kind: mailtypes.ActRead, IDs: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	th, _ := st.ListThreads(store.ListQuery{AccountID: "a1"})
	ms, _ := st.Thread(th[0].ID)
	if !ms[0].Read {
		t.Fatal("the change did not show in the cache while offline")
	}
	if ops, _ := st.PendingOps("a1"); len(ops) != 1 {
		t.Fatalf("the change was not kept for later: %d queued", len(ops))
	}
	prov.mu.Lock()
	prov.up = true // and comes back
	prov.mu.Unlock()
	e.SyncNow("a1")
	waitCond(t, 15*time.Second, "the queued change to be replayed", func() bool { ops, _ := st.PendingOps("a1"); return len(ops) == 0 })
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if ops, _ := st.PendingOps("a1"); len(ops) != 0 || len(prov.saw) != 1 || prov.saw[0].Kind != mailtypes.ActRead {
		t.Fatalf("the queued change was not replayed exactly once: queued %v, server saw %+v", ops, prov.saw)
	}
}

// A message sent from an address uses that identity's signature and reply-to; an alias with no signature of its own
// sends without one, and the account's own address falls back to the account signature.
func TestIdentitySignatureAndReplyTo(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	a, err := e.AddAccount(ctx, mailtypes.Account{ID: "a2", Kind: "imap", Email: "me@x.test", Name: "Me", Signature: "Account signature"}, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.St.PutIdentity(mailtypes.Identity{AccountID: a.ID, Name: "Sales", Email: "sales@x.test", Signature: "The sales team", ReplyTo: "reply@x.test", Alias: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.St.PutIdentity(mailtypes.Identity{AccountID: a.ID, Name: "Quiet", Email: "quiet@x.test", Alias: true}); err != nil {
		t.Fatal(err)
	}
	send := func(from string) mailtypes.Outgoing {
		id, err := e.SendAt(mailtypes.Outgoing{AccountID: a.ID, From: mailtypes.Address{Email: from}, To: []mailtypes.Address{{Email: "z@y.test"}}, Subject: from, BodyText: "hello"}, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		items, _ := e.St.DueOutbox(time.Now().Add(2 * time.Hour))
		for _, it := range items {
			if it.ID == id {
				var o mailtypes.Outgoing
				_ = json.Unmarshal(it.Payload, &o)
				return o
			}
		}
		t.Fatal("not queued")
		return mailtypes.Outgoing{}
	}
	if o := send("sales@x.test"); !strings.Contains(o.BodyText, "The sales team") || strings.Contains(o.BodyText, "Account signature") || len(o.ReplyTo) != 1 || o.ReplyTo[0].Email != "reply@x.test" || o.From.Name != "Sales" {
		t.Fatalf("alias with a signature: %+v", o)
	}
	if o := send("quiet@x.test"); strings.Contains(o.BodyText, "signature") || strings.Contains(o.BodyText, "--") {
		t.Fatalf("alias without a signature: %q", o.BodyText)
	}
	if o := send("me@x.test"); !strings.Contains(o.BodyText, "Account signature") {
		t.Fatalf("own address: %q", o.BodyText)
	}
}

func TestRemoveAndUpdateAccount(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	a, err := e.AddAccount(ctx, mailtypes.Account{ID: "a3", Kind: "imap", Email: "me@x.test"}, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	a.Name, a.Signature = "New Name", "Regards"
	if err := e.St.PutAccount(a); err != nil {
		t.Fatal(err)
	}
	got, _ := e.St.Account(a.ID)
	if got.Name != "New Name" || got.Signature != "Regards" {
		t.Fatalf("%+v", got)
	}
	if err := e.RemoveAccount(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.St.Account(a.ID); err == nil {
		t.Fatal("the account is still there")
	}
}

func TestSendingToAGroupSendsToItsMembers(t *testing.T) {
	e := testEngine(t)
	ctx := context.Background()
	a, _ := e.AddAccount(ctx, mailtypes.Account{ID: "ag", Kind: "imap", Email: "me@x.test"}, []byte("pw"))
	g, err := e.SaveGroup(ctx, "", mailtypes.ContactGroup{Name: "Plant team", Members: []string{"a@x.test", "b@y.test", "A@x.test"}})
	if err != nil || len(g.Members) != 2 {
		t.Fatalf("%+v %v", g, err)
	}
	if cs := e.SearchContacts(ctx, "plant"); len(cs) == 0 || len(cs[0].Members) != 2 {
		t.Fatalf("the group is not suggested: %+v", cs)
	}
	id, err := e.SendAt(mailtypes.Outgoing{AccountID: a.ID, To: []mailtypes.Address{{Email: "Plant team"}, {Email: "c@z.test"}}, Subject: "s", BodyText: "b"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	items, _ := e.St.DueOutbox(time.Now().Add(2 * time.Hour))
	for _, it := range items {
		if it.ID == id {
			var o mailtypes.Outgoing
			_ = json.Unmarshal(it.Payload, &o)
			if len(o.To) != 3 || o.To[0].Email != "a@x.test" || o.To[2].Email != "c@z.test" {
				t.Fatalf("recipients: %+v", o.To)
			}
			if err := e.DeleteGroup(ctx, "", g.ID); err != nil {
				t.Fatal(err)
			}
			if gs, _ := e.St.Groups(); len(gs) != 0 {
				t.Fatal("not deleted")
			}
			return
		}
	}
	t.Fatal("not queued")
}

// waitCond polls a condition on a ticker until a deadline.
func waitCond(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	end := time.After(d)
	for !f() {
		select {
		case <-tick.C:
		case <-end:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// waitEvent waits for the engine to announce an event of a kind.
func waitEvent(t *testing.T, ch chan Event, kind string) {
	t.Helper()
	end := time.After(15 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == kind {
				return
			}
		case <-end:
			t.Fatalf("timed out waiting for a %s event", kind)
		}
	}
}
