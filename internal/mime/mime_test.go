package mime

import (
	"strings"
	"testing"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func TestBuildParseRoundTrip(t *testing.T) {
	o := mailtypes.Outgoing{
		From: mailtypes.Address{Name: "Pat Ryan", Email: "pat@x.test"},
		To:   []mailtypes.Address{{Name: "Dana", Email: "dana@y.test"}}, Bcc: []mailtypes.Address{{Email: "secret@z.test"}},
		Subject: "Quarterly, with ünïcode", BodyText: "plain body", BodyHTML: "<p>html <b>body</b></p>",
		InReplyTo: "<a@b>", References: []string{"<a@b>"},
		Attachments: []mailtypes.OutAttachment{{Filename: "report.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 fake")}},
	}
	raw, err := Build(o, "<id1@x.test>")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret@z.test") {
		t.Fatal("bcc leaked into headers")
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != o.Subject || p.From.Email != "pat@x.test" || p.To[0].Name != "Dana" {
		t.Fatalf("headers: %+v", p)
	}
	if p.Text != "plain body" || !strings.Contains(p.HTML, "<b>body</b>") {
		t.Fatalf("bodies: %q %q", p.Text, p.HTML)
	}
	if len(p.Attach) != 1 || p.Attach[0].Filename != "report.pdf" || string(p.Parts[0]) != "%PDF-1.4 fake" {
		t.Fatalf("attachment: %+v", p.Attach)
	}
	if p.InReplyTo != "<a@b>" || len(Envelope(o)) != 2 {
		t.Fatalf("threading or envelope")
	}
}
