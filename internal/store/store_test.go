package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.PutAccount(mailtypes.Account{ID: "a1", Kind: "imap", Email: "me@x.test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFolders("a1", []mailtypes.Folder{
		{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox},
		{RemoteID: "Archive", Name: "Archive", Role: mailtypes.RoleArchive},
		{RemoteID: "Trash", Name: "Trash", Role: mailtypes.RoleTrash},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func msg(id, thread, subj, body string, ago time.Duration, folders ...string) mailtypes.Message {
	return mailtypes.Message{AccountID: "a1", RemoteID: id, ThreadID: "a1/" + thread, Subject: subj, BodyText: body,
		From: mailtypes.Address{Name: "Pat", Email: "pat@x.test"}, Date: time.Now().Add(-ago), Folders: folders}
}

func TestThreadsSearchArchiveSnooze(t *testing.T) {
	s := open(t)
	if err := s.UpsertMessages([]mailtypes.Message{
		msg("1", "t1", "Quarterly numbers", "the invoice total is 4200", 3*time.Hour, "INBOX"),
		msg("2", "t1", "Re: Quarterly numbers", "thanks, paid", 2*time.Hour, "INBOX"),
		msg("3", "t2", "Lunch", "tacos on friday", time.Hour, "INBOX"),
	}); err != nil {
		t.Fatal(err)
	}
	th, err := s.ListThreads(ListQuery{Folder: "role:inbox"})
	if err != nil || len(th) != 2 {
		t.Fatalf("threads %v %v", th, err)
	}
	if th[0].ID != "a1/t2" || th[1].Count != 2 {
		t.Fatalf("order or count wrong: %+v", th)
	}
	// full text search with prefix and a column operator
	hits, _ := s.ListThreads(ListQuery{Text: "invoi"})
	if len(hits) != 1 || hits[0].ID != "a1/t1" {
		t.Fatalf("search: %+v", hits)
	}
	hits, _ = s.ListThreads(ListQuery{Text: "from:pat tacos"})
	if len(hits) != 1 || hits[0].ID != "a1/t2" {
		t.Fatalf("operator search: %+v", hits)
	}
	// archive moves it out of the inbox, still searchable
	if err := s.ApplyLocal(mailtypes.Action{Kind: mailtypes.ActArchive, IDs: []string{"3"}}, "a1"); err != nil {
		t.Fatal(err)
	}
	th, _ = s.ListThreads(ListQuery{Folder: "role:inbox"})
	if len(th) != 1 {
		t.Fatalf("archive left %d in inbox", len(th))
	}
	th, _ = s.ListThreads(ListQuery{Folder: "role:archive"})
	if len(th) != 1 {
		t.Fatalf("archive view has %d", len(th))
	}
	// snooze hides from inbox and shows in snoozed, wakes when due
	if err := s.Snooze("a1/t1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	th, _ = s.ListThreads(ListQuery{Folder: "role:inbox"})
	if len(th) != 0 {
		t.Fatalf("snoozed still in inbox: %d", len(th))
	}
	th, _ = s.ListThreads(ListQuery{Folder: "snoozed"})
	if len(th) != 1 {
		t.Fatalf("snoozed view %d", len(th))
	}
	woke, _ := s.WakeSnoozed(time.Now().Add(2 * time.Hour))
	if len(woke) != 1 {
		t.Fatalf("woke %v", woke)
	}
	th, _ = s.ListThreads(ListQuery{Folder: "role:inbox"})
	if len(th) != 1 {
		t.Fatalf("after wake %d", len(th))
	}
	// trash is excluded from search
	_ = s.ApplyLocal(mailtypes.Action{Kind: mailtypes.ActTrash, IDs: []string{"1", "2"}}, "a1")
	hits, _ = s.ListThreads(ListQuery{Text: "invoice"})
	if len(hits) != 0 {
		t.Fatalf("trashed message found by search")
	}
}

func TestOutboxUndoAndOps(t *testing.T) {
	s := open(t)
	_ = s.QueueOutbox(OutboxItem{ID: "o1", AccountID: "a1", Payload: []byte("x"), SendAt: time.Now().Add(10 * time.Second), Subject: "hi"})
	if due, _ := s.DueOutbox(time.Now()); len(due) != 0 {
		t.Fatal("not due yet")
	}
	ok, _ := s.CancelOutbox("o1")
	if !ok {
		t.Fatal("undo should cancel")
	}
	_ = s.QueueOutbox(OutboxItem{ID: "o2", AccountID: "a1", Payload: []byte("y"), SendAt: time.Now().Add(-time.Second)})
	due, _ := s.DueOutbox(time.Now())
	if len(due) != 1 || due[0].ID != "o2" {
		t.Fatalf("due %v", due)
	}
	if ok, _ := s.CancelOutbox("o2"); !ok {
		t.Fatal("still cancellable until sent")
	}
	_ = s.QueueOp("a1", mailtypes.Action{Kind: mailtypes.ActRead, IDs: []string{"1"}})
	ops, _ := s.PendingOps("a1")
	if len(ops) != 1 || ops[0].Action.Kind != mailtypes.ActRead {
		t.Fatalf("ops %v", ops)
	}
}

func TestContactsSuggest(t *testing.T) {
	s := open(t)
	_ = s.UpsertMessages([]mailtypes.Message{msg("1", "t", "s", "b", 0, "INBOX")})
	_ = s.PutContacts("a1", []mailtypes.Contact{{Name: "Dana Lee", Email: "dana@x.test"}})
	c, _ := s.SuggestContacts("da", 5)
	if len(c) != 1 || c[0].Name != "Dana Lee" {
		t.Fatalf("%v", c)
	}
	c, _ = s.SuggestContacts("pat", 5)
	if len(c) != 1 {
		t.Fatalf("seen address not learned: %v", c)
	}
}

func TestIMAPStyleThreading(t *testing.T) {
	s := open(t)
	a := mailtypes.Message{AccountID: "a1", RemoteID: "1", Subject: "Plan", MessageID: "<a@x>", Date: time.Now().Add(-3 * time.Hour), Folders: []string{"INBOX"}}
	b := mailtypes.Message{AccountID: "a1", RemoteID: "2", Subject: "Re: Plan", MessageID: "<b@x>", InReplyTo: "<a@x>", References: []string{"<a@x>"}, Date: time.Now().Add(-2 * time.Hour), Folders: []string{"INBOX"}}
	// the reply arrives first in a second sync order: c names b, d names a, then the bridge
	c := mailtypes.Message{AccountID: "a1", RemoteID: "3", Subject: "Re: Re: Plan", MessageID: "<c@x>", InReplyTo: "<b@x>", References: []string{"<a@x>", "<b@x>"}, Date: time.Now().Add(-time.Hour), Folders: []string{"INBOX"}}
	if err := s.UpsertMessages([]mailtypes.Message{c, a}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages([]mailtypes.Message{b}); err != nil {
		t.Fatal(err)
	}
	th, _ := s.ListThreads(ListQuery{Folder: "role:inbox"})
	if len(th) != 1 || th[0].Count != 3 {
		t.Fatalf("expected one thread of 3, got %+v", th)
	}
}

func TestThreadReturnsFoldersWithoutDeadlock(t *testing.T) {
	s := open(t)
	_ = s.UpsertMessages([]mailtypes.Message{msg("1", "t1", "a", "b", time.Hour, "INBOX", "Archive")})
	done := make(chan []mailtypes.Message, 1)
	go func() { ms, _ := s.Thread("a1/t1"); done <- ms }()
	select {
	case ms := <-done:
		if len(ms) != 1 || len(ms[0].Folders) != 2 {
			t.Fatalf("%+v", ms)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Thread deadlocked on the single connection")
	}
}
