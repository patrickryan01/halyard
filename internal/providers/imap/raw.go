package imap

import (
	"context"
	"errors"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Raw fetches the message exactly as stored.
func (p *Provider) Raw(ctx context.Context, remoteID string) ([]byte, error) {
	mb, uid, err := parseRID(remoteID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = p.withConn(func(c *imapclient.Client) error {
		if _, err := c.Select(mb, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return err
		}
		bufs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
		if err != nil {
			return err
		}
		if len(bufs) == 0 {
			return errors.New("message not found")
		}
		raw = bufs[0].FindBodySection(&imap.FetchItemBodySection{Peek: true})
		return nil
	})
	return raw, err
}
