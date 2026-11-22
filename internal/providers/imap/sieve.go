package imap

import (
	"context"
	"strconv"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/sieve"
)

// Sieve opens the account's filter service (ManageSieve). The server is the IMAP server unless the account says
// otherwise (sieve_host, sieve_port, sieve_security).
func (p *Provider) Sieve(ctx context.Context) (*sieve.Client, error) {
	pw, err := p.password()
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(p.set("sieve_port", "4190"))
	cfg := sieve.Config{Host: p.set("sieve_host", p.acct.Settings[SHost]), Port: port, Security: p.set("sieve_security", "starttls"), User: p.user(),
		SkipVerify: p.acct.Settings[SSkipVerify] == "true"}
	if p.acct.Settings["auth"] == "xoauth2" {
		cfg.Token = pw
	} else {
		cfg.Password = pw
	}
	return sieve.Dial(cfg)
}

// GetVacation reads the automatic reply from the active Sieve script.
func (p *Provider) GetVacation(ctx context.Context) (mailtypes.Vacation, error) {
	c, err := p.Sieve(ctx)
	if err != nil {
		return mailtypes.Vacation{}, err
	}
	defer c.Close()
	return c.GetVacation()
}

// SetVacation writes the automatic reply into the active Sieve script.
func (p *Provider) SetVacation(ctx context.Context, v mailtypes.Vacation) error {
	c, err := p.Sieve(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.SetVacation(v)
}
