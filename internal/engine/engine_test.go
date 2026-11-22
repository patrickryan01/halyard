package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

// scripted is a provider whose mailbox the test controls, to check what the engine does with a sync.
type scripted struct {
	mu    sync.Mutex
	msgs  []mailtypes.Message
	state int
}

func (s *scripted) Kind() string { return "imap" }
func (s *scripted) Close() error { return nil }
func (s *scripted) Profile(context.Context) (plugin.Profile, error) {
	return plugin.Profile{Email: "me@x.test"}, nil
}
func (s *scripted) Send(context.Context, mailtypes.Outgoing) error                { return nil }
func (s *scripted) Apply(context.Context, mailtypes.Action) error                 { return nil }
func (s *scripted) Contacts(context.Context, string) ([]mailtypes.Contact, error) { return nil, nil }
func (s *scripted) Attachment(context.Context, string, string) ([]byte, string, error) {
	return nil, "", nil
}
func (s *scripted) Watch(ctx context.Context, _ func()) error { <-ctx.Done(); return nil }
func (s *scripted) Sync(_ context.Context, state string) (plugin.SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state++
	res := plugin.SyncResult{State: string(rune('a' + s.state)), Full: state == ""}
	res.Change.Folders = []mailtypes.Folder{{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox}}
	res.Change.Upsert = append(res.Change.Upsert, s.msgs...)
	s.msgs = nil
	return res, nil
}
func (s *scripted) add(m mailtypes.Message) { s.mu.Lock(); s.msgs = append(s.msgs, m); s.mu.Unlock() }

func msg(id, from string, ago time.Duration, read bool) mailtypes.Message {
	return mailtypes.Message{AccountID: "a1", RemoteID: id, Subject: "s" + id, From: mailtypes.Address{Name: "Bob", Email: from}, Date: time.Now().Add(-ago), Read: read, Folders: []string{"INBOX"}}
}

// A message that arrives after the first sync, unread, in the inbox, from someone else, raises a new mail event, even
// when the server's clock and ours disagree. What was already there, what the person sent, and what is read does not.
func TestNewMailEvent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prov := &scripted{}
	prov.add(msg("old", "bob@x.test", time.Minute, false)) // in the first sync: no event
	e := New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	e.Factory = func(mailtypes.Account, plugin.Secrets) (plugin.Provider, error) { return prov, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	ch, unsub := e.Subscribe()
	defer unsub()
	if _, err := e.AddAccount(ctx, mailtypes.Account{ID: "a1", Kind: "imap", Email: "me@x.test"}, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	drain := func(d time.Duration) []Event {
		var out []Event
		end := time.After(d)
		for {
			select {
			case ev := <-ch:
				out = append(out, ev)
			case <-end:
				return out
			}
		}
	}
	for _, ev := range drain(1500 * time.Millisecond) {
		if ev.Type == "new" {
			t.Fatalf("the first sync must not announce what was already there: %+v", ev)
		}
	}
	prov.add(msg("mine", "me@x.test", time.Second, false))
	prov.add(msg("skewed", "bob@x.test", 8*time.Hour, false)) // a server clock hours behind ours
	prov.add(msg("seen", "bob@x.test", time.Second, true))
	prov.add(msg("fresh", "bob@x.test", time.Second, false))
	e.SyncNow("a1")
	var news []Event
	for _, ev := range drain(2 * time.Second) {
		if ev.Type == "new" {
			news = append(news, ev)
		}
	}
	if len(news) != 2 || news[0].Subject != "sskewed" || news[1].Subject != "sfresh" || news[0].From != "Bob" {
		t.Fatalf("want new mail events for the skewed and the fresh message only, got %+v", news)
	}
	// a message already in the cache that merely changes (a flag, a move) is not new
	prov.add(msg("fresh", "bob@x.test", time.Second, false))
	e.SyncNow("a1")
	for _, ev := range drain(1500 * time.Millisecond) {
		if ev.Type == "new" {
			t.Fatalf("a refreshed message must not be announced again: %+v", ev)
		}
	}
}
