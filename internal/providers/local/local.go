// Package local is the account that has no server: mail imported from mbox files and Thunderbird profiles lives in it,
// in folders, on this computer. Nothing here syncs, sends or fetches.
package local

import (
	"context"
	"errors"

	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() {
	plugin.Register(mailtypes.KindLocal, func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) { return &Provider{acct: a.ID}, nil })
}

// Raw returns the stored message for an imported one; the engine sets it.
var Raw func(accountID, remoteID string) ([]byte, error)

type Provider struct{ acct string }

func (*Provider) Kind() string                                    { return mailtypes.KindLocal }
func (*Provider) Close() error                                    { return nil }
func (*Provider) Profile(context.Context) (plugin.Profile, error) { return plugin.Profile{}, nil }

// Sync reports nothing: the folders and messages are written by the importer, and syncing must not replace them.
func (*Provider) Sync(_ context.Context, state string) (plugin.SyncResult, error) {
	return plugin.SyncResult{State: "local"}, nil
}
func (*Provider) Send(context.Context, mailtypes.Outgoing) error {
	return errors.New("this account holds imported mail only; send from another account")
}
func (*Provider) Apply(context.Context, mailtypes.Action) error { return nil }
func (p *Provider) Attachment(_ context.Context, remoteID, attID string) ([]byte, string, error) {
	if Raw == nil {
		return nil, "", errors.New("the stored message is not available")
	}
	raw, err := Raw(p.acct, remoteID)
	if err != nil {
		return nil, "", err
	}
	pm, err := mime.Parse(raw)
	if err != nil {
		return nil, "", err
	}
	for i, a := range pm.Attach {
		if a.ID == attID {
			return pm.Parts[i], a.ContentType, nil
		}
	}
	return nil, "", errors.New("attachment not found")
}
func (*Provider) Contacts(context.Context, string) ([]mailtypes.Contact, error) { return nil, nil }
func (*Provider) Watch(ctx context.Context, _ func()) error                 { <-ctx.Done(); return nil }

// Raw returns the stored original.
func (p *Provider) Raw(_ context.Context, remoteID string) ([]byte, error) {
	if Raw == nil {
		return nil, errors.New("the stored message is not available")
	}
	return Raw(p.acct, remoteID)
}
