// Package local is the account that has no server: mail imported from mbox files and Thunderbird profiles lives in it,
// in folders, on this computer. Nothing here syncs, sends or fetches.
package local

import (
	"context"
	"errors"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() {
	plugin.Register(mailtypes.KindLocal, func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) { return &Provider{}, nil })
}

type Provider struct{}

func (*Provider) Kind() string                                    { return mailtypes.KindLocal }
func (*Provider) Close() error                                    { return nil }
func (*Provider) Profile(context.Context) (plugin.Profile, error) { return plugin.Profile{}, nil }

// Sync reports the standard folders once. Folders an import adds are kept: later passes say nothing about folders.
func (*Provider) Sync(_ context.Context, state string) (plugin.SyncResult, error) {
	res := plugin.SyncResult{State: "local"}
	if state == "" {
		res.Change.Folders = []mailtypes.Folder{
			{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox}, {RemoteID: "SENT", Name: "Sent", Role: mailtypes.RoleSent},
			{RemoteID: "ARCHIVE", Name: "Archive", Role: mailtypes.RoleArchive}, {RemoteID: "TRASH", Name: "Trash", Role: mailtypes.RoleTrash},
		}
	}
	return res, nil
}
func (*Provider) Send(context.Context, mailtypes.Outgoing) error {
	return errors.New("this account holds imported mail only; send from another account")
}
func (*Provider) Apply(context.Context, mailtypes.Action) error { return nil }
func (*Provider) Attachment(context.Context, string, string) ([]byte, string, error) {
	return nil, "", errors.New("attachments of imported mail are read from the stored message")
}
func (*Provider) Contacts(context.Context, string) ([]mailtypes.Contact, error) { return nil, nil }
func (*Provider) Watch(ctx context.Context, _ func()) error                 { <-ctx.Done(); return nil }
