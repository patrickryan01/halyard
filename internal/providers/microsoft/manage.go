package microsoft

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// RenameFolder renames a mail folder.
func (p *Provider) RenameFolder(ctx context.Context, remoteID, name string) error {
	return p.api.Do(ctx, "PATCH", p.base+"/me/mailFolders/"+url.PathEscape(remoteID), map[string]string{"displayName": name}, hdr(), nil)
}

// DeleteFolder deletes a mail folder; Graph moves it to Deleted Items.
func (p *Provider) DeleteFolder(ctx context.Context, remoteID string) error {
	return p.api.Do(ctx, "DELETE", p.base+"/me/mailFolders/"+url.PathEscape(remoteID), nil, hdr(), nil)
}

// Identities lists the mailbox address and its aliases.
func (p *Provider) Identities(ctx context.Context) ([]mailtypes.Identity, error) {
	var r struct {
		Mail           string   `json:"mail"`
		DisplayName    string   `json:"displayName"`
		ProxyAddresses []string `json:"proxyAddresses"`
		UPN            string   `json:"userPrincipalName"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/me?$select=mail,displayName,proxyAddresses,userPrincipalName", nil, nil, &r); err != nil {
		return nil, err
	}
	primary := r.Mail
	if primary == "" {
		primary = r.UPN
	}
	out := []mailtypes.Identity{{Name: r.DisplayName, Email: primary, Default: true}}
	seen := map[string]bool{strings.ToLower(primary): true}
	for _, a := range r.ProxyAddresses {
		if !strings.HasPrefix(a, "smtp:") && !strings.HasPrefix(a, "SMTP:") {
			continue
		}
		e := a[5:]
		if seen[strings.ToLower(e)] {
			continue
		}
		seen[strings.ToLower(e)] = true
		out = append(out, mailtypes.Identity{Name: r.DisplayName, Email: e, Alias: true})
	}
	return out, nil
}

type graphWhen struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

func (w graphWhen) time() time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.0000000", w.DateTime)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", w.DateTime)
	}
	if err != nil {
		return time.Time{}
	}
	return t
}

// GetVacation reads the automatic replies setting.
func (p *Provider) GetVacation(ctx context.Context) (mailtypes.Vacation, error) {
	var r struct {
		Auto struct {
			Status   string    `json:"status"`
			Internal string    `json:"internalReplyMessage"`
			External string    `json:"externalReplyMessage"`
			Start    graphWhen `json:"scheduledStartDateTime"`
			End      graphWhen `json:"scheduledEndDateTime"`
			Audience string    `json:"externalAudience"`
		} `json:"automaticRepliesSetting"`
	}
	if err := p.api.Do(ctx, "GET", p.base+"/me/mailboxSettings", nil, nil, &r); err != nil {
		return mailtypes.Vacation{}, err
	}
	a := r.Auto
	body := a.External
	if body == "" {
		body = a.Internal
	}
	return mailtypes.Vacation{Enabled: a.Status != "disabled" && a.Status != "", Body: body, Start: a.Start.time(), End: a.End.time(), ContactsOnly: a.Audience == "contactsOnly"}, nil
}

// SetVacation writes the automatic replies setting. Graph has one message for people inside the organization and one
// for everyone else; both get the same text.
func (p *Provider) SetVacation(ctx context.Context, v mailtypes.Vacation) error {
	status := "disabled"
	if v.Enabled {
		status = "alwaysEnabled"
		if !v.Start.IsZero() && !v.End.IsZero() {
			status = "scheduled"
		}
	}
	aud := "all"
	if v.ContactsOnly {
		aud = "contactsOnly"
	}
	auto := map[string]any{"status": status, "internalReplyMessage": v.Body, "externalReplyMessage": v.Body, "externalAudience": aud}
	if status == "scheduled" {
		auto["scheduledStartDateTime"] = graphWhen{v.Start.UTC().Format("2006-01-02T15:04:05"), "UTC"}
		auto["scheduledEndDateTime"] = graphWhen{v.End.UTC().Format("2006-01-02T15:04:05"), "UTC"}
	}
	return p.api.Do(ctx, "PATCH", p.base+"/me/mailboxSettings", map[string]any{"automaticRepliesSetting": auto}, nil, nil)
}
