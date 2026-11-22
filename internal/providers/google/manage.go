package google

import (
	"context"
	"net/url"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// RenameFolder renames a label.
func (p *Provider) RenameFolder(ctx context.Context, remoteID, name string) error {
	return p.api.Do(ctx, "PATCH", p.base+"/labels/"+url.PathEscape(remoteID), map[string]any{"name": name}, nil, nil)
}

// DeleteFolder deletes a label; the mail stays, without that label.
func (p *Provider) DeleteFolder(ctx context.Context, remoteID string) error {
	return p.api.Do(ctx, "DELETE", p.base+"/labels/"+url.PathEscape(remoteID), nil, nil, nil)
}

// Identities lists the send-as addresses, with their signatures.
func (p *Provider) Identities(ctx context.Context) ([]mailtypes.Identity, error) {
	var r struct {
		SendAs []struct {
			Email     string `json:"sendAsEmail"`
			Name      string `json:"displayName"`
			ReplyTo   string `json:"replyToAddress"`
			Signature string `json:"signature"`
			Primary   bool   `json:"isPrimary"`
			Default   bool   `json:"isDefault"`
		} `json:"sendAs"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/settings/sendAs", nil, nil, &r); err != nil {
		return nil, err
	}
	var out []mailtypes.Identity
	for _, s := range r.SendAs {
		out = append(out, mailtypes.Identity{Name: s.Name, Email: s.Email, ReplyTo: s.ReplyTo, Signature: htmlToPlain(s.Signature), Default: s.Default, Alias: !s.Primary})
	}
	return out, nil
}

func htmlToPlain(h string) string {
	var b []rune
	in := false
	for _, r := range h {
		switch {
		case r == '<':
			in = true
			b = append(b, '\n')
		case r == '>':
			in = false
		case !in:
			b = append(b, r)
		}
	}
	out, nl := []rune{}, 0
	for _, r := range b {
		if r == '\n' {
			nl++
			if nl > 1 {
				continue
			}
		} else {
			nl = 0
		}
		out = append(out, r)
	}
	return string(out)
}

// GetVacation reads the automatic reply.
func (p *Provider) GetVacation(ctx context.Context) (mailtypes.Vacation, error) {
	var r struct {
		Enable   bool   `json:"enableAutoReply"`
		Subject  string `json:"responseSubject"`
		Body     string `json:"responseBodyPlainText"`
		Start    string `json:"startTime"`
		End      string `json:"endTime"`
		Contacts bool   `json:"restrictToContacts"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/settings/vacation", nil, nil, &r); err != nil {
		return mailtypes.Vacation{}, err
	}
	v := mailtypes.Vacation{Enabled: r.Enable, Subject: r.Subject, Body: r.Body, ContactsOnly: r.Contacts}
	v.Start, v.End = msTime(r.Start), msTime(r.End)
	return v, nil
}

func msTime(s string) time.Time {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return time.Time{}
		}
		n = n*10 + int64(c-'0')
	}
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

// SetVacation writes the automatic reply.
func (p *Provider) SetVacation(ctx context.Context, v mailtypes.Vacation) error {
	b := map[string]any{"enableAutoReply": v.Enabled, "responseSubject": v.Subject, "responseBodyPlainText": v.Body, "restrictToContacts": v.ContactsOnly}
	if !v.Start.IsZero() {
		b["startTime"] = v.Start.UnixMilli()
	}
	if !v.End.IsZero() {
		b["endTime"] = v.End.UnixMilli()
	}
	return p.api.Do(ctx, "PUT", p.base+"/settings/vacation", b, nil, nil)
}
