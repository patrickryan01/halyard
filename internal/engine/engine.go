// Package engine runs the accounts: one loop per account that sends queued
// actions, syncs, and then waits on the provider's watch for the next change.
// It owns the outbox that gives Undo Send, the snooze clock, and the events
// the UI listens to.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

type Event struct {
	Type      string `json:"type"` // synced, new, error, outbox, sent, snooze, accounts
	AccountID string `json:"accountId,omitempty"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	From      string `json:"from,omitempty"`
	Subject   string `json:"subject,omitempty"`
}

type Engine struct {
	St        *store.Store
	Sec       *secret.Store
	UndoDelay time.Duration

	mu      sync.Mutex
	runners map[string]*runner
	subs    map[chan Event]struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	// Factory builds providers; tests replace it.
	Factory func(mailtypes.Account, plugin.Secrets) (plugin.Provider, error)
}

func New(st *store.Store, sec *secret.Store) *Engine {
	return &Engine{St: st, Sec: sec, UndoDelay: 10 * time.Second, runners: map[string]*runner{}, subs: map[chan Event]struct{}{}, Factory: plugin.New}
}

func (e *Engine) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, ch)
		e.mu.Unlock()
	}
}

func (e *Engine) emit(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Start begins every account's loop and the outbox and snooze clocks.
func (e *Engine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	accts, err := e.St.Accounts()
	if err != nil {
		return err
	}
	if err := e.St.RecoverOutbox(); err != nil {
		return err
	}
	for _, a := range accts {
		e.startRunner(a)
	}
	go e.clock()
	return nil
}

func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Lock()
	rs := e.runners
	e.runners = map[string]*runner{}
	e.mu.Unlock()
	for _, r := range rs {
		r.stop()
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// AddAccount verifies the credentials by asking the provider who it is, then
// saves the account and starts syncing it. secretBlob is the token JSON or the
// password.
func (e *Engine) AddAccount(ctx context.Context, a mailtypes.Account, secretBlob []byte) (mailtypes.Account, error) {
	if a.ID == "" {
		a.ID = newID()
	}
	if err := e.Sec.Put(a.ID, secretBlob); err != nil {
		return a, err
	}
	p, err := e.Factory(a, e.Sec.For(a.ID))
	if err != nil {
		e.Sec.Delete(a.ID)
		return a, err
	}
	prof, err := p.Profile(ctx)
	p.Close()
	if err != nil {
		e.Sec.Delete(a.ID)
		return a, fmt.Errorf("could not sign in: %w", err)
	}
	if a.Email == "" {
		a.Email = prof.Email
	}
	if a.Name == "" {
		a.Name = prof.Name
	}
	if a.Color == "" {
		cols := []string{"#4f7cff", "#e5484d", "#30a46c", "#f5a524", "#8e4ec6", "#12a594"}
		all, _ := e.St.Accounts()
		a.Color = cols[len(all)%len(cols)]
	}
	if err := e.St.PutAccount(a); err != nil {
		return a, err
	}
	e.startRunner(a)
	e.emit(Event{Type: "accounts"})
	return a, nil
}

func (e *Engine) RemoveAccount(id string) error {
	e.mu.Lock()
	r := e.runners[id]
	delete(e.runners, id)
	e.mu.Unlock()
	if r != nil {
		r.stop()
	}
	if err := e.St.DeleteAccount(id); err != nil {
		return err
	}
	e.Sec.Delete(id)
	e.emit(Event{Type: "accounts"})
	return nil
}

func (e *Engine) startRunner(a mailtypes.Account) {
	r := &runner{e: e, acct: a, kick: make(chan struct{}, 1)}
	r.ctx, r.cancel = context.WithCancel(e.ctx)
	e.mu.Lock()
	if old := e.runners[a.ID]; old != nil {
		old.stop()
	}
	e.runners[a.ID] = r
	e.mu.Unlock()
	go r.loop()
}

func (e *Engine) runner(id string) *runner {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runners[id]
}

// SyncNow asks an account (or all of them) to sync immediately.
func (e *Engine) SyncNow(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, r := range e.runners {
		if id == "" || id == k {
			r.poke()
		}
	}
}

// Act applies an action to the cache at once, queues it for the server, and
// kicks the account's loop. Offline, the queue holds it until the next sync.
func (e *Engine) Act(accountID string, a mailtypes.Action) error {
	if err := e.St.ApplyLocal(a, accountID); err != nil {
		return err
	}
	if err := e.St.QueueOp(accountID, a); err != nil {
		return err
	}
	if r := e.runner(accountID); r != nil {
		r.poke()
	}
	return nil
}

// Send queues a message in the outbox. It goes out after UndoDelay unless
// Undo is called first.
func (e *Engine) Send(o mailtypes.Outgoing) (string, error) {
	if len(o.To)+len(o.Cc)+len(o.Bcc) == 0 {
		return "", errors.New("add at least one recipient")
	}
	acct, err := e.St.Account(o.AccountID)
	if err != nil {
		return "", errors.New("unknown account")
	}
	if o.From.Email == "" {
		o.From = mailtypes.Address{Name: acct.Name, Email: acct.Email}
	}
	if sig := strings.TrimSpace(acct.Signature); sig != "" && !strings.Contains(o.BodyText, sig) {
		o.BodyText += "\n\n-- \n" + sig
		if o.BodyHTML != "" {
			o.BodyHTML += "<br><br>-- <br>" + strings.ReplaceAll(htmlEscape(sig), "\n", "<br>")
		}
	}
	payload, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	id := newID()
	if err := e.St.QueueOutbox(store.OutboxItem{ID: id, AccountID: o.AccountID, Payload: payload, SendAt: time.Now().Add(e.UndoDelay), Subject: o.Subject}); err != nil {
		return "", err
	}
	e.emit(Event{Type: "outbox", ID: id, Subject: o.Subject})
	return id, nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Undo cancels a queued message. It returns false when it already went out.
func (e *Engine) Undo(id string) (bool, error) { return e.St.CancelOutbox(id) }

func (e *Engine) clock() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-t.C:
			e.dispatchOutbox(now)
			if now.Sub(last) > 5*time.Second {
				last = now
				if woke, err := e.St.WakeSnoozed(now); err == nil && len(woke) > 0 {
					e.emit(Event{Type: "snooze", Text: fmt.Sprintf("%d snoozed conversation(s) are back", len(woke))})
				}
			}
		}
	}
}

func (e *Engine) dispatchOutbox(now time.Time) {
	due, err := e.St.DueOutbox(now)
	if err != nil {
		return
	}
	for _, it := range due {
		// claim it so a second tick does not send it twice, and so Undo after
		// this point reports that it is too late
		ok, err := e.St.ClaimOutbox(it.ID)
		if err != nil || !ok {
			continue
		}
		go e.sendOne(it)
	}
}

func (e *Engine) sendOne(it store.OutboxItem) {
	var o mailtypes.Outgoing
	if err := json.Unmarshal(it.Payload, &o); err != nil {
		_ = e.St.FinishOutbox(it.ID, err)
		return
	}
	acct, err := e.St.Account(it.AccountID)
	if err != nil {
		_ = e.St.FinishOutbox(it.ID, err)
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Minute)
	defer cancel()
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err == nil {
		err = p.Send(ctx, o)
		p.Close()
	}
	_ = e.St.FinishOutbox(it.ID, err)
	if err != nil {
		e.emit(Event{Type: "error", AccountID: acct.ID, ID: it.ID, Subject: it.Subject, Text: "Could not send: " + err.Error()})
		return
	}
	e.emit(Event{Type: "sent", AccountID: acct.ID, ID: it.ID, Subject: it.Subject})
	e.SyncNow(acct.ID)
}

// RetryOutbox puts a failed message back.
func (e *Engine) RetryOutbox(id string) error { return e.St.RetryOutbox(id) }

// Attachment fetches an attachment's bytes from the provider.
func (e *Engine) Attachment(ctx context.Context, accountID, remoteMsgID, attID string) ([]byte, string, error) {
	acct, err := e.St.Account(accountID)
	if err != nil {
		return nil, "", err
	}
	p, err := e.Factory(acct, e.Sec.For(acct.ID))
	if err != nil {
		return nil, "", err
	}
	defer p.Close()
	return p.Attachment(ctx, remoteMsgID, attID)
}

// Contacts asks the provider for address book matches, for the composer.
func (e *Engine) Contacts(ctx context.Context, q string) []mailtypes.Contact {
	out, _ := e.St.SuggestContacts(q, 8)
	return out
}

// ---- per account loop ----

type runner struct {
	e      *Engine
	acct   mailtypes.Account
	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{}
	prov   plugin.Provider
	first  bool
}

func (r *runner) stop() {
	r.cancel()
	if r.prov != nil {
		r.prov.Close()
	}
}

func (r *runner) poke() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *runner) loop() {
	backoff := 2 * time.Second
	watching := false
	for r.ctx.Err() == nil {
		err := r.once()
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			log.Printf("sync %s: %v", r.acct.Email, err)
			_ = r.e.St.SetAccountError(r.acct.ID, err.Error())
			r.e.emit(Event{Type: "error", AccountID: r.acct.ID, Text: err.Error()})
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(backoff):
			case <-r.kick:
				backoff = 2 * time.Second
			}
			if backoff < 5*time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		if !watching {
			watching = true
			go r.watch()
		}
		select {
		case <-r.ctx.Done():
			return
		case <-r.kick:
		case <-time.After(10 * time.Minute): // safety net behind the watch
		}
	}
}

func (r *runner) watch() {
	wait := 5 * time.Second
	for r.ctx.Err() == nil {
		// a separate provider instance keeps the watch connection apart from
		// the one that syncs
		wp, err := r.e.Factory(r.acct, r.e.Sec.For(r.acct.ID))
		if err == nil {
			err = wp.Watch(r.ctx, r.poke)
			wp.Close()
		}
		if r.ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("watch %s: %v", r.acct.Email, err)
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(wait):
		}
		if wait < 2*time.Minute {
			wait *= 2
		}
	}
}

func (r *runner) provider() (plugin.Provider, error) {
	if r.prov != nil {
		return r.prov, nil
	}
	p, err := r.e.Factory(r.acct, r.e.Sec.For(r.acct.ID))
	if err != nil {
		return nil, err
	}
	r.prov = p
	return p, nil
}

func (r *runner) once() error {
	p, err := r.provider()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Minute)
	defer cancel()
	// queued actions first, so the sync that follows reports their result
	ops, err := r.e.St.PendingOps(r.acct.ID)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := p.Apply(ctx, op.Action); err != nil {
			return fmt.Errorf("could not apply %s: %w", op.Action.Kind, err)
		}
		_ = r.e.St.DoneOp(op.ID)
	}
	acct, err := r.e.St.Account(r.acct.ID)
	if err != nil {
		return err
	}
	state := acct.SyncState
	for round := 0; round < 50; round++ {
		res, err := p.Sync(ctx, state)
		if err != nil {
			return err
		}
		if err := r.apply(res, state == ""); err != nil {
			return err
		}
		more := res.State != "" && res.State != state && len(res.Change.Upsert)+len(res.Change.Delete) > 0 && r.acct.Kind == mailtypes.KindJMAP
		state = res.State
		if !more {
			break
		}
	}
	// contacts refresh, best effort: the address book is not worth failing a sync
	if cs, err := p.Contacts(ctx, ""); err == nil && len(cs) > 0 {
		_ = r.e.St.PutContacts(r.acct.ID, cs)
	}
	return nil
}

func (r *runner) apply(res plugin.SyncResult, firstSync bool) error {
	st := r.e.St
	if len(res.Change.Folders) > 0 {
		if err := st.ReplaceFolders(r.acct.ID, res.Change.Folders); err != nil {
			return err
		}
	}
	if res.Full {
		if err := st.ClearMessages(r.acct.ID); err != nil {
			return err
		}
	}
	if len(res.Change.Delete) > 0 {
		if err := st.DeleteMessages(r.acct.ID, res.Change.Delete); err != nil {
			return err
		}
	}
	if len(res.Change.Upsert) > 0 {
		if err := st.UpsertMessages(res.Change.Upsert); err != nil {
			return err
		}
	}
	if len(res.Change.Flags) > 0 {
		if err := st.ApplyFlags(r.acct.ID, res.Change.Flags); err != nil {
			return err
		}
	}
	if err := st.SetSyncState(r.acct.ID, res.State, ""); err != nil {
		return err
	}
	if !firstSync && !res.Full {
		inbox, _ := st.FolderByRole(r.acct.ID, mailtypes.RoleInbox)
		cutoff := time.Now().Add(-15 * time.Minute)
		for _, m := range res.Change.Upsert {
			if m.Read || m.Draft || m.Date.Before(cutoff) || strings.EqualFold(m.From.Email, r.acct.Email) {
				continue
			}
			in := false
			for _, f := range m.Folders {
				if f == inbox {
					in = true
				}
			}
			if in {
				name := m.From.Name
				if name == "" {
					name = m.From.Email
				}
				r.e.emit(Event{Type: "new", AccountID: r.acct.ID, From: name, Subject: m.Subject, ID: m.ThreadID})
			}
		}
	}
	r.e.emit(Event{Type: "synced", AccountID: r.acct.ID})
	return nil
}
