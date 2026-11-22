package engine

import (
	"bytes"
	"context"
	"errors"
	_ "github.com/patrickryan01/halyard/internal/providers/local"
	"path/filepath"
	"strings"
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
	time.Sleep(800 * time.Millisecond)
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
