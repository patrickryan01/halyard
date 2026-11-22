// Package app wires the core together: data directory, store, engine, API.
// The native window and the headless daemon both start it.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/patrickryan01/halyard/internal/api"
	"github.com/patrickryan01/halyard/internal/brand"
	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
	"github.com/patrickryan01/halyard/ui"

	// the provider plugins register themselves
	_ "github.com/patrickryan01/halyard/internal/providers/google"
	_ "github.com/patrickryan01/halyard/internal/providers/imap"
	_ "github.com/patrickryan01/halyard/internal/providers/jmap"
	_ "github.com/patrickryan01/halyard/internal/providers/microsoft"
)

type App struct {
	Dir    string
	Store  *store.Store
	Engine *engine.Engine
	API    *api.Server
	cancel context.CancelFunc
}

// DataDir is where mail, settings and fallback secrets live.
func DataDir() (string, error) {
	if d := os.Getenv("MAILCLIENT_DATA"); d != "" {
		return d, os.MkdirAll(d, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, brand.Slug)
	return d, os.MkdirAll(d, 0o700)
}

// Start opens the store and begins syncing every account. token is empty for
// the in process native window and set for the loopback daemon.
func Start(dir, token string) (*App, error) {
	st, err := store.Open(filepath.Join(dir, "mail.db"))
	if err != nil {
		return nil, err
	}
	sec := &secret.Store{Dir: filepath.Join(dir, "secrets"), NoKeyring: os.Getenv("MAILCLIENT_NO_KEYRING") != ""}
	eng := engine.New(st, sec)
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("start: %w", err)
	}
	srv := api.New(eng, brand.Version, ui.Handler())
	srv.Token = token
	return &App{Dir: dir, Store: st, Engine: eng, API: srv, cancel: cancel}, nil
}

func (a *App) Close() {
	a.cancel()
	a.Engine.Stop()
	a.Store.Close()
}

func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
