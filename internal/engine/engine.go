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
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/providers/local"
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
	local.Raw = st.Raw
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

// Emit sends an event to every listener, for changes made outside the sync loop.
func (e *Engine) Emit(ev Event) { e.emit(ev) }

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
		e.EnsureIdentity(a)
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
	e.EnsureIdentity(a)
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
func (e *Engine) Send(o mailtypes.Outgoing) (string, error) { return e.SendAfter(o, e.UndoDelay) }

// SendAfter is Send with an explicit undo window; zero sends at once.
func (e *Engine) SendAfter(o mailtypes.Outgoing, delay time.Duration) (string, error) {
	return e.SendAt(o, time.Now().Add(delay))
}

// SendAt queues a message to go out at a given time. Until then it sits in the outbox and can be cancelled; a time in
// the past sends at once.
func (e *Engine) SendAt(o mailtypes.Outgoing, at time.Time) (string, error) {
	o.To, o.Cc, o.Bcc = e.groupMembers(o.To), e.groupMembers(o.Cc), e.groupMembers(o.Bcc)
	if len(o.To)+len(o.Cc)+len(o.Bcc) == 0 {
		return "", errors.New("add at least one recipient")
	}
	acct, err := e.St.Account(o.AccountID)
	if err != nil {
		return "", errors.New("unknown account")
	}
	sig := acct.Signature
	if o.From.Email == "" {
		o.From = mailtypes.Address{Name: acct.Name, Email: acct.Email}
	}
	if ident, ok := e.identityFor(o.AccountID, o.From.Email); ok {
		if o.From.Name == "" {
			o.From.Name = ident.Name
		}
		if ident.Signature != "" {
			sig = ident.Signature
		} else if !ident.Default {
			sig = "" // an alias without a signature of its own sends without one
		}
		if ident.ReplyTo != "" && len(o.ReplyTo) == 0 {
			o.ReplyTo = mime.ParseList(ident.ReplyTo)
		}
	}
	if o.From.Name == "" {
		o.From.Name = acct.Name
	}
	if sig = strings.TrimSpace(sig); sig != "" && !strings.Contains(o.BodyText, sig) {
		o.BodyText += "\n\n-- \n" + sig
		if o.BodyHTML != "" {
			o.BodyHTML += "<br><br>-- <br>" + strings.ReplaceAll(htmlEscape(sig), "\n", "<br>")
		}
	}
	if o.Sign || o.Encrypt {
		raw, err := e.protect(o)
		if err != nil {
			return "", err
		}
		o.Raw = raw
	}
	payload, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	id := newID()
	if err := e.St.QueueOutbox(store.OutboxItem{ID: id, AccountID: o.AccountID, Payload: payload, SendAt: at, Subject: o.Subject}); err != nil {
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
	if err != nil && temporary(err) {
		if ok, derr := e.St.DeferOutbox(it.ID, err, retryDelays); derr == nil && ok {
			e.emit(Event{Type: "outbox", ID: it.ID, Subject: it.Subject})
			return
		}
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
	out := e.SearchContacts(ctx, q)
	if len(out) > 8 {
		out = out[:8]
	}
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
	r.e.syncDAVContacts(ctx, acct)
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
	var known map[string]bool
	if !firstSync && !res.Full && len(res.Change.Upsert) > 0 {
		ids := make([]string, 0, len(res.Change.Upsert))
		for _, m := range res.Change.Upsert {
			ids = append(ids, m.RemoteID)
		}
		known = st.KnownMessages(r.acct.ID, ids)
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
		// announce mail that is new to this computer, unread, in the inbox, and not from the person themself. The test is
		// "never seen before", not "recent": a server's clock and this one's can disagree by hours.
		inbox, _ := st.FolderByRole(r.acct.ID, mailtypes.RoleInbox)
		var fresh []mailtypes.Message
		for _, m := range res.Change.Upsert {
			if known[m.RemoteID] || m.Read || m.Draft || strings.EqualFold(m.From.Email, r.acct.Email) {
				continue
			}
			for _, f := range m.Folders {
				if f == inbox {
					fresh = append(fresh, m)
					break
				}
			}
		}
		// rules run on mail that is new to this computer, in the inbox, whether read or not
		var arrivals []mailtypes.Message
		for _, m := range res.Change.Upsert {
			if known[m.RemoteID] || m.Draft || strings.EqualFold(m.From.Email, r.acct.Email) {
				continue
			}
			for _, f := range m.Folders {
				if f == inbox {
					arrivals = append(arrivals, m)
					break
				}
			}
		}
		if quiet := r.e.RunRules(arrivals); len(quiet) > 0 {
			kept := fresh[:0]
			for _, m := range fresh {
				if !quiet[m.RemoteID] {
					kept = append(kept, m)
				}
			}
			fresh = kept
		}
		for i, m := range fresh {
			if i == 5 && len(fresh) > 5 {
				r.e.emit(Event{Type: "new", AccountID: r.acct.ID, From: fmt.Sprintf("%d more new messages", len(fresh)-5), Subject: r.acct.Email})
				break
			}
			name := m.From.Name
			if name == "" {
				name = m.From.Email
			}
			r.e.emit(Event{Type: "new", AccountID: r.acct.ID, From: name, Subject: m.Subject, ID: m.ThreadID})
		}
	}
	r.e.emit(Event{Type: "synced", AccountID: r.acct.ID})
	return nil
}

// retryDelays is how long a message that could not be sent for a temporary reason waits before each new try.
var retryDelays = []time.Duration{20 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// temporary says whether a failed send is worth trying again by itself: the server said "try later" (an SMTP 4xx
// answer) or the network was not there. A refusal (5xx), a bad login and a message that cannot be built are not.
func temporary(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"smtp error 4", " 421", " 450", " 451", " 452", "timeout", "timed out", "connection refused", "connection reset", "no such host", "network is unreachable", "i/o timeout", "temporar", "try again", "rate limit"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
