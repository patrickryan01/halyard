package e2e

// Live end to end tests against the Podman lab server (lab/stalwart.sh). They
// run the real engine, store and provider plugins; nothing is stubbed. Set
// MAIL_LAB_HOST to the host the lab listens on (default 127.0.0.1) to run them.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"

	_ "github.com/patrickryan01/halyard/internal/providers/imap"
	_ "github.com/patrickryan01/halyard/internal/providers/jmap"
)

func labHost(t *testing.T) string {
	h := os.Getenv("MAIL_LAB_HOST")
	if h == "" {
		t.Skip("MAIL_LAB_HOST not set: lab server not available")
	}
	return h
}

type box struct {
	eng  *engine.Engine
	st   *store.Store
	acct mailtypes.Account
}

func newEngine(t *testing.T) (*engine.Engine, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(st, &secret.Store{Dir: filepath.Join(dir, "s"), NoKeyring: true})
	e.UndoDelay = 1500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); e.Stop(); st.Close() })
	return e, st
}

func imapSettings(host string) map[string]string {
	return map[string]string{"imap_host": host, "imap_port": "18993", "imap_security": "ssl", "smtp_host": host, "smtp_port": "18465", "smtp_security": "ssl",
		"insecure_skip_verify": "true", "username": ""}
}

func jmapSettings(host string) map[string]string {
	return map[string]string{"jmap_url": "https://" + host + ":18443", "insecure_skip_verify": "true"}
}

func addUser(t *testing.T, kind, host, user string) box {
	t.Helper()
	e, st := newEngine(t)
	set := imapSettings(host)
	if kind == mailtypes.KindJMAP {
		set = jmapSettings(host)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, err := e.AddAccount(ctx, mailtypes.Account{Kind: kind, Email: user + "@lab.test", Settings: set}, []byte("labpass-"+user))
	if err != nil {
		t.Fatalf("add %s: %v", user, err)
	}
	waitFor(t, 60*time.Second, "first sync of "+user, func() bool {
		fs, _ := st.Folders(a.ID)
		acct, _ := st.Account(a.ID)
		return len(fs) > 0 && !acct.LastSync.IsZero()
	})
	return box{eng: e, st: st, acct: a}
}

func waitFor(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func threadBySubject(b box, folder, subj string) (mailtypes.Thread, bool) {
	ts, _ := b.st.ListThreads(store.ListQuery{AccountID: b.acct.ID, Folder: folder})
	for _, th := range ts {
		if strings.Contains(th.Subject, subj) {
			return th, true
		}
	}
	return mailtypes.Thread{}, false
}

func TestIMAPLab(t *testing.T) { scenario(t, mailtypes.KindIMAP) }
func TestJMAPLab(t *testing.T) { scenario(t, mailtypes.KindJMAP) }

func scenario(t *testing.T, kind string) {
	host := labHost(t)
	alice := addUser(t, kind, host, "alice")
	bob := addUser(t, kind, host, "bob")
	tag := fmt.Sprintf("%s%d", kind, time.Now().UnixNano()%1e9)
	subj := "lab hello " + tag

	// 1. undo send: queue, cancel inside the window, and it never arrives
	if _, err := alice.eng.Send(mailtypes.Outgoing{AccountID: alice.acct.ID, To: []mailtypes.Address{{Email: "bob@lab.test"}}, Subject: "undone " + tag, BodyText: "should not arrive"}); err != nil {
		t.Fatal(err)
	}
	items, _ := alice.st.Outbox()
	if len(items) != 1 {
		t.Fatalf("outbox has %d", len(items))
	}
	if ok, _ := alice.eng.Undo(items[0].ID); !ok {
		t.Fatal("undo should win inside the window")
	}

	// 2. send with HTML and an attachment
	if _, err := alice.eng.Send(mailtypes.Outgoing{AccountID: alice.acct.ID, To: []mailtypes.Address{{Name: "Bob", Email: "bob@lab.test"}}, Subject: subj,
		BodyText: "hello bob, the invoice total is 4200", BodyHTML: "<p>hello bob, the <b>invoice</b> total is 4200</p>",
		Attachments: []mailtypes.OutAttachment{{Filename: "note.txt", ContentType: "text/plain", Data: []byte("attachment payload " + tag)}}}); err != nil {
		t.Fatal(err)
	}

	// 3. it arrives at bob without a manual sync: the watch (IDLE or EventSource) fires it
	waitFor(t, 45*time.Second, "delivery to bob", func() bool { _, ok := threadBySubject(bob, "role:inbox", subj); return ok })
	if _, ok := threadBySubject(bob, "role:inbox", "undone "+tag); ok {
		t.Fatal("the undone message was delivered")
	}
	th, _ := threadBySubject(bob, "role:inbox", subj)
	if th.Unread != 1 || !th.HasAttach {
		t.Fatalf("thread flags: %+v", th)
	}
	msgs, _ := bob.st.Thread(th.ID)
	m := msgs[0]
	if m.From.Email != "alice@lab.test" || !strings.Contains(m.BodyText+m.BodyHTML, "invoice") {
		t.Fatalf("body or sender: %+v", m)
	}
	var att *mailtypes.Attachment
	for i := range m.Attach {
		if m.Attach[i].Filename == "note.txt" {
			att = &m.Attach[i]
		}
	}
	if att == nil {
		t.Fatalf("attachment missing: %+v", m.Attach)
	}
	got, _, err := bob.eng.Attachment(context.Background(), bob.acct.ID, m.RemoteID, att.ID)
	if err != nil || string(got) != "attachment payload "+tag {
		t.Fatalf("attachment bytes: %q %v", got, err)
	}

	// 4. full text search
	hits, _ := bob.st.ListThreads(store.ListQuery{Text: "invoice " + tag})
	if len(hits) != 1 || hits[0].ID != th.ID {
		t.Fatalf("search: %+v", hits)
	}

	// 5. reply: threading on both sides
	if _, err := bob.eng.Send(mailtypes.Outgoing{AccountID: bob.acct.ID, To: []mailtypes.Address{{Email: "alice@lab.test"}}, Subject: "Re: " + subj, BodyText: "got it, thanks",
		InReplyTo: m.MessageID, References: append(m.References, m.MessageID)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 45*time.Second, "reply threaded at alice", func() bool {
		th, ok := threadBySubject(alice, "role:inbox", subj)
		return ok && th.Count == 2
	})
	waitFor(t, 45*time.Second, "reply threaded at bob", func() bool {
		th, ok := threadBySubject(bob, "role:inbox", subj)
		_ = ok
		ms, _ := bob.st.Thread(th.ID)
		return len(ms) == 2
	})

	// 6. read, star, archive reach the server: a brand new client sees them
	rem := []string{m.RemoteID}
	if err := bob.eng.Act(bob.acct.ID, mailtypes.Action{Kind: mailtypes.ActRead, IDs: rem}); err != nil {
		t.Fatal(err)
	}
	if err := bob.eng.Act(bob.acct.ID, mailtypes.Action{Kind: mailtypes.ActStar, IDs: rem}); err != nil {
		t.Fatal(err)
	}
	waitOps(t, bob)
	fresh := addUser(t, kind, host, "bob")
	waitFor(t, 45*time.Second, "fresh client sees bob's mail", func() bool { _, ok := threadBySubject(fresh, "role:inbox", subj); return ok })
	fm := findMsg(fresh, subj, m.MessageID)
	if fm == nil || !fm.Read || !fm.Starred {
		t.Fatalf("server did not keep read and star: %+v", fm)
	}
	if err := bob.eng.Act(bob.acct.ID, mailtypes.Action{Kind: mailtypes.ActArchive, IDs: rem}); err != nil {
		t.Fatal(err)
	}
	waitOps(t, bob)
	fresh2 := addUser(t, kind, host, "bob")
	waitFor(t, 45*time.Second, "archive visible to a fresh client", func() bool {
		ts, _ := fresh2.st.ListThreads(store.ListQuery{Text: tag})
		return len(ts) > 0
	})
	in, _ := fresh2.st.ListThreads(store.ListQuery{Folder: "role:inbox"})
	for _, x := range in {
		if fm2 := findMsg(fresh2, subj, m.MessageID); fm2 != nil && x.ID == fm2.ThreadID {
			// the reply Bob sent sits in Sent, and the original left the inbox
			ms, _ := fresh2.st.Thread(x.ID)
			for _, mm := range ms {
				if mm.MessageID == m.MessageID {
					t.Fatalf("archived message still in the inbox of a fresh client")
				}
			}
		}
	}

	// 7. trash. A move gives the message a new ID on IMAP, so take its current one from bob's cache.
	waitFor(t, 45*time.Second, "bob's cache to follow the archive move", func() bool {
		ts, _ := bob.st.ListThreads(store.ListQuery{Text: tag})
		for _, x := range ts {
			ms, _ := bob.st.Thread(x.ID)
			for _, mm := range ms {
				if mm.MessageID == m.MessageID && mm.RemoteID != rem[0] {
					rem = []string{mm.RemoteID}
					return true
				}
			}
		}
		return kind == mailtypes.KindJMAP // JMAP ids survive a move
	})
	if err := bob.eng.Act(bob.acct.ID, mailtypes.Action{Kind: mailtypes.ActTrash, IDs: rem}); err != nil {
		t.Fatal(err)
	}
	waitOps(t, bob)
	fresh3 := addUser(t, kind, host, "bob")
	waitFor(t, 45*time.Second, "trash visible to a fresh client", func() bool {
		ts, _ := fresh3.st.ListThreads(store.ListQuery{Folder: "role:trash"})
		return len(ts) > 0
	})
}

func waitOps(t *testing.T, b box) {
	t.Helper()
	waitFor(t, 45*time.Second, "queued actions to reach the server", func() bool {
		ops, _ := b.st.PendingOps(b.acct.ID)
		return len(ops) == 0
	})
	time.Sleep(500 * time.Millisecond)
}

func findMsg(b box, subj, messageID string) *mailtypes.Message {
	th, ok := threadBySubject(b, "role:inbox", subj)
	if !ok {
		ts, _ := b.st.ListThreads(store.ListQuery{Text: subj})
		if len(ts) == 0 {
			return nil
		}
		th = ts[0]
	}
	ms, _ := b.st.Thread(th.ID)
	for i := range ms {
		if ms[i].MessageID == messageID {
			return &ms[i]
		}
	}
	return nil
}
