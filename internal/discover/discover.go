// Package discover works out which plugin and which servers an email address
// needs, so adding an account is one field and one button. In order: providers
// we know by name, Microsoft and Google domains (by suffix and by MX, which
// catches Workspace and Microsoft 365 on custom domains), JMAP, Mozilla's
// ISP database and the domain's own autoconfig (what Thunderbird uses), SRV
// records for IMAP, POP3 and submission, and last the common host names.
package discover

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Kind         string `json:"kind"` // google, microsoft, imap, pop3, jmap
	Domain       string `json:"domain"`
	Provider     string `json:"provider"`
	IMAPHost     string `json:"imapHost,omitempty"`
	IMAPPort     int    `json:"imapPort,omitempty"`
	IMAPSecurity string `json:"imapSecurity,omitempty"`
	POP3Host     string `json:"pop3Host,omitempty"`
	POP3Port     int    `json:"pop3Port,omitempty"`
	POP3Security string `json:"pop3Security,omitempty"`
	SMTPHost     string `json:"smtpHost,omitempty"`
	SMTPPort     int    `json:"smtpPort,omitempty"`
	SMTPSecurity string `json:"smtpSecurity,omitempty"`
	Username     string `json:"username,omitempty"` // when the server wants something other than the address
	JMAPURL      string `json:"jmapUrl,omitempty"`
	Note         string `json:"note,omitempty"`
	// Source says where the answer came from, so a wrong guess is easy to understand.
	Source string `json:"source,omitempty"`
}

// Gmail over IMAP and SMTP, for the app password path.
var gmailServers = Result{IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "ssl"}

var known = map[string]Result{
	"gmail.com":      {Kind: "google", Provider: "Google", Source: "known provider"},
	"googlemail.com": {Kind: "google", Provider: "Google", Source: "known provider"},
	"outlook.com":    {Kind: "microsoft", Provider: "Microsoft", Source: "known provider"},
	"hotmail.com":    {Kind: "microsoft", Provider: "Microsoft", Source: "known provider"},
	"live.com":       {Kind: "microsoft", Provider: "Microsoft", Source: "known provider"},
	"msn.com":        {Kind: "microsoft", Provider: "Microsoft", Source: "known provider"},
	"icloud.com":     {Kind: "imap", Provider: "iCloud", IMAPHost: "imap.mail.me.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: "starttls", Note: "iCloud needs an app-specific password.", Source: "known provider"},
	"yahoo.com":      {Kind: "imap", Provider: "Yahoo", IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 465, SMTPSecurity: "ssl", Note: "Yahoo needs an app password.", Source: "known provider"},
	"fastmail.com":   {Kind: "jmap", Provider: "Fastmail", JMAPURL: "https://api.fastmail.com", Note: "Use a Fastmail app password.", Source: "known provider"},
	"proton.me":      {Kind: "imap", Provider: "Proton", IMAPHost: "127.0.0.1", IMAPPort: 1143, IMAPSecurity: "starttls", SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPSecurity: "starttls", Note: "Proton Mail needs its Bridge running on this computer.", Source: "known provider"},
}

func google(domain, provider, source string) Result {
	r := gmailServers
	r.Kind, r.Domain, r.Provider, r.Source = "google", domain, provider, source
	return r
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
	// ISPDB is the base of Mozilla's database. Replaceable in tests.
	ISPDB string
}

func New() *Discoverer {
	return &Discoverer{Res: net.DefaultResolver, ISPDB: "https://autoconfig.thunderbird.net/v1.1/",
		HTTP: &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}}
}

func (d *Discoverer) Find(ctx context.Context, email string) (Result, error) {
	email = strings.TrimSpace(email)
	i := strings.LastIndex(email, "@")
	if i < 1 || i == len(email)-1 {
		return Result{}, fmt.Errorf("that does not look like an email address")
	}
	domain := strings.ToLower(email[i+1:])
	if r, ok := known[domain]; ok {
		r.Domain = domain
		if r.Kind == "google" {
			return google(domain, r.Provider, r.Source), nil
		}
		return r, nil
	}
	if strings.HasSuffix(domain, ".onmicrosoft.com") {
		return Result{Kind: "microsoft", Domain: domain, Provider: "Microsoft 365", Source: "Microsoft domain"}, nil
	}
	// MX: Google Workspace and Microsoft 365 hide behind custom domains
	if mx, err := d.Res.LookupMX(ctx, domain); err == nil {
		for _, m := range mx {
			h := strings.ToLower(strings.TrimSuffix(m.Host, "."))
			switch {
			case strings.HasSuffix(h, ".google.com") || strings.HasSuffix(h, ".googlemail.com"):
				return google(domain, "Google Workspace", "MX record"), nil
			case strings.HasSuffix(h, ".mail.protection.outlook.com") || strings.HasSuffix(h, ".outlook.com"):
				return Result{Kind: "microsoft", Domain: domain, Provider: "Microsoft 365", Source: "MX record"}, nil
			}
		}
	}
	// JMAP: a 401 or 200 on the well-known URL means the server speaks it
	for _, host := range []string{domain, "mail." + domain, "jmap." + domain} {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+host+"/.well-known/jmap", nil)
		if resp, err := d.HTTP.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 401 || resp.StatusCode == 200 {
				return Result{Kind: "jmap", Domain: domain, Provider: "JMAP server", JMAPURL: "https://" + host, Source: "JMAP well-known"}, nil
			}
		}
	}
	// Mozilla's database, then the domain's own autoconfig
	cfgs := []struct{ url, src string }{
		{d.ISPDB + url.PathEscape(domain), "Mozilla ISP database"},
		{"https://autoconfig." + domain + "/mail/config-v1.1.xml?emailaddress=" + url.QueryEscape(email), "the domain's autoconfig"},
		{"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml", "the domain's autoconfig"},
	}
	for _, c := range cfgs {
		if r, ok := d.autoconfig(ctx, c.url, domain, email); ok {
			r.Source = c.src
			return r, nil
		}
	}
	if r, ok := d.srv(ctx, domain); ok {
		r.Source = "SRV records"
		return r, nil
	}
	for _, h := range []string{"imap." + domain, "mail." + domain} {
		if _, err := d.Res.LookupHost(ctx, h); err == nil {
			return Result{Kind: "imap", Domain: domain, Provider: "IMAP server", IMAPHost: h, IMAPPort: 993, IMAPSecurity: "ssl",
				SMTPHost: strings.Replace(strings.Replace(h, "imap.", "smtp.", 1), "mail.", "smtp.", 1), SMTPPort: 587, SMTPSecurity: "starttls",
				Note: "These server names are a guess. Check them.", Source: "common host names"}, nil
		}
	}
	return Result{Kind: "imap", Domain: domain, Provider: "IMAP server", IMAPPort: 993, IMAPSecurity: "ssl", SMTPPort: 587, SMTPSecurity: "starttls",
		Note: "Could not find this server automatically. Enter its address.", Source: "manual"}, nil
}

type acServer struct {
	Type       string `xml:"type,attr"`
	Hostname   string `xml:"hostname"`
	Port       string `xml:"port"`
	SocketType string `xml:"socketType"`
	Username   string `xml:"username"`
}

type autoconfigXML struct {
	Incoming []acServer `xml:"emailProvider>incomingServer"`
	Outgoing []acServer `xml:"emailProvider>outgoingServer"`
	Display  string     `xml:"emailProvider>displayName"`
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

func username(tpl, email string) string {
	local := email
	if i := strings.Index(email, "@"); i > 0 {
		local = email[:i]
	}
	switch strings.ToUpper(tpl) {
	case "%EMAILLOCALPART%":
		return local
	case "%EMAILADDRESS%", "":
		return ""
	}
	return strings.NewReplacer("%EMAILADDRESS%", email, "%EMAILLOCALPART%", local).Replace(tpl)
}

func (d *Discoverer) autoconfig(ctx context.Context, u, domain, email string) (Result, bool) {
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
	r := Result{Kind: "imap", Domain: domain, Provider: "Mail server"}
	if x.Display != "" {
		r.Provider = x.Display
	}
	for _, in := range x.Incoming {
		port, _ := strconv.Atoi(in.Port)
		switch strings.ToLower(in.Type) {
		case "imap":
			if r.IMAPHost == "" {
				r.IMAPHost, r.IMAPPort, r.IMAPSecurity = in.Hostname, port, sock(in.SocketType)
				r.Username = username(in.Username, email)
			}
		case "pop3":
			if r.POP3Host == "" {
				r.POP3Host, r.POP3Port, r.POP3Security = in.Hostname, port, sock(in.SocketType)
				if r.Username == "" {
					r.Username = username(in.Username, email)
				}
			}
		}
	}
	for _, o := range x.Outgoing {
		if strings.EqualFold(o.Type, "smtp") {
			r.SMTPHost, r.SMTPSecurity = o.Hostname, sock(o.SocketType)
			r.SMTPPort, _ = strconv.Atoi(o.Port)
			break
		}
	}
	switch {
	case r.IMAPHost != "":
	case r.POP3Host != "":
		r.Kind = "pop3"
	default:
		return Result{}, false
	}
	return r, true
}

func (d *Discoverer) srv(ctx context.Context, domain string) (Result, bool) {
	r := Result{Kind: "imap", Domain: domain, Provider: "Mail server"}
	pick := func(service string) (string, int, bool) {
		if _, s, err := d.Res.LookupSRV(ctx, service, "tcp", domain); err == nil && len(s) > 0 && strings.TrimSuffix(s[0].Target, ".") != "" {
			return strings.TrimSuffix(s[0].Target, "."), int(s[0].Port), true
		}
		return "", 0, false
	}
	if h, p, ok := pick("imaps"); ok {
		r.IMAPHost, r.IMAPPort, r.IMAPSecurity = h, p, "ssl"
	} else if h, p, ok := pick("imap"); ok {
		r.IMAPHost, r.IMAPPort, r.IMAPSecurity = h, p, "starttls"
	}
	if h, p, ok := pick("pop3s"); ok {
		r.POP3Host, r.POP3Port, r.POP3Security = h, p, "ssl"
	} else if h, p, ok := pick("pop3"); ok {
		r.POP3Host, r.POP3Port, r.POP3Security = h, p, "starttls"
	}
	if h, p, ok := pick("submissions"); ok {
		r.SMTPHost, r.SMTPPort, r.SMTPSecurity = h, p, "ssl"
	} else if h, p, ok := pick("submission"); ok {
		r.SMTPHost, r.SMTPPort, r.SMTPSecurity = h, p, "starttls"
	}
	switch {
	case r.IMAPHost != "":
	case r.POP3Host != "":
		r.Kind = "pop3"
	default:
		return Result{}, false
	}
	return r, true
}
