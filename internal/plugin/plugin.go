// Package plugin is the one interface every mail provider implements. The
// sync engine and the UI know nothing about Gmail, Graph, IMAP or JMAP; they
// only see a Provider.
package plugin

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// Profile is who the provider says the signed in person is.
type Profile struct {
	Email string
	Name  string
}

// SyncResult is one incremental pass. State is opaque to the engine: Gmail
// stores a history ID, Graph a delta link per folder, IMAP UIDVALIDITY and
// UIDNEXT, JMAP the Email state string.
type SyncResult struct {
	Change mailtypes.Change
	State  string
	// Full is true when the pass was a full resync, so the engine replaces
	// what it holds instead of merging.
	Full bool
}

type Provider interface {
	Kind() string
	Profile(ctx context.Context) (Profile, error)
	// Sync fetches changes since state. An empty state means first sync.
	Sync(ctx context.Context, state string) (SyncResult, error)
	Send(ctx context.Context, m mailtypes.Outgoing) error
	Apply(ctx context.Context, a mailtypes.Action) error
	Attachment(ctx context.Context, remoteMsgID, attachmentID string) ([]byte, string, error)
	Contacts(ctx context.Context, query string) ([]mailtypes.Contact, error)
	// Watch blocks, calling notify whenever the server says something changed
	// (IMAP IDLE, JMAP EventSource, or a short poll where the API has no
	// stream). It returns when ctx ends or the connection fails.
	Watch(ctx context.Context, notify func()) error
	Close() error
}

// Factory builds a provider for one account. secret returns the stored secret
// blob for the account (token JSON or password) and save writes back a
// refreshed one.
type Factory func(acct mailtypes.Account, sec Secrets) (Provider, error)

type Secrets interface {
	Get() ([]byte, error)
	Put([]byte) error
}

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

func Register(kind string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[kind] = f
}

func New(acct mailtypes.Account, sec Secrets) (Provider, error) {
	mu.RLock()
	f, ok := registry[acct.Kind]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no provider plugin for %q", acct.Kind)
	}
	return f(acct, sec)
}

func Kinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	var k []string
	for n := range registry {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

// FolderCreator is implemented by providers that can create a label or folder.
// It returns the new folder's remote ID.
type FolderCreator interface {
	CreateFolder(ctx context.Context, name string, role string) (string, error)
}

// ContactWriter is implemented by providers with an address book that takes
// writes. Contacts for other accounts are kept in the local address book.
type ContactWriter interface {
	CreateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error)
	UpdateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error)
	DeleteContact(ctx context.Context, id string) error
}

// DraftSaver is implemented by providers that keep drafts on the server, so a
// draft written here shows up in the provider's own Drafts folder everywhere.
type DraftSaver interface {
	// SaveDraft writes the draft, replacing replaceID when it is not empty, and
	// returns the ID to pass back next time.
	SaveDraft(ctx context.Context, o mailtypes.Outgoing, replaceID string) (string, error)
	DeleteDraft(ctx context.Context, id string) error
	// DraftID turns the remote ID of a message in the Drafts folder into the
	// ID SaveDraft and DeleteDraft use for it.
	DraftID(ctx context.Context, messageRemoteID string) (string, error)
}
