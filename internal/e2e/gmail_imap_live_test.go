package e2e

// The Gmail path every IMAP client has: imap.gmail.com and smtp.gmail.com with a credential. The credential here is a
// Gmail access token over XOAUTH2 (a real app password for account@fireballz.ai is not in the codex); the servers,
// folders, threading and sending are the real Gmail ones.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/store"
)

func TestGmailOverIMAPLive(t *testing.T) {
	tok := os.Getenv("HEARTH_LIVE_GOOGLE_TOKEN")
	if tok == "" {
		t.Skip("HEARTH_LIVE_GOOGLE_TOKEN not set")
	}
	e, st := newEngine(t)
	set := map[string]string{"imap_host": "imap.gmail.com", "imap_port": "993", "imap_security": "ssl", "smtp_host": "smtp.gmail.com", "smtp_port": "465",
		"smtp_security": "ssl", "auth": "xoauth2", "save_sent": "false", "sync_limit": "40", "username": "account@fireballz.ai"}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, err := e.AddAccount(ctx, mailtypes.Account{Kind: mailtypes.KindIMAP, Email: "account@fireballz.ai", Settings: set}, []byte(tok))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	waitFor(t, 120*time.Second, "first sync", func() bool { x, _ := st.Account(a.ID); return !x.LastSync.IsZero() && x.LastError == "" })
	fs, _ := st.Folders(a.ID)
	have := map[string]bool{}
	for _, f := range fs {
		have[f.Role] = true
	}
	for _, r := range []string{mailtypes.RoleInbox, mailtypes.RoleSent, mailtypes.RoleTrash, mailtypes.RoleSpam} {
		if !have[r] {
			t.Fatalf("Gmail over IMAP has no %s folder: %+v", r, fs)
		}
	}
	tag := fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
	e.UndoDelay = 500 * time.Millisecond
	if _, err := e.Send(mailtypes.Outgoing{AccountID: a.ID, To: []mailtypes.Address{{Email: "account@fireballz.ai"}}, Subject: "imap gmail " + tag, BodyText: "sent through smtp.gmail.com " + tag,
		Attachments: []mailtypes.OutAttachment{{Filename: "g.txt", ContentType: "text/plain", Data: []byte("payload " + tag)}}}); err != nil {
		t.Fatal(err)
	}
	var th mailtypes.Thread
	waitFor(t, 120*time.Second, "mail through Gmail IMAP", func() bool {
		ts, _ := st.ListThreads(store.ListQuery{Text: "gmail " + tag})
		if len(ts) > 0 {
			th = ts[0]
			return true
		}
		return false
	})
	if !th.HasAttach || !strings.Contains(th.Subject, tag) {
		t.Fatalf("thread: %+v", th)
	}
	// clean up
	ms, _ := st.Thread(th.ID)
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.RemoteID)
	}
	_ = e.Act(a.ID, mailtypes.Action{Kind: mailtypes.ActDelete, IDs: ids})
	waitOpsFor(t, st, a.ID)
}
