package discover

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRes struct{ mx map[string][]string }

func (f fakeRes) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	var out []*net.MX
	for _, h := range f.mx[n] {
		out = append(out, &net.MX{Host: h})
	}
	if out == nil {
		return nil, errors.New("none")
	}
	return out, nil
}
func (fakeRes) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, errors.New("none")
}
func (fakeRes) LookupHost(context.Context, string) ([]string, error) { return nil, errors.New("none") }

func TestKnownMXAndAutoconfig(t *testing.T) {
	d := &Discoverer{Res: fakeRes{mx: map[string][]string{
		"corp.test": {"aspmx.l.google.com."},
		"m365.test": {"m365-test.mail.protection.outlook.com."},
	}}, HTTP: http.DefaultClient}
	for email, kind := range map[string]string{"a@gmail.com": "google", "a@outlook.com": "microsoft", "a@corp.test": "google", "a@m365.test": "microsoft", "a@fastmail.com": "jmap"} {
		r, err := d.Find(context.Background(), email)
		if err != nil || r.Kind != kind {
			t.Fatalf("%s: got %+v %v want %s", email, r, err, kind)
		}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<clientConfig><emailProvider><incomingServer type="imap"><hostname>imap.x.test</hostname><port>993</port><socketType>SSL</socketType></incomingServer><outgoingServer type="smtp"><hostname>smtp.x.test</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer></emailProvider></clientConfig>`))
	}))
	defer srv.Close()
	d.HTTP = srv.Client()
	r, ok := d.autoconfig(context.Background(), srv.URL, "x.test", "a@x.test")
	if !ok || r.IMAPHost != "imap.x.test" || r.SMTPSecurity != "starttls" {
		t.Fatalf("%+v", r)
	}
	if _, err := d.Find(context.Background(), "nonsense"); err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatal("bad address should error")
	}
}

func TestGmailOnmicrosoftIspdbAndPop3(t *testing.T) {
	d := &Discoverer{Res: fakeRes{mx: map[string][]string{}}, HTTP: http.DefaultClient}
	r, _ := d.Find(context.Background(), "me@gmail.com")
	if r.Kind != "google" || r.IMAPHost != "imap.gmail.com" || r.SMTPHost != "smtp.gmail.com" {
		t.Fatalf("gmail must carry its IMAP and SMTP servers for the app password path: %+v", r)
	}
	r, _ = d.Find(context.Background(), "x@contoso.onmicrosoft.com")
	if r.Kind != "microsoft" {
		t.Fatalf("%+v", r)
	}
	// Mozilla's database answers for a domain that only has POP3
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/oldschool.test") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`<clientConfig><emailProvider><displayName>Old School</displayName><incomingServer type="pop3"><hostname>pop.oldschool.test</hostname><port>995</port><socketType>SSL</socketType><username>%EMAILLOCALPART%</username></incomingServer><outgoingServer type="smtp"><hostname>smtp.oldschool.test</hostname><port>465</port><socketType>SSL</socketType></outgoingServer></emailProvider></clientConfig>`))
	}))
	defer srv.Close()
	d = &Discoverer{Res: fakeRes{mx: map[string][]string{}}, HTTP: srv.Client(), ISPDB: srv.URL + "/v1.1/"}
	r, err := d.Find(context.Background(), "bob@oldschool.test")
	if err != nil || r.Kind != "pop3" || r.POP3Host != "pop.oldschool.test" || r.POP3Security != "ssl" || r.SMTPPort != 465 || r.Source != "Mozilla ISP database" {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Username != "bob" {
		t.Fatalf("the autoconfig says log in with the local part: %+v", r)
	}
}
