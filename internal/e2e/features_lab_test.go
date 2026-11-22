package e2e

// Feature checks against the real lab server, one per completeness row (the maintainer feature list).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/ical"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/sieve"
	"github.com/patrickryan01/halyard/internal/store"

	_ "github.com/patrickryan01/halyard/internal/providers/local"
	_ "github.com/patrickryan01/halyard/internal/providers/pop3"
)

func to(email string) []mailtypes.Address { return []mailtypes.Address{{Email: email}} }

// deliver sends now and waits until the message is in the receiver's inbox cache.
func deliver(t *testing.T, from, rcv box, o mailtypes.Outgoing) mailtypes.Message {
	t.Helper()
	o.AccountID = from.acct.ID
	if _, err := from.eng.SendAfter(o, 0); err != nil {
		t.Fatal(err)
	}
	// a send that fails says why, instead of the delivery timing out
	waitFor(t, 25*time.Second, "the outbox to empty", func() bool {
		items, _ := from.st.Outbox()
		for _, it := range items {
			if it.State == "failed" {
				t.Fatalf("sending %q failed: %s", o.Subject, it.Error)
			}
		}
		return len(items) == 0
	})
	return arrive(t, rcv, o.Subject)
}

func arrive(t *testing.T, rcv box, subj string) mailtypes.Message {
	t.Helper()
	var got mailtypes.Message
	waitFor(t, 60*time.Second, "delivery of "+subj, func() bool {
		rcv.eng.SyncNow(rcv.acct.ID)
		ts, _ := rcv.st.ListThreads(store.ListQuery{AccountID: rcv.acct.ID, Text: "subject:\"" + subj + "\""})
		for _, th := range ts {
			ms, _ := rcv.st.Thread(th.ID)
			for _, m := range ms {
				if strings.Contains(m.Subject, subj) {
					got = m
					return true
				}
			}
		}
		return false
	})
	return got
}

func tag() string { return fmt.Sprint(time.Now().UnixNano() % 1e9) }

func eachKind(t *testing.T, f func(t *testing.T, kind string)) {
	host := labHost(t)
	_ = host
	for _, k := range []string{mailtypes.KindIMAP, mailtypes.KindJMAP} {
		k := k
		t.Run(k, func(t *testing.T) { f(t, k) })
	}
}

func TestFeatSearchOperatorsLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		g := tag()
		deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "ops plain " + g, BodyText: "just words zebra" + g})
		deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "ops attach " + g, BodyText: "with a file", Attachments: []mailtypes.OutAttachment{{Filename: "Quarterly-" + g + ".pdf", ContentType: "application/pdf", Data: make([]byte, 80000)}}})
		cases := []struct {
			q    string
			want string
			not  string
		}{
			{"from:alice subject:\"ops plain " + g + "\"", "ops plain", "ops attach"},
			{"has:attachment " + g, "ops attach", "ops plain"},
			{"filename:quarterly-" + g, "ops attach", "ops plain"},
			{"larger:50K " + g, "ops attach", "ops plain"},
			{"smaller:50K " + g, "ops plain", "ops attach"},
			{"is:unread " + g + " -attach", "ops plain", "ops attach"},
			{"newer_than:1d to:bob " + g, "ops plain", ""},
			{"\"just words\" zebra" + g, "ops plain", "ops attach"},
		}
		for _, c := range cases {
			ts, err := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Text: c.q})
			if err != nil {
				t.Fatal(err)
			}
			var subs []string
			for _, th := range ts {
				subs = append(subs, th.Subject)
			}
			j := strings.Join(subs, "|")
			if !strings.Contains(j, c.want) || (c.not != "" && strings.Contains(j, c.not)) {
				t.Errorf("%q gave %q (want %q, not %q)", c.q, j, c.want, c.not)
			}
		}
	})
}

func TestFeatRulesLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		g := tag()
		ctx := context.Background()
		folder, err := bob.eng.CreateFolder(ctx, bob.acct.ID, "Bills"+g)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, 30*time.Second, "folder", func() bool { fs, _ := bob.st.Folders(bob.acct.ID); return hasFolder(fs, folder) })
		if _, err := bob.eng.SaveRule(mailtypes.Rule{Name: "bills" + g, Enabled: true, AccountID: bob.acct.ID, Conditions: []mailtypes.RuleCondition{{Field: "subject", Op: "contains", Value: "invoice" + g}},
			Actions: []mailtypes.RuleAction{{Kind: "move", FolderID: folder}, {Kind: "read"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := alice.eng.SendAfter(mailtypes.Outgoing{AccountID: alice.acct.ID, To: to("bob@lab.test"), Subject: "your invoice" + g, BodyText: "pay"}, 0); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 60*time.Second, "the rule to move the message", func() bool {
			ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Folder: bob.acct.ID + "/" + folder})
			return len(ts) == 1
		})
		waitOps(t, bob)
		// the server has it in the folder, read: a new client sees that
		fresh := addUser(t, kind, host, "bob")
		waitFor(t, 45*time.Second, "fresh client", func() bool {
			fresh.eng.SyncNow(fresh.acct.ID)
			ts, _ := fresh.st.ListThreads(store.ListQuery{AccountID: fresh.acct.ID, Folder: fresh.acct.ID + "/" + folder})
			if len(ts) != 1 {
				return false
			}
			ms, _ := fresh.st.Thread(ts[0].ID)
			return len(ms) == 1 && ms[0].Read
		})
		in, _ := fresh.st.ListThreads(store.ListQuery{AccountID: fresh.acct.ID, Folder: "role:inbox", Text: "invoice" + g})
		if len(in) != 0 {
			t.Fatal("the message is still in the inbox")
		}
	})
}

func hasFolder(fs []mailtypes.Folder, remote string) bool {
	for _, f := range fs {
		if f.RemoteID == remote {
			return true
		}
	}
	return false
}

func TestFeatScheduledSendAndUndoLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		g := tag()
		id, err := alice.eng.SendAt(mailtypes.Outgoing{AccountID: alice.acct.ID, To: to("bob@lab.test"), Subject: "later " + g, BodyText: "scheduled"}, time.Now().Add(9*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		items, _ := alice.st.Outbox()
		if len(items) != 1 || items[0].SendAt.Before(time.Now().Add(5*time.Second)) {
			t.Fatalf("outbox: %+v", items)
		}
		syncAndWait(t, bob) // a whole sync pass has run, and the scheduled message has not been sent
		if left, _ := alice.st.Outbox(); len(left) != 1 || left[0].State != "queued" {
			t.Fatalf("the scheduled message left early: %+v", left)
		}
		if ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Text: "later " + g}); len(ts) != 0 {
			t.Fatal("a scheduled message arrived early")
		}
		arrive(t, bob, "later "+g)
		_ = id
		// a scheduled message can be cancelled until its time
		id2, _ := alice.eng.SendAt(mailtypes.Outgoing{AccountID: alice.acct.ID, To: to("bob@lab.test"), Subject: "cancelled " + g, BodyText: "no"}, time.Now().Add(30*time.Second))
		if ok, err := alice.eng.Undo(id2); !ok || err != nil {
			t.Fatalf("cancel a scheduled message: %v %v", ok, err)
		}
	})
}

func TestFeatInviteRSVPLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		g := tag()
		ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\nBEGIN:VEVENT\r\nUID:inv-" + g + "@lab\r\nDTSTART:20261203T140000Z\r\nDTEND:20261203T150000Z\r\nSUMMARY:Review " + g + "\r\nORGANIZER;CN=Alice:mailto:alice@lab.test\r\nATTENDEE;CN=Bob;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:bob@lab.test\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
		m := deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "Invitation " + g, BodyText: "join us",
			Attachments: []mailtypes.OutAttachment{{Filename: "invite.ics", ContentType: "text/calendar; method=REQUEST; charset=utf-8", Data: []byte(ics)}}})
		ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Text: "has:invite " + g})
		if len(ts) != 1 {
			t.Fatalf("has:invite found %d", len(ts))
		}
		ctx := context.Background()
		inv, err := bob.eng.Invite(ctx, m.ID)
		if err != nil || inv.Method != "REQUEST" || inv.Events[0].Summary != "Review "+g || inv.Me == nil || inv.Me.PartStat != "NEEDS-ACTION" {
			t.Fatalf("invite: %+v %v", inv, err)
		}
		if _, err := bob.eng.RespondInvite(ctx, m.ID, "ACCEPTED"); err != nil {
			t.Fatal(err)
		}
		reply := arrive(t, alice, "Accepted: Review "+g)
		ri, err := alice.eng.Invite(ctx, reply.ID)
		if err != nil || ri.Method != "REPLY" || len(ri.Events[0].Attendees) != 1 || ri.Events[0].Attendees[0].PartStat != "ACCEPTED" || ri.Events[0].Attendees[0].Email != "bob@lab.test" {
			t.Fatalf("reply: %+v %v", ri, err)
		}
		if _, err := alice.eng.RespondInvite(ctx, reply.ID, "ACCEPTED"); err == nil {
			t.Fatal("a reply can be answered")
		}
		// the invitation can be saved to a calendar on the server
		if err := bob.eng.SetDAV(ctx, bob.acct.ID, "https://"+host+":18443", "bob@lab.test", "labpass-bob"); err != nil {
			t.Fatal(err)
		}
		ev, err := bob.eng.AddInviteToCalendar(ctx, m.ID, "")
		if err != nil || ev.URL == "" {
			t.Fatalf("add to calendar: %+v %v", ev, err)
		}
		evs, err := bob.eng.Events(ctx, bob.acct.ID, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC))
		found := false
		for _, e := range evs {
			if e.Summary == "Review "+g {
				found = true
			}
		}
		if err != nil || !found {
			t.Fatalf("the saved event is not in the calendar: %+v %v", evs, err)
		}
		_ = bob.eng.DeleteEvent(ctx, bob.acct.ID, ev.URL, "")
	})
}

func TestFeatVacationLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		bob := addUser(t, kind, host, "bob")
		ctx := context.Background()
		if kind == mailtypes.KindIMAP { // the lab maps ManageSieve to 14190
			a, _ := bob.st.Account(bob.acct.ID)
			a.Settings["sieve_port"] = "14190"
			_ = bob.st.PutAccount(a)
		}
		v := mailtypes.Vacation{Enabled: true, Subject: "Away " + tag(), Body: "I am away until Monday.\nSee you then.", Start: time.Now().Add(-time.Hour).UTC().Truncate(time.Second), End: time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)}
		if err := bob.eng.SetVacation(ctx, bob.acct.ID, v); err != nil {
			t.Fatal(err)
		}
		got, err := bob.eng.GetVacation(ctx, bob.acct.ID)
		if err != nil || !got.Enabled || got.Subject != v.Subject || !strings.Contains(got.Body, "away until Monday") {
			t.Fatalf("read back: %+v %v", got, err)
		}
		if !got.End.Equal(v.End) {
			t.Fatalf("end date: %v want %v", got.End, v.End)
		}
		if err := bob.eng.SetVacation(ctx, bob.acct.ID, mailtypes.Vacation{}); err != nil {
			t.Fatal(err)
		}
		got, _ = bob.eng.GetVacation(ctx, bob.acct.ID)
		if got.Enabled {
			t.Fatal("still on")
		}
	})
}

func TestFeatManageSieveLab(t *testing.T) {
	host := labHost(t)
	bob := addUser(t, mailtypes.KindIMAP, host, "bob")
	ctx := context.Background()
	// the lab maps ManageSieve to 14190
	a, _ := bob.st.Account(bob.acct.ID)
	a.Settings["sieve_port"], a.Settings["sieve_host"] = "14190", host
	_ = bob.st.PutAccount(a)
	name := "halyard-test-" + tag()
	script := "require [\"fileinto\"];\nif header :contains \"subject\" \"lab\" { fileinto \"Labs\"; }\n"
	if err := bob.eng.SieveCheck(ctx, bob.acct.ID, script); err != nil {
		t.Fatalf("a valid script failed the check: %v", err)
	}
	if err := bob.eng.SieveCheck(ctx, bob.acct.ID, "this is not sieve ;;"); err == nil {
		t.Fatal("a broken script passed the check")
	}
	if err := bob.eng.SievePut(ctx, bob.acct.ID, name, script, true); err != nil {
		t.Fatal(err)
	}
	ss, err := bob.eng.SieveScripts(ctx, bob.acct.ID)
	active := false
	for _, s := range ss {
		if s.Name == name && s.Active {
			active = true
		}
	}
	if err != nil || !active {
		t.Fatalf("scripts: %+v %v", ss, err)
	}
	got, err := bob.eng.SieveScript(ctx, bob.acct.ID, name)
	if err != nil || !strings.Contains(got, "fileinto") {
		t.Fatalf("read back: %q %v", got, err)
	}
	if err := bob.eng.SievePut(ctx, bob.acct.ID, name+"x", "garbage ;;", false); err == nil {
		t.Fatal("the server stored a broken script")
	}
	if err := bob.eng.SieveActivate(ctx, bob.acct.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := bob.eng.SieveDelete(ctx, bob.acct.ID, name); err != nil {
		t.Fatal(err)
	}
	_ = sieve.Config{}
}

func TestFeatFoldersAndBulkLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		ctx := context.Background()
		g := tag()
		name := "Proj" + g
		id, err := bob.eng.CreateFolder(ctx, bob.acct.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, 30*time.Second, "created", func() bool { fs, _ := bob.st.Folders(bob.acct.ID); return hasFolder(fs, id) })
		if err := bob.eng.RenameFolder(ctx, bob.acct.ID, id, name+"-renamed"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 30*time.Second, "renamed", func() bool {
			fs, _ := bob.st.Folders(bob.acct.ID)
			for _, f := range fs {
				if f.Name == name+"-renamed" {
					id = f.RemoteID
					return true
				}
			}
			return false
		})
		if err := bob.eng.RenameFolder(ctx, bob.acct.ID, mustRole(t, bob, mailtypes.RoleInbox), "Mine"); err == nil {
			t.Fatal("the inbox was renamed")
		}
		for i := 0; i < 3; i++ {
			deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: fmt.Sprintf("bulk %s %d", g, i), BodyText: "x"})
		}
		n, err := bob.eng.MarkAllRead(bob.acct.ID, mustRole(t, bob, mailtypes.RoleInbox))
		if err != nil || n < 3 {
			t.Fatalf("mark all read: %d %v", n, err)
		}
		waitOps(t, bob)
		fresh := addUser(t, kind, host, "bob")
		waitFor(t, 45*time.Second, "server marked read", func() bool {
			ts, _ := fresh.st.ListThreads(store.ListQuery{AccountID: fresh.acct.ID, Folder: "role:inbox", Unread: true})
			return len(ts) == 0
		})
		// trash three, empty the trash, and they are gone for good
		ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Text: "bulk " + g})
		var ids []string
		for _, th := range ts {
			ids = append(ids, th.ID)
		}
		if err := bob.eng.ActThreads(mailtypes.ActTrash, ids, "", ""); err != nil {
			t.Fatal(err)
		}
		waitOps(t, bob)
		waitFor(t, 45*time.Second, "in trash", func() bool {
			bob.eng.SyncNow(bob.acct.ID)
			ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Folder: "role:trash"})
			return len(ts) >= 3
		})
		if _, err := bob.eng.EmptyFolder(bob.acct.ID, mailtypes.RoleTrash); err != nil {
			t.Fatal(err)
		}
		waitOps(t, bob)
		waitFor(t, 45*time.Second, "trash empty", func() bool {
			bob.eng.SyncNow(bob.acct.ID)
			ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Folder: "role:trash"})
			return len(ts) == 0
		})
		if err := bob.eng.DeleteFolder(ctx, bob.acct.ID, id); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 30*time.Second, "deleted", func() bool { fs, _ := bob.st.Folders(bob.acct.ID); return !hasFolder(fs, id) })
	})
}

func mustRole(t *testing.T, b box, role string) string {
	t.Helper()
	id, ok := b.st.FolderByRole(b.acct.ID, role)
	if !ok {
		t.Fatalf("no %s folder", role)
	}
	return id
}

func TestFeatUnsubscribeAndReceiptLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		ctx := context.Background()
		g := tag()
		// one click: an https endpoint that records the POST
		got := make(chan string, 1)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := make([]byte, 64)
			n, _ := r.Body.Read(b)
			got <- r.Method + " " + string(b[:n])
		}))
		defer srv.Close()
		old := engine.HTTPClient
		engine.HTTPClient = srv.Client()
		defer func() { engine.HTTPClient = old }()
		m := deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "news " + g, BodyText: "buy", ReceiptRequested: true,
			ExtraHeaders: map[string]string{"List-Unsubscribe": "<" + srv.URL + "/u/1>, <mailto:alice@lab.test?subject=unsubscribe%20" + g + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}})
		if m.ListUnsubscribe == "" || !m.ListUnsubscribePost || m.ReceiptTo == "" {
			t.Fatalf("headers did not survive: %+v", m)
		}
		if ts, _ := bob.st.ListThreads(store.ListQuery{AccountID: bob.acct.ID, Text: "has:unsubscribe " + g}); len(ts) != 1 {
			t.Fatal("has:unsubscribe did not find it")
		}
		res, err := bob.eng.Unsubscribe(ctx, m.ID)
		if err != nil || !res.Done || res.Method != "one-click" {
			t.Fatalf("%+v %v", res, err)
		}
		select {
		case s := <-got:
			if s != "POST List-Unsubscribe=One-Click" {
				t.Fatalf("the sender got %q", s)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the sender was not told")
		}
		// without the one click header the mailto goes out
		m2 := deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "news2 " + g, BodyText: "buy",
			ExtraHeaders: map[string]string{"List-Unsubscribe": "<mailto:alice@lab.test?subject=unsubscribe%20" + g + ">"}})
		res, err = bob.eng.Unsubscribe(ctx, m2.ID)
		if err != nil || !res.Done || res.Method != "mailto" {
			t.Fatalf("%+v %v", res, err)
		}
		arrive(t, alice, "unsubscribe "+g)
		// the read receipt
		if _, err := bob.eng.SendReceipt(ctx, m.ID); err != nil {
			t.Fatal(err)
		}
		arrive(t, alice, "Read: news "+g)
	})
}

func TestFeatExportImportLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		g := tag()
		m := deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "keep me " + g, BodyText: "archive text " + g,
			Attachments: []mailtypes.OutAttachment{{Filename: "k.txt", ContentType: "text/plain", Data: []byte("kept " + g)}}})
		ctx := context.Background()
		eml, name, err := bob.eng.ExportEML(ctx, m.ID)
		if err != nil || !strings.Contains(string(eml), "keep me "+g) || !strings.HasSuffix(name, ".eml") {
			t.Fatalf("%v %s", err, name)
		}
		mb, err := bob.eng.ExportMbox(ctx, []string{m.ThreadID})
		if err != nil || !strings.HasPrefix(string(mb), "From ") {
			t.Fatalf("mbox: %v", err)
		}
		// into a fresh client's local account, then searched and its attachment read
		other, _ := newEngine(t)
		res, err := other.ImportMbox(ctx, "Restored", strings.NewReader(string(mb)))
		if err != nil || res.Imported != 1 {
			t.Fatalf("%+v %v", res, err)
		}
		ts, _ := other.St.ListThreads(store.ListQuery{Text: "archive text " + g})
		if len(ts) != 1 {
			t.Fatalf("imported mail not found: %v", ts)
		}
		ms, _ := other.St.Thread(ts[0].ID)
		if len(ms[0].Attach) != 1 {
			t.Fatalf("attachments: %+v", ms[0].Attach)
		}
		b, _, err := other.Attachment(ctx, ms[0].AccountID, ms[0].RemoteID, ms[0].Attach[0].ID)
		if err != nil || string(b) != "kept "+g {
			t.Fatalf("attachment: %q %v", b, err)
		}
	})
}

func TestFeatSecurityLab(t *testing.T) {
	eachKind(t, func(t *testing.T, kind string) {
		host := labHost(t)
		alice, bob := addUser(t, kind, host, "alice"), addUser(t, kind, host, "bob")
		ctx := context.Background()
		for _, proto := range []string{"pgp", "smime"} {
			g := tag()
			for _, p := range []struct {
				b    box
				name string
			}{{alice, "Alice"}, {bob, "Bob"}} {
				if err := p.b.eng.MakeKey(proto, p.name, strings.ToLower(p.name)+"@lab.test"); err != nil {
					t.Fatal(err)
				}
			}
			// each side gives the other its public half, as a person would send a key file
			for _, pair := range [][2]box{{alice, bob}, {bob, alice}} {
				kr, _ := pair[0].eng.Keyring()
				id := ""
				if proto == "pgp" {
					id = kr.PGP[0].ID
				} else {
					id = kr.SMIME[0].ID
				}
				txt, _, err := pair[0].eng.ExportKey(id)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pair[1].eng.ImportKey(proto, []byte(txt), ""); err != nil {
					t.Fatal(err)
				}
			}
			// signed and encrypted
			m := deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "secret " + proto + " " + g, BodyText: "the plan is " + g, Protect: proto, Sign: true, Encrypt: true})
			if strings.Contains(m.BodyText+m.BodyHTML, g) {
				t.Fatalf("%s: the server copy is readable", proto)
			}
			v, err := bob.eng.Security(ctx, m.ID)
			if err != nil || !v.Decrypted || !v.Valid || v.Signer.Email != "alice@lab.test" || !v.SignerMatchesFrom || !strings.Contains(v.Text, "the plan is "+g) {
				t.Fatalf("%s: %+v %v", proto, v, err)
			}
			// signed only
			m = deliver(t, alice, bob, mailtypes.Outgoing{To: to("bob@lab.test"), Subject: "signed " + proto + " " + g, BodyText: "open text " + g, Protect: proto, Sign: true})
			v, err = bob.eng.Security(ctx, m.ID)
			if err != nil || !v.Valid || !v.Trusted || v.Encrypted || !strings.Contains(v.Text, "open text "+g) {
				t.Fatalf("%s signed: %+v %v", proto, v, err)
			}
			// a recipient with no key is named, nothing is sent
			if _, err := alice.eng.SendAfter(mailtypes.Outgoing{AccountID: alice.acct.ID, To: to("nokey@lab.test"), Subject: "x", BodyText: "y", Protect: proto, Encrypt: true}, 0); err == nil || !strings.Contains(err.Error(), "nokey@lab.test") {
				t.Fatalf("%s: %v", proto, err)
			}
		}
	})
}

func TestFeatCalDAVCardDAVLab(t *testing.T) {
	host := labHost(t)
	alice := addUser(t, mailtypes.KindIMAP, host, "alice")
	ctx := context.Background()
	if err := alice.eng.SetDAV(ctx, alice.acct.ID, "https://"+host+":18443", "alice@lab.test", "labpass-alice"); err != nil {
		t.Fatalf("the server did not accept the calendar settings: %v", err)
	}
	cals, err := alice.eng.Calendars(ctx, alice.acct.ID)
	if err != nil || len(cals) == 0 {
		t.Fatalf("calendars: %+v %v", cals, err)
	}
	g := tag()
	start := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Minute)
	ev, err := alice.eng.SaveEvent(ctx, alice.acct.ID, cals[0].URL, engine.CalEvent{Event: icalEvent("Standup "+g, start, time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := alice.eng.Events(ctx, alice.acct.ID, time.Now(), time.Now().AddDate(0, 0, 10))
	found := false
	for _, e := range evs {
		if e.Summary == "Standup "+g && e.Start.Equal(start) {
			found = true
		}
	}
	if err != nil || !found {
		t.Fatalf("events: %+v %v", evs, err)
	}
	ev.Summary = "Standup moved " + g
	if _, err := alice.eng.SaveEvent(ctx, alice.acct.ID, cals[0].URL, ev); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := alice.eng.DeleteEvent(ctx, alice.acct.ID, ev.URL, ""); err != nil {
		t.Fatal(err)
	}
	// contacts
	g0 := g
	c, err := alice.eng.CreateContact(ctx, alice.acct.ID, mailtypes.Contact{Name: "Dana Lee " + g, Email: "dana" + g + "@y.test", Phone: "555 0101"})
	if err != nil {
		t.Fatal(err)
	}
	book, err := alice.eng.AddressBook(ctx, alice.acct.ID)
	has := false
	for _, b := range book {
		if b.Email == "dana"+g+"@y.test" && b.Phone == "555 0101" {
			has = true
		}
	}
	if err != nil || !has {
		t.Fatalf("address book: %+v %v", book, err)
	}
	c.Phone = "555 0202"
	if _, err := alice.eng.UpdateContact(ctx, alice.acct.ID, c); err != nil {
		t.Fatal(err)
	}
	book, _ = alice.eng.AddressBook(ctx, alice.acct.ID)
	for _, b := range book {
		if b.Email == "dana"+g+"@y.test" && b.Phone != "555 0202" {
			t.Fatalf("update not kept: %+v", b)
		}
	}
	if err := alice.eng.DeleteContact(ctx, alice.acct.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	// a group is kept on the server as a group card, and comes back from there
	grp, err := alice.eng.SaveGroup(ctx, alice.acct.ID, mailtypes.ContactGroup{Name: "Crew " + g0, Members: []string{"x@y.test", "z@y.test"}})
	if err != nil || !strings.HasPrefix(grp.ID, "dav:") {
		t.Fatalf("group: %+v %v", grp, err)
	}
	if err := alice.st.DeleteGroup(grp.ID); err != nil {
		t.Fatal(err)
	}
	alice.eng.SyncNow(alice.acct.ID)
	waitFor(t, 30*time.Second, "the group to come back from the server", func() bool {
		gs, _ := alice.st.Groups()
		for _, x := range gs {
			if x.Name == "Crew "+g0 && len(x.Members) == 2 {
				return true
			}
		}
		return false
	})
	if err := alice.eng.DeleteGroup(ctx, alice.acct.ID, grp.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFeatPOP3Lab(t *testing.T) {
	host := labHost(t)
	alice := addUser(t, mailtypes.KindIMAP, host, "alice")
	e, st := newEngine(t)
	ctx := context.Background()
	a, err := e.AddAccount(ctx, mailtypes.Account{Kind: mailtypes.KindPOP3, Email: "bob@lab.test", Settings: map[string]string{"pop3_host": host, "pop3_port": "18995", "pop3_security": "ssl",
		"smtp_host": host, "smtp_port": "18465", "smtp_security": "ssl", "insecure_skip_verify": "true"}}, []byte("labpass-bob"))
	if err != nil {
		t.Fatal(err)
	}
	g := tag()
	if _, err := alice.eng.SendAfter(mailtypes.Outgoing{AccountID: alice.acct.ID, To: to("bob@lab.test"), Subject: "pop " + g, BodyText: "over pop3"}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 60*time.Second, "the message by POP3", func() bool {
		e.SyncNow(a.ID)
		ts, _ := st.ListThreads(store.ListQuery{AccountID: a.ID, Text: "pop " + g})
		return len(ts) == 1
	})
	syncAndWaitOn(t, e, a.ID) // a whole second pass has run
	if ts, _ := st.ListThreads(store.ListQuery{AccountID: a.ID, Text: "pop " + g}); len(ts) != 1 {
		t.Fatal("a second sync made a duplicate")
	}
	// sending through the POP3 account's SMTP works
	if _, err := e.SendAfter(mailtypes.Outgoing{AccountID: a.ID, To: to("alice@lab.test"), Subject: "from pop " + g, BodyText: "reply"}, 0); err != nil {
		t.Fatal(err)
	}
	arrive(t, alice, "from pop "+g)
}

func icalEvent(summary string, start time.Time, d time.Duration) ical.Event {
	return ical.Event{Summary: summary, Start: start, End: start.Add(d)}
}

// syncAndWait asks for a sync and waits for the engine to say the pass is done.
func syncAndWait(t *testing.T, b box) { t.Helper(); syncAndWaitOn(t, b.eng, b.acct.ID) }

func syncAndWaitOn(t *testing.T, e *engine.Engine, account string) {
	t.Helper()
	ch, unsub := e.Subscribe()
	defer unsub()
	e.SyncNow(account)
	timeout := time.After(60 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "synced" && ev.AccountID == account {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for a sync pass to finish")
		}
	}
}
