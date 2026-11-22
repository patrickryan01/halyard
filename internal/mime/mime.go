// Package mime builds outgoing RFC 5322 messages and parses incoming ones. It
// is shared by the IMAP, JMAP, Gmail and Graph plugins, which all deal in raw
// messages somewhere (send, MIME import, body fetch).
package mime

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func toGo(as []mailtypes.Address) []*gomail.Address {
	var out []*gomail.Address
	for _, a := range as {
		out = append(out, &gomail.Address{Name: a.Name, Address: a.Email})
	}
	return out
}

// MessageID makes a Message-ID for a new message.
func MessageID(domain string, nonce string) string {
	if domain == "" {
		domain = "localhost"
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), nonce, domain)
}

// Build writes the message as RFC 5322 with a text and an HTML alternative and
// the attachments as mixed parts. Bcc is deliberately not written as a header:
// the SMTP envelope carries it.
func Build(o mailtypes.Outgoing, msgID string) ([]byte, error) { return build(o, msgID, false) }

// BuildDraft is Build for a message that is saved, not sent: it keeps the Bcc
// header, so reopening the draft still shows who is on Bcc.
func BuildDraft(o mailtypes.Outgoing, msgID string) ([]byte, error) { return build(o, msgID, true) }

func build(o mailtypes.Outgoing, msgID string, keepBcc bool) ([]byte, error) {
	if len(o.Raw) > 0 && !keepBcc {
		return o.Raw, nil
	}
	var h gomail.Header
	if o.Date.IsZero() {
		h.SetDate(time.Now())
	} else {
		h.SetDate(o.Date)
	}
	h.SetSubject(o.Subject)
	h.SetAddressList("From", toGo([]mailtypes.Address{o.From}))
	if len(o.To) > 0 {
		h.SetAddressList("To", toGo(o.To))
	}
	if len(o.Cc) > 0 {
		h.SetAddressList("Cc", toGo(o.Cc))
	}
	if keepBcc && len(o.Bcc) > 0 {
		h.SetAddressList("Bcc", toGo(o.Bcc))
	}
	if len(o.ReplyTo) > 0 {
		h.SetAddressList("Reply-To", toGo(o.ReplyTo))
	}
	if o.ReceiptRequested {
		h.Set("Disposition-Notification-To", (&gomail.Address{Name: o.From.Name, Address: o.From.Email}).String())
	}
	for k, v := range o.ExtraHeaders {
		if !strings.ContainsAny(k+v, "\r\n") {
			h.Set(k, v)
		}
	}
	h.Set("Message-ID", msgID)
	if o.InReplyTo != "" {
		h.Set("In-Reply-To", o.InReplyTo)
	}
	if len(o.References) > 0 {
		h.Set("References", strings.Join(o.References, " "))
	}
	h.Set("MIME-Version", "1.0")
	var buf bytes.Buffer
	w, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	var ih gomail.InlineHeader
	ih.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	text := o.BodyText
	if text == "" && o.BodyHTML != "" {
		text = htmlToText(o.BodyHTML)
	}
	if o.BodyHTML == "" {
		tw, err := w.CreateSingleInline(ih)
		if err != nil {
			return nil, err
		}
		io.WriteString(tw, text)
		tw.Close()
	} else {
		// multipart/alternative inside the mixed container
		aw, err := w.CreateInline()
		if err != nil {
			return nil, err
		}
		tp, _ := aw.CreatePart(ih)
		io.WriteString(tp, text)
		tp.Close()
		var hh gomail.InlineHeader
		hh.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		hp, _ := aw.CreatePart(hh)
		io.WriteString(hp, o.BodyHTML)
		hp.Close()
		aw.Close()
	}
	for _, a := range o.Attachments {
		var ah gomail.AttachmentHeader
		ct := a.ContentType
		if ct == "" {
			ct = mime.TypeByExtension(strings.ToLower(extOf(a.Filename)))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		ah.Set("Content-Type", ct)
		ah.SetFilename(a.Filename)
		aw, err := w.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		aw.Write(a.Data)
		aw.Close()
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func extOf(n string) string {
	if i := strings.LastIndex(n, "."); i >= 0 {
		return n[i:]
	}
	return ""
}

func htmlToText(h string) string {
	r := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n", "</div>", "\n")
	h = r.Replace(h)
	var b strings.Builder
	in := false
	for _, c := range h {
		switch {
		case c == '<':
			in = true
		case c == '>':
			in = false
		case !in:
			b.WriteRune(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// Envelope lists every recipient for SMTP, Bcc included.
func Envelope(o mailtypes.Outgoing) []string {
	var rc []string
	for _, l := range [][]mailtypes.Address{o.To, o.Cc, o.Bcc} {
		for _, a := range l {
			rc = append(rc, a.Email)
		}
	}
	return rc
}

func fromGo(as []*gomail.Address) []mailtypes.Address {
	var out []mailtypes.Address
	for _, a := range as {
		out = append(out, mailtypes.Address{Name: a.Name, Email: a.Address})
	}
	return out
}

func init() {
	// accept legacy charsets in headers and bodies
	message.CharsetReader = charsetReader
}

// Parsed is a raw message broken into what the UI shows.
type Parsed struct {
	Subject    string
	From       mailtypes.Address
	To, Cc     []mailtypes.Address
	ReplyTo    []mailtypes.Address
	Date       time.Time
	MessageID  string
	InReplyTo  string
	References []string
	Text, HTML string
	Attach     []mailtypes.Attachment
	// Parts holds attachment bytes by index so a provider that has the raw
	// message can serve them without another round trip.
	Parts [][]byte
	// ICS is the calendar invitation in the message, if there is one.
	ICS                 string
	ListUnsubscribe     string
	ListUnsubscribePost bool
	ReceiptTo           string
	Size                int64
}

// IsCalendar says whether an attachment is a calendar invitation or a calendar file.
func IsCalendar(a mailtypes.Attachment) bool {
	ct := strings.ToLower(a.ContentType)
	return strings.HasPrefix(ct, "text/calendar") || strings.HasPrefix(ct, "application/ics") || strings.HasSuffix(strings.ToLower(a.Filename), ".ics")
}

// Parse reads a raw RFC 5322 message.
func Parse(raw []byte) (Parsed, error) {
	var p Parsed
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) {
		return p, err
	}
	if mr == nil {
		return p, err
	}
	h := mr.Header
	p.Subject, _ = h.Subject()
	if d, err := h.Date(); err == nil {
		p.Date = d
	}
	if fr, err := h.AddressList("From"); err == nil && len(fr) > 0 {
		p.From = mailtypes.Address{Name: fr[0].Name, Email: fr[0].Address}
	}
	p.To, p.Cc = nil, nil
	if l, err := h.AddressList("To"); err == nil {
		p.To = fromGo(l)
	}
	if l, err := h.AddressList("Cc"); err == nil {
		p.Cc = fromGo(l)
	}
	if l, err := h.AddressList("Reply-To"); err == nil {
		p.ReplyTo = fromGo(l)
	}
	p.MessageID = strings.TrimSpace(h.Get("Message-ID"))
	p.InReplyTo = strings.TrimSpace(h.Get("In-Reply-To"))
	p.References = strings.Fields(h.Get("References"))
	p.ListUnsubscribe = strings.TrimSpace(h.Get("List-Unsubscribe"))
	p.ListUnsubscribePost = strings.Contains(strings.ToLower(h.Get("List-Unsubscribe-Post")), "one-click")
	p.ReceiptTo = strings.TrimSpace(h.Get("Disposition-Notification-To"))
	p.Size = int64(len(raw))
	idx := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}
		if part == nil {
			continue
		}
		switch ph := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(part.Body)
			switch ct {
			case "text/plain":
				if p.Text == "" {
					p.Text = string(b)
				} else {
					p.Text += "\n" + string(b)
				}
			case "text/html":
				if p.HTML == "" {
					p.HTML = string(b)
				} else {
					p.HTML += string(b)
				}
			case "text/calendar":
				if p.ICS == "" {
					p.ICS = string(b)
				}
				p.Attach = append(p.Attach, mailtypes.Attachment{ID: fmt.Sprint(idx), Filename: "invite.ics", ContentType: ct, Size: int64(len(b))})
				p.Parts = append(p.Parts, b)
				idx++
			default:
				name := ct
				_, cparams, _ := ph.ContentType()
				if n := cparams["name"]; n != "" {
					name = n
				}
				if _, dparams, err := ph.ContentDisposition(); err == nil && dparams["filename"] != "" {
					name = dparams["filename"]
				}
				cid := strings.Trim(ph.Get("Content-ID"), "<> ")
				p.Attach = append(p.Attach, mailtypes.Attachment{ID: fmt.Sprint(idx), Filename: name, ContentType: ct, Size: int64(len(b)), Inline: true, ContentID: cid})
				p.Parts = append(p.Parts, b)
				idx++
			}
		case *gomail.AttachmentHeader:
			fn, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(part.Body)
			cid := strings.Trim(ph.Get("Content-ID"), "<> ")
			disp, _, _ := ph.ContentDisposition()
			if fn == "" {
				fn = "attachment"
			}
			if p.ICS == "" && (strings.HasPrefix(strings.ToLower(ct), "text/calendar") || strings.HasSuffix(strings.ToLower(fn), ".ics")) {
				p.ICS = string(b)
			}
			p.Attach = append(p.Attach, mailtypes.Attachment{ID: fmt.Sprint(idx), Filename: fn, ContentType: ct, Size: int64(len(b)),
				Inline: disp == "inline" && cid != "", ContentID: cid})
			p.Parts = append(p.Parts, b)
			idx++
		}
	}
	return p, nil
}

// Snippet makes the one line preview.
func Snippet(text, html string) string {
	t := text
	if t == "" {
		t = htmlToText(html)
	}
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > 160 {
		t = string(r[:160])
	}
	return t
}

// ParseAddress parses one "Name <a@b>" string.
func ParseAddress(s string) (mailtypes.Address, bool) {
	a, err := mail.ParseAddress(s)
	if err != nil {
		return mailtypes.Address{}, false
	}
	return mailtypes.Address{Name: a.Name, Email: a.Address}, true
}

// ParseList parses a comma separated address list, as typed in the composer.
func ParseList(s string) []mailtypes.Address {
	l, err := mail.ParseAddressList(s)
	if err != nil {
		var out []mailtypes.Address
		for _, p := range strings.Split(s, ",") {
			if a, ok := ParseAddress(strings.TrimSpace(p)); ok {
				out = append(out, a)
			}
		}
		return out
	}
	var out []mailtypes.Address
	for _, a := range l {
		out = append(out, mailtypes.Address{Name: a.Name, Email: a.Address})
	}
	return out
}
