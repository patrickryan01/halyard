// Package secret keeps account credentials (OAuth tokens, passwords) out of
// the database: in the operating system's keyring when there is one (Secret
// Service on Linux, Credential Manager on Windows), and otherwise in a file
// that only the owner can read.
package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "mailclient"

type Store struct {
	Dir string // fallback directory
	// NoKeyring forces the file fallback, for tests and headless boxes.
	NoKeyring bool
}

func (s *Store) path(id string) string {
	return filepath.Join(s.Dir, strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(id)+".secret")
}

func (s *Store) Get(id string) ([]byte, error) {
	if !s.NoKeyring {
		if v, err := keyring.Get(service, id); err == nil {
			return []byte(v), nil
		}
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, errors.New("no saved credentials for this account; sign in again")
	}
	return b, nil
}

func (s *Store) Put(id string, b []byte) error {
	if !s.NoKeyring {
		if err := keyring.Set(service, id, string(b)); err == nil {
			_ = os.Remove(s.path(id))
			return nil
		}
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path(id), b, 0o600)
}

func (s *Store) Delete(id string) {
	_ = keyring.Delete(service, id)
	_ = os.Remove(s.path(id))
}

// For returns the plugin.Secrets view of one account.
func (s *Store) For(id string) *Account { return &Account{s: s, id: id} }

type Account struct {
	s  *Store
	id string
}

func (a *Account) Get() ([]byte, error) { return a.s.Get(a.id) }
func (a *Account) Put(b []byte) error   { return a.s.Put(a.id, b) }
