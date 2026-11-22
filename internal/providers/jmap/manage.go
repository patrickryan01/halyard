package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

const capVacation = "urn:ietf:params:jmap:vacationresponse"

// RenameFolder renames a mailbox.
func (p *Provider) RenameFolder(ctx context.Context, remoteID, name string) error {
	return p.mailboxSet(ctx, map[string]any{"update": map[string]any{remoteID: map[string]any{"name": name}}}, "updated", remoteID)
}

// DeleteFolder removes a mailbox. Mail in it that lives nowhere else is removed with it.
func (p *Provider) DeleteFolder(ctx context.Context, remoteID string) error {
	return p.mailboxSet(ctx, map[string]any{"destroy": []string{remoteID}, "onDestroyRemoveEmails": true}, "destroyed", remoteID)
}

func (p *Provider) mailboxSet(ctx context.Context, args map[string]any, okKey, id string) error {
	acct, err := p.accountID(ctx)
	if err != nil {
		return err
	}
	args["accountId"] = acct
	r, err := p.do(ctx, mailUse, call{"Mailbox/set", args, "m"})
	if err != nil {
		return err
	}
	var out map[string]json.RawMessage
	_ = json.Unmarshal(r["m"], &out)
	for _, k := range []string{"notUpdated", "notDestroyed"} {
		if raw, ok := out[k]; ok && string(raw) != "null" && string(raw) != "{}" {
			return fmt.Errorf("jmap: %s: %s", k, trunc(string(raw), 200))
		}
	}
	if _, err := p.mailboxes(ctx, acct); err != nil {
		return err
	}
	return nil
}

// Identities lists the sending identities the server has.
func (p *Provider) Identities(ctx context.Context) ([]mailtypes.Identity, error) {
	acct, err := p.accountID(ctx)
	if err != nil {
		return nil, err
	}
	r, err := p.do(ctx, []string{capCore, capMail, capSub}, call{"Identity/get", map[string]any{"accountId": acct}, "i"})
	if err != nil {
		return nil, err
	}
	var l struct {
		List []struct {
			ID, Name, Email, TextSignature string
			ReplyTo                        []jaddr
		} `json:"list"`
	}
	_ = json.Unmarshal(r["i"], &l)
	var out []mailtypes.Identity
	for i, x := range l.List {
		id := mailtypes.Identity{Name: x.Name, Email: x.Email, Signature: x.TextSignature, Default: i == 0, Alias: i > 0}
		if rt := conv(x.ReplyTo); len(rt) > 0 {
			id.ReplyTo = rt[0].Email
		}
		out = append(out, id)
	}
	return out, nil
}

func (p *Provider) vacationUse(ctx context.Context) ([]string, string, error) {
	s, err := p.session(ctx)
	if err != nil {
		return nil, "", err
	}
	acct := s.PrimaryAccounts[capVacation]
	if acct == "" {
		acct = s.PrimaryAccounts[capMail]
	}
	return []string{capCore, capVacation}, acct, nil
}

// GetVacation reads the server's vacation response.
func (p *Provider) GetVacation(ctx context.Context) (mailtypes.Vacation, error) {
	use, acct, err := p.vacationUse(ctx)
	if err != nil {
		return mailtypes.Vacation{}, err
	}
	r, err := p.do(ctx, use, call{"VacationResponse/get", map[string]any{"accountId": acct}, "v"})
	if err != nil {
		return mailtypes.Vacation{}, err
	}
	var l struct {
		List []struct {
			IsEnabled bool       `json:"isEnabled"`
			From      *time.Time `json:"fromDate"`
			To        *time.Time `json:"toDate"`
			Subject   *string    `json:"subject"`
			Text      *string    `json:"textBody"`
		} `json:"list"`
	}
	_ = json.Unmarshal(r["v"], &l)
	var v mailtypes.Vacation
	if len(l.List) > 0 {
		x := l.List[0]
		v.Enabled = x.IsEnabled
		if x.From != nil {
			v.Start = *x.From
		}
		if x.To != nil {
			v.End = *x.To
		}
		if x.Subject != nil {
			v.Subject = *x.Subject
		}
		if x.Text != nil {
			v.Body = *x.Text
		}
	}
	return v, nil
}

// SetVacation writes the vacation response (the singleton object has the ID "singleton").
func (p *Provider) SetVacation(ctx context.Context, v mailtypes.Vacation) error {
	use, acct, err := p.vacationUse(ctx)
	if err != nil {
		return err
	}
	u := map[string]any{"isEnabled": v.Enabled, "subject": v.Subject, "textBody": v.Body, "htmlBody": nil}
	if v.Start.IsZero() {
		u["fromDate"] = nil
	} else {
		u["fromDate"] = v.Start.UTC().Format(time.RFC3339)
	}
	if v.End.IsZero() {
		u["toDate"] = nil
	} else {
		u["toDate"] = v.End.UTC().Format(time.RFC3339)
	}
	r, err := p.do(ctx, use, call{"VacationResponse/set", map[string]any{"accountId": acct, "update": map[string]any{"singleton": u}}, "v"})
	if err != nil {
		return err
	}
	var out struct {
		NotUpdated map[string]json.RawMessage `json:"notUpdated"`
	}
	_ = json.Unmarshal(r["v"], &out)
	if len(out.NotUpdated) > 0 {
		return fmt.Errorf("jmap: vacation not saved: %s", trunc(string(out.NotUpdated["singleton"]), 200))
	}
	return nil
}
