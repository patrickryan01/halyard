package google

import (
	"context"
	"encoding/base64"
	"strings"
)

// Raw fetches the message in its original form.
func (p *Provider) Raw(ctx context.Context, msgID string) ([]byte, error) {
	var r struct {
		Raw string `json:"raw"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/messages/"+msgID+"?format=raw", nil, nil, &r); err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(r.Raw, "="))
}
