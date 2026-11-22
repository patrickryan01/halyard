package imap

import (
	"context"

	"github.com/emersion/go-imap/v2/imapclient"
)

// RenameFolder renames a mailbox.
func (p *Provider) RenameFolder(ctx context.Context, remoteID, name string) error {
	return p.withConn(func(c *imapclient.Client) error { return c.Rename(remoteID, name, nil).Wait() })
}

// DeleteFolder removes a mailbox and the mail in it.
func (p *Provider) DeleteFolder(ctx context.Context, remoteID string) error {
	return p.withConn(func(c *imapclient.Client) error { return c.Delete(remoteID).Wait() })
}
