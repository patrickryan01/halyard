package microsoft

import (
	"context"
	"net/url"
)

// Raw fetches the message as MIME.
func (p *Provider) Raw(ctx context.Context, msgID string) ([]byte, error) {
	var raw []byte
	if err := p.api.Do(ctx, "GET", p.base+"/me/messages/"+url.PathEscape(msgID)+"/$value", nil, hdr(), &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
