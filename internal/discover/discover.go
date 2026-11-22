// Package discover works out which plugin and which servers an email address
// needs, so adding an account is one field and one button: known providers,
// then the domain's MX records (which catches Google Workspace and Microsoft
// 365 on custom domains), then JMAP and the Thunderbird style autoconfig, then
// SRV records, then the common hostnames.
package discover

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Kind         string `json:"kind"` // google, microsoft, imap, jmap
	Domain       string `json:"domain"`
	Provider     string `json:"provider"`
	IMAPHost     string `json:"imapHost,omitempty"`
	IMAPPort     int    `json:"imapPort,omitempty"`
	IMAPSecurity string `json:"imapSecurity,omitempty"`
	SMTPHost     string `json:"smtpHost,omitempty"`
	SMTPPort     int    `json:"smtpPort,omitempty"`
	SMTPSecurity string `json:"smtpSecurity,omitempty"`
	JMAPURL      string `json:"jmapUrl,omitempty"`
	Note         string `json:"note,omitempty"`
}

var known = map[string]Result{
	"gmail.com":      {Kind: "google", Provider: "Google"},
	"googlemail.com": {Kind: "google", Provider: "Google"},
	"outlook.com":    {Kind: "microsoft", Provider: "Microsoft"},
	"hotmail.com":    {Kind: "microsoft", Provider: "Microsoft"},
	"live.com":       {Kind: "microsoft", Provider: "Microsoft"},
	"msn.com":        {Kind: "microsoft", Provider: "Microsoft"},
	"icloud.com":     {Kind: "imap", Provider: "iCloud", IMAPHost: "imap.mail.me.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: "starttls", Note: "iCloud needs an app-specific password."},
	"yahoo.com":      {Kind: "imap", Provider: "Yahoo", IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 465, SMTPSecurity: "ssl", Note: "Yahoo needs an app password."},
	"fastmail.com":   {Kind: "jmap", Provider: "Fastmail", JMAPURL: "https://api.fastmail.com", Note: "Use a Fastmail app password."},
	"proton.me":      {Kind: "imap", Provider: "Proton", IMAPHost: "127.0.0.1", IMAPPort: 1143, IMAPSecurity: "starttls", SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPSecurity: "starttls", Note: "Proton Mail needs its Bridge running on this computer."},
}

// Resolver is replaceable in tests.
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type Discoverer struct {
	Res  Resolver
	HTTP *http.Client
}

func New() *Discoverer {
	return &Discoverer{Res: net.DefaultResolver, HTTP: &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}}
}

func (d *Discoverer) Find(ctx context.Context, email string) (Result, error) {
	i := strings.LastIndex(email, "@")
	if i < 1 || i == len(email)-1 {
		return Result{}, fmt.Errorf("that does not look like an email address")
	}
	domain := strings.ToLower(strings.TrimSpace(email[i+1:]))
	if r, ok := known[domain]; ok {
		r.Domain = domain
		return r, nil
	}
	// MX: Google Workspace and Microsoft 365 hide behind custom domains
	if mx, err := d.Res.LookupMX(ctx, domain); err == nil {
		for _, m := range mx {
			h := strings.ToLower(strings.TrimSuffix(m.Host, "."))
			switch {
			case strings.HasSuffix(h, ".google.com") || strings.HasSuffix(h, ".googlemail.com") || strings.HasSuffix(h, ".l.google.com"):
				return Result{Kind: "google", Domain: domain, Provider: "Google Workspace"}, nil
			case strings.HasSuffix(h, ".mail.protection.outlook.com") || strings.HasSuffix(h, ".outlook.com"):
				return Result{Kind: "microsoft", Domain: domain, Provider: "Microsoft 365"}, nil
			}
		}
	}
	// JMAP: a 401 or 200 on the well-known URL means the server speaks it
	for _, host := range []string{domain, "mail." + domain, "jmap." + domain} {
		u := "https://" + host + "/.well-known/jmap"
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		if resp, err := d.HTTP.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 401 || resp.StatusCode == 200 {
				r := Result{Kind: "jmap", Domain: domain, Provider: "JMAP server", JMAPURL: "https://" + host}
				// IMAP details too, so a person can fall back
				return r, nil
			}
		}
	}
	// autoconfig
	for _, u := range []string{"https://autoconfig." + domain + "/mail/config-v1.1.xml", "https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml"} {
		if r, ok := d.autoconfig(ctx, u, domain); ok {
			return r, nil
		}
	}
	// SRV
	if r, ok := d.srv(ctx, domain); ok {
		return r, nil
	}
	// guess
	for _, h := range []string{"imap." + domain, "mail." + domain} {
		if _, err := d.Res.LookupHost(ctx, h); err == nil {
			return Result{Kind: "imap", Domain: domain, Provider: "IMAP server", IMAPHost: h, IMAPPort: 993, IMAPSecurity: "ssl",
				SMTPHost: strings.Replace(h, "imap.", "smtp.", 1), SMTPPort: 587, SMTPSecurity: "starttls", Note: "Check these server names."}, nil
		}
	}
	return Result{Kind: "imap", Domain: domain, Provider: "IMAP server", IMAPPort: 993, IMAPSecurity: "ssl", SMTPPort: 587, SMTPSecurity: "starttls",
		Note: "Could not find this server. Enter its address."}, nil
}

type acServer struct {
	Type       string `xml:"type,attr"`
	Hostname   string `xml:"hostname"`
	Port       string `xml:"port"`
	SocketType string `xml:"socketType"`
}

type autoconfigXML struct {
	Incoming []acServer `xml:"emailProvider>incomingServer"`
	Outgoing []acServer `xml:"emailProvider>outgoingServer"`
}

func sock(s string) string {
	switch strings.ToUpper(s) {
	case "SSL":
		return "ssl"
	case "STARTTLS":
		return "starttls"
	}
	return "none"
}

func (d *Discoverer) autoconfig(ctx context.Context, u, domain string) (Result, bool) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := d.HTTP.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return Result{}, false
	}
	defer resp.Body.Close()
	var x autoconfigXML
	if err := xml.NewDecoder(resp.Body).Decode(&x); err != nil {
		return Result{}, false
	}
	r := Result{Kind: "imap", Domain: domain, Provider: "IMAP server"}
	for _, in := range x.Incoming {
		if strings.EqualFold(in.Type, "imap") {
			r.IMAPHost, r.IMAPSecurity = in.Hostname, sock(in.SocketType)
			r.IMAPPort, _ = strconv.Atoi(in.Port)
			break
		}
	}
	for _, o := range x.Outgoing {
		if strings.EqualFold(o.Type, "smtp") {
			r.SMTPHost, r.SMTPSecurity = o.Hostname, sock(o.SocketType)
			r.SMTPPort, _ = strconv.Atoi(o.Port)
			break
		}
	}
	return r, r.IMAPHost != ""
}

func (d *Discoverer) srv(ctx context.Context, domain string) (Result, bool) {
	r := Result{Kind: "imap", Domain: domain, Provider: "IMAP server"}
	if _, s, err := d.Res.LookupSRV(ctx, "imaps", "tcp", domain); err == nil && len(s) > 0 {
		r.IMAPHost, r.IMAPPort, r.IMAPSecurity = strings.TrimSuffix(s[0].Target, "."), int(s[0].Port), "ssl"
	} else if _, s, err := d.Res.LookupSRV(ctx, "imap", "tcp", domain); err == nil && len(s) > 0 {
		r.IMAPHost, r.IMAPPort, r.IMAPSecurity = strings.TrimSuffix(s[0].Target, "."), int(s[0].Port), "starttls"
	} else {
		return Result{}, false
	}
	if _, s, err := d.Res.LookupSRV(ctx, "submissions", "tcp", domain); err == nil && len(s) > 0 {
		r.SMTPHost, r.SMTPPort, r.SMTPSecurity = strings.TrimSuffix(s[0].Target, "."), int(s[0].Port), "ssl"
	} else if _, s, err := d.Res.LookupSRV(ctx, "submission", "tcp", domain); err == nil && len(s) > 0 {
		r.SMTPHost, r.SMTPPort, r.SMTPSecurity = strings.TrimSuffix(s[0].Target, "."), int(s[0].Port), "starttls"
	}
	return r, true
}
