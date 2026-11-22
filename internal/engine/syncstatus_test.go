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

// gated is a provider whose Sync waits for the test to let it finish, so the progress of a Sync now can be read while it runs.
type gated struct {
	scripted
	mu2  sync.Mutex
	hold chan struct{}
}

func (g *gated) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	g.mu2.Lock()
	h := g.hold
	g.mu2.Unlock()
	if h != nil {
		select {
		case <-h:
		case <-ctx.Done():
			return plugin.SyncResult{}, ctx.Err()
		}
	}
	return g.scripted.Sync(ctx, state)
}

func (g *gated) setHold(h chan struct{}) { g.mu2.Lock(); g.hold = h; g.mu2.Unlock() }

// Sync now reports a running sync, how many of the accounts it started have finished, and a last-synced time that moves forward
// only when every account has finished.
func TestSyncStatusProgress(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	provs := map[string]*gated{"a1": {}, "a2": {}}
	e := New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	e.Factory = func(a mailtypes.Account, _ plugin.Secrets) (plugin.Provider, error) { return provs[a.ID], nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	ch, unsub := e.Subscribe()
	defer unsub()
	for _, id := range []string{"a1", "a2"} {
		if _, err := e.AddAccount(ctx, mailtypes.Account{ID: id, Kind: "imap", Email: id + "@x.test"}, []byte("pw")); err != nil {
			t.Fatal(err)
		}
		waitEvent(t, ch, "synced")
	}
	waitFor := func(what string, ok func(SyncStatus) bool) SyncStatus {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for {
			s := e.SyncStatus()
			if ok(s) {
				return s
			}
			if time.Now().After(end) {
				t.Fatalf("timed out waiting for %s, last status %+v", what, s)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	idle := waitFor("idle", func(s SyncStatus) bool { return !s.Running })
	if idle.LastSync.IsZero() || len(idle.Accounts) != 2 {
		t.Fatalf("after the first syncs every account has a last sync: %+v", idle)
	}
	before := idle.LastSync
	h1, h2 := make(chan struct{}), make(chan struct{})
	provs["a1"].setHold(h1)
	provs["a2"].setHold(h2)
	time.Sleep(1100 * time.Millisecond) // the stored last sync has one second resolution
	e.SyncNow("")
	s := waitFor("both running", func(s SyncStatus) bool { return s.Running && s.Total == 2 && s.Done == 0 })
	if !s.Accounts[0].Syncing && !s.Accounts[1].Syncing {
		t.Fatalf("an account must read as syncing: %+v", s)
	}
	close(h1)
	s = waitFor("one done", func(s SyncStatus) bool { return s.Running && s.Total == 2 && s.Done == 1 })
	if !s.LastSync.Equal(before) {
		t.Fatalf("last synced must not move until every account has finished: %v then %v", before, s.LastSync)
	}
	close(h2)
	s = waitFor("all done", func(s SyncStatus) bool { return !s.Running })
	if s.Done != 2 || s.Total != 2 || !s.LastSync.After(before) {
		t.Fatalf("a finished Sync now reads 2 of 2 and a newer last sync: %+v (before %v)", s, before)
	}
}
