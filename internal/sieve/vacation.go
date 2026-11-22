package sieve

import (
	"regexp"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

const (
	vacBegin = "# halyard-vacation-begin"
	vacEnd   = "# halyard-vacation-end"
	reqMark  = "# halyard-vacation-require"
)

// header splits a script into its leading comments and require commands, and the rest.
func header(script string) (head, rest string) {
	lines := strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n")
	i := 0
	inReq := false
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		switch {
		case inReq:
			if strings.Contains(t, ";") {
				inReq = false
			}
		case t == "" || strings.HasPrefix(t, "#"):
		case strings.HasPrefix(strings.ToLower(t), "require"):
			if !strings.Contains(t, ";") {
				inReq = true
			}
		default:
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i:], "\n")
		}
	}
	return strings.Join(lines, "\n"), ""
}

func sieveString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.ReplaceAll(s, "\r", "")) + `"`
}

func textBlock(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	b.WriteString("text:\n")
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.HasPrefix(l, ".") {
			l = "." + l
		}
		b.WriteString(l + "\n")
	}
	b.WriteString(".\n")
	return b.String()
}

// WithoutVacation removes the block this app keeps.
func WithoutVacation(script string) string {
	var out []string
	skip := false
	for _, l := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == vacBegin:
			skip = true
			continue
		case t == vacEnd:
			skip = false
			continue
		case t == reqMark:
			skip = true // the require line follows the marker
			continue
		}
		if skip && strings.HasPrefix(strings.ToLower(t), "require") {
			skip = false
			continue
		}
		if !skip {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// WithVacation puts the vacation reply in a script, replacing the one this app wrote before. Disabled removes it.
func WithVacation(script string, v mailtypes.Vacation) string {
	base := WithoutVacation(script)
	if !v.Enabled {
		return strings.TrimLeft(base, "\n")
	}
	head, rest := header(base)
	reqs := []string{`"vacation"`}
	cond := ""
	if !v.Start.IsZero() || !v.End.IsZero() {
		reqs = append(reqs, `"date"`, `"relational"`)
		var tests []string
		if !v.Start.IsZero() {
			tests = append(tests, `currentdate :value "ge" "iso8601" "`+v.Start.UTC().Format("2006-01-02T15:04:05Z")+`"`)
		}
		if !v.End.IsZero() {
			tests = append(tests, `currentdate :value "le" "iso8601" "`+v.End.UTC().Format("2006-01-02T15:04:05Z")+`"`)
		}
		cond = "if allof (" + strings.Join(tests, ", ") + ") "
	}
	subj := ""
	if v.Subject != "" {
		subj = " :subject " + sieveString(v.Subject)
	}
	var b strings.Builder
	b.WriteString(reqMark + "\nrequire [" + strings.Join(reqs, ",") + "];\n")
	if strings.TrimSpace(head) != "" {
		b.WriteString(strings.TrimSpace(head) + "\n")
	}
	b.WriteString(vacBegin + "\n")
	b.WriteString(cond + "{ vacation :days 1" + subj + " " + textBlock(v.Body) + "}\n")
	b.WriteString(vacEnd + "\n")
	if strings.TrimSpace(rest) != "" {
		b.WriteString(strings.TrimLeft(rest, "\n") + "\n")
	}
	return b.String()
}

var isoRe = regexp.MustCompile(`"(ge|le)" "iso8601" "([0-9T:\-Z]+)"`)

// ParseVacation reads back the reply this app wrote. A script with no such block has no vacation reply.
func ParseVacation(script string) mailtypes.Vacation {
	script = strings.ReplaceAll(script, "\r\n", "\n")
	i := strings.Index(script, vacBegin)
	j := strings.Index(script, vacEnd)
	if i < 0 || j < i {
		return mailtypes.Vacation{}
	}
	blk := script[i:j]
	v := mailtypes.Vacation{Enabled: true}
	for _, m := range isoRe.FindAllStringSubmatch(blk, -1) {
		t, err := time.Parse("2006-01-02T15:04:05Z", m[2])
		if err != nil {
			continue
		}
		if m[1] == "ge" {
			v.Start = t
		} else {
			v.End = t
		}
	}
	if k := strings.Index(blk, ":subject \""); k >= 0 {
		rest := blk[k+len(":subject \""):]
		var sb strings.Builder
		for n := 0; n < len(rest); n++ {
			if rest[n] == '\\' && n+1 < len(rest) {
				n++
				sb.WriteByte(rest[n])
				continue
			}
			if rest[n] == '"' {
				break
			}
			sb.WriteByte(rest[n])
		}
		v.Subject = sb.String()
	}
	if k := strings.Index(blk, "text:\n"); k >= 0 {
		var body []string
		for _, l := range strings.Split(blk[k+len("text:\n"):], "\n") {
			if l == "." {
				break
			}
			if strings.HasPrefix(l, "..") {
				l = l[1:]
			}
			body = append(body, l)
		}
		v.Body = strings.Join(body, "\n")
	}
	return v
}

// SetVacation edits the account's active script (or makes one) so it holds the vacation reply, and activates it.
func (c *Client) SetVacation(v mailtypes.Vacation) error {
	scripts, err := c.List()
	if err != nil {
		return err
	}
	name, body := "halyard", ""
	for _, s := range scripts {
		if s.Active {
			name = s.Name
			if body, err = c.Get(s.Name); err != nil {
				return err
			}
		}
	}
	next := WithVacation(body, v)
	if strings.TrimSpace(next) == "" {
		next = "# no rules\n"
	}
	if err := c.Put(name, next); err != nil {
		return err
	}
	return c.Activate(name)
}

// GetVacation reads the reply from the active script.
func (c *Client) GetVacation() (mailtypes.Vacation, error) {
	scripts, err := c.List()
	if err != nil {
		return mailtypes.Vacation{}, err
	}
	for _, s := range scripts {
		if s.Active {
			body, err := c.Get(s.Name)
			if err != nil {
				return mailtypes.Vacation{}, err
			}
			return ParseVacation(body), nil
		}
	}
	return mailtypes.Vacation{}, nil
}
