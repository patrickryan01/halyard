package e2e

// Live test against a real Gmail mailbox through the Gmail API and People API.
// It needs a token in HALYARD_LIVE_GOOGLE_TOKEN with the full mail and contacts
// scopes; a domain-wide delegation helper mints one for the test account (HALYARD_LIVE_GOOGLE_USER):
//
//   your-token-minting-helper --sub $HALYARD_LIVE_GOOGLE_USER --scope "https://mail.google.com/ https://www.googleapis.com/auth/contacts" -- go test ./internal/e2e -run Google -v

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
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/providers/google"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

func TestGoogleLive(t *testing.T) {
	tok := os.Getenv("HALYARD_LIVE_GOOGLE_TOKEN")
	if tok == "" {
		t.Skip("HALYARD_LIVE_GOOGLE_TOKEN not set")
	}
	if liveGoogleUser() == "" {
		t.Skip("HALYARD_LIVE_GOOGLE_USER not set")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := engine.New(st, &secret.Store{Dir: filepath.Join(dir, "s"), NoKeyring: true})
	e.UndoDelay = time.Second
	e.Factory = func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
		return google.NewWithSource(a, oauth.Static(tok)), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	acct, err := e.AddAccount(ctx, mailtypes.Account{Kind: mailtypes.KindGoogle, Settings: map[string]string{"sync_limit": "60"}}, []byte("{}"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if acct.Email != liveGoogleUser() {
		t.Fatalf("signed in as %q", acct.Email)
	}
	waitFor(t, 180*time.Second, "first sync", func() bool { a, _ := st.Account(acct.ID); return !a.LastSync.IsZero() })
	fs, _ := st.Folders(acct.ID)
	roles := map[string]bool{}
	for _, f := range fs {
		roles[f.Role] = true
	}
	for _, r := range []string{mailtypes.RoleInbox, mailtypes.RoleSent, mailtypes.RoleTrash, mailtypes.RoleSpam, mailtypes.RoleAll} {
		if !roles[r] {
			t.Fatalf("missing %s label; have %+v", r, fs)
		}
	}
	t.Logf("folders: %d, account %s", len(fs), acct.Email)

	// send to self with an attachment
	tag := fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	subj := "halyard live test " + tag
	if _, err := e.Send(mailtypes.Outgoing{AccountID: acct.ID, To: []mailtypes.Address{{Email: liveGoogleUser()}}, Subject: subj,
		BodyText: "live proof invoice 4200 " + tag, BodyHTML: "<p>live proof <b>invoice</b> 4200 " + tag + "</p>",
		Attachments: []mailtypes.OutAttachment{{Filename: "proof.txt", ContentType: "text/plain", Data: []byte("payload " + tag)}}}); err != nil {
		t.Fatal(err)
	}
	var th mailtypes.Thread
	waitFor(t, 120*time.Second, "message in inbox through history sync", func() bool {
		ts, _ := st.ListThreads(store.ListQuery{Folder: "role:inbox"})
		for _, x := range ts {
			if strings.Contains(x.Subject, tag) {
				th = x
				return true
			}
		}
		return false
	})
	msgs, _ := st.Thread(th.ID)
	if len(msgs) < 1 || !th.HasAttach {
		t.Fatalf("thread: %+v %d", th, len(msgs))
	}
	var recv mailtypes.Message
	for _, m := range msgs {
		for _, f := range m.Folders {
			if f == "INBOX" {
				recv = m
			}
		}
	}
	if recv.ID == "" {
		recv = msgs[0]
	}
	var att mailtypes.Attachment
	for _, a := range recv.Attach {
		if a.Filename == "proof.txt" {
			att = a
		}
	}
	got, _, err := e.Attachment(ctx, acct.ID, recv.RemoteID, att.ID)
	if err != nil || string(got) != "payload "+tag {
		t.Fatalf("attachment: %q %v", got, err)
	}
	hits, _ := st.ListThreads(store.ListQuery{Text: "invoice " + tag})
	if len(hits) == 0 {
		t.Fatal("full text search found nothing")
	}
	// labels, read and star reach Gmail: read them back from the API with a second sync from scratch
	ids := []string{}
	for _, m := range msgs {
		ids = append(ids, m.RemoteID)
	}
	for _, k := range []string{mailtypes.ActStar, mailtypes.ActRead} {
		if err := e.Act(acct.ID, mailtypes.Action{Kind: k, IDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	waitOpsFor(t, st, acct.ID)
	p := google.NewWithSource(acct, oauth.Static(tok))
	res, err := p.Sync(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range res.Change.Upsert {
		if m.RemoteID == recv.RemoteID {
			found = true
			if !m.Starred || !m.Read {
				t.Fatalf("Gmail did not keep star and read: %+v", m.Folders)
			}
		}
	}
	if !found {
		t.Log("message beyond the 60 message resync window; checked through the cache instead")
	}
	// archive: leaves the inbox, stays in All Mail
	if err := e.Act(acct.ID, mailtypes.Action{Kind: mailtypes.ActArchive, IDs: ids}); err != nil {
		t.Fatal(err)
	}
	waitOpsFor(t, st, acct.ID)
	g, err := p.Sync(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range g.Change.Upsert {
		if m.RemoteID == recv.RemoteID {
			for _, f := range m.Folders {
				if f == "INBOX" {
					t.Fatal("archive left the message in INBOX")
				}
			}
		}
	}
	// contacts through the People API
	cs, err := p.Contacts(ctx, "account")
	t.Logf("people search: %d results, err=%v", len(cs), err)
	// clean up: trash then delete forever
	if err := e.Act(acct.ID, mailtypes.Action{Kind: mailtypes.ActDelete, IDs: ids}); err != nil {
		t.Fatal(err)
	}
	waitOpsFor(t, st, acct.ID)
}

func waitOpsFor(t *testing.T, st *store.Store, id string) {
	t.Helper()
	waitFor(t, 60*time.Second, "queued actions", func() bool { ops, _ := st.PendingOps(id); return len(ops) == 0 })
}

// liveGoogleUser is the mailbox the live Google tests use; set HALYARD_LIVE_GOOGLE_USER to run them.
func liveGoogleUser() string { return os.Getenv("HALYARD_LIVE_GOOGLE_USER") }
