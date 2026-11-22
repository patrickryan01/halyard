package jmap

import (
	"context"
	"encoding/json"
	"errors"
)

// Raw downloads the message blob.
func (p *Provider) Raw(ctx context.Context, msgID string) ([]byte, error) {
	acct, err := p.accountID(ctx)
	if err != nil {
		return nil, err
	}
	r, err := p.do(ctx, mailUse, call{"Email/get", map[string]any{"accountId": acct, "ids": []string{msgID}, "properties": []string{"blobId"}}, "b"})
	if err != nil {
		return nil, err
	}
	var l struct {
		List []struct {
			BlobID string `json:"blobId"`
		} `json:"list"`
	}
	_ = json.Unmarshal(r["b"], &l)
	if len(l.List) == 0 || l.List[0].BlobID == "" {
		return nil, errors.New("message not found")
	}
	b, _, err := p.Attachment(ctx, msgID, l.List[0].BlobID)
	return b, err
}
