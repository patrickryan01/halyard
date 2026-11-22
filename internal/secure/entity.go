package secure

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
)

// A message is split into its header lines and its body. Header lines are kept as written (with folding) so the
// message can be put back together; the lookup helpers read them unfolded.
type entityParts struct {
	headers []string // each header field, folded lines joined with CRLF
	body    []byte
}

func canon(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}

func splitEntity(raw []byte) entityParts {
	raw = canon(raw)
	var head, body []byte
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		head, body = raw[:i], raw[i+4:]
	} else if bytes.HasPrefix(raw, []byte("\r\n")) {
		body = raw[2:]
	} else {
		head = raw
	}
	var e entityParts
	e.body = body
	for _, l := range strings.Split(string(head), "\r\n") {
		if l == "" {
			continue
		}
		if (l[0] == ' ' || l[0] == '\t') && len(e.headers) > 0 {
			e.headers[len(e.headers)-1] += "\r\n" + l
			continue
		}
		e.headers = append(e.headers, l)
	}
	return e
}

func (e entityParts) get(name string) string {
	p := strings.ToLower(name) + ":"
	for _, h := range e.headers {
		if strings.HasPrefix(strings.ToLower(h), p) {
			v := h[len(p):]
			v = strings.NewReplacer("\r\n ", " ", "\r\n\t", " ").Replace(v)
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func isContentHeader(h string) bool {
	l := strings.ToLower(h)
	return strings.HasPrefix(l, "content-") || strings.HasPrefix(l, "mime-version:")
}

// contentOf is the entity a message body is made of: its Content headers and body, as one block.
func (e entityParts) content() []byte {
	var b bytes.Buffer
	for _, h := range e.headers {
		if strings.HasPrefix(strings.ToLower(h), "content-") {
			b.WriteString(h + "\r\n")
		}
	}
	b.WriteString("\r\n")
	b.Write(e.body)
	return b.Bytes()
}

// outer is the message's own header fields: everything that is not about the content.
func (e entityParts) outer() []string {
	var out []string
	for _, h := range e.headers {
		if !isContentHeader(h) {
			out = append(out, h)
		}
	}
	return out
}

func assemble(outer []string, contentHeaders []string, body []byte) []byte {
	var b bytes.Buffer
	for _, h := range outer {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	for _, h := range contentHeaders {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("\r\n")
	b.Write(body)
	return b.Bytes()
}

func boundary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "=_halyard_" + hex.EncodeToString(b)
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}

func b64(data []byte) string { return wrap76(base64.StdEncoding.EncodeToString(data)) }

// decodeBody undoes the part's transfer encoding.
func decodeBody(cte string, body []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		clean := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, string(body))
		return base64.StdEncoding.DecodeString(clean)
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
	}
	return body, nil
}

// splitMultipart returns the raw text of each part of a multipart body, exactly as it was between the boundary lines.
// The line break before a boundary belongs to the boundary, not to the part.
func splitMultipart(body []byte, bound string) ([][]byte, error) {
	marker := []byte("\r\n--" + bound)
	b := append([]byte("\r\n"), body...)
	var idx []int
	for from := 0; ; {
		i := bytes.Index(b[from:], marker)
		if i < 0 {
			break
		}
		idx = append(idx, from+i)
		from += i + len(marker)
	}
	if len(idx) == 0 {
		return nil, errors.New("the message is not built the way it says")
	}
	var parts [][]byte
	for k, at := range idx {
		rest := b[at+len(marker):]
		if bytes.HasPrefix(rest, []byte("--")) {
			break
		}
		nl := bytes.Index(rest, []byte("\r\n"))
		if nl < 0 {
			break
		}
		start := at + len(marker) + nl + 2
		end := len(b)
		if k+1 < len(idx) {
			end = idx[k+1]
		}
		if start > end {
			start = end
		}
		parts = append(parts, b[start:end])
	}
	return parts, nil
}

func mediaType(ct string) (string, map[string]string) {
	m, p, err := mime.ParseMediaType(ct)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])), map[string]string{}
	}
	return m, p
}
