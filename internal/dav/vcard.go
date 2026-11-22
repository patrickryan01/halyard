package dav

import (
	"strings"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func vunescape(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

func vescape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`).Replace(strings.ReplaceAll(s, "\r", ""))
}

// ParseVCard reads one vCard (versions 3 and 4) into a contact: the first email and phone, and the organization.
func ParseVCard(data string) mailtypes.Contact {
	var c mailtypes.Contact
	data = strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
	var lines []string
	for _, l := range strings.Split(data, "\n") {
		if len(l) > 0 && (l[0] == ' ' || l[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	var n string
	for _, l := range lines {
		i := strings.Index(l, ":")
		if i < 0 {
			continue
		}
		name, val := strings.ToUpper(l[:i]), l[i+1:]
		if j := strings.Index(name, ";"); j >= 0 {
			name = name[:j]
		}
		if k := strings.LastIndex(name, "."); k >= 0 { // item1.EMAIL
			name = name[k+1:]
		}
		switch name {
		case "UID":
			c.ID = val
		case "FN":
			c.Name = vunescape(val)
		case "N":
			p := strings.Split(val, ";")
			n = strings.TrimSpace(vunescape(strings.TrimSpace(strings.Join([]string{get(p, 3), get(p, 1), get(p, 2), get(p, 0)}, " "))))
		case "EMAIL":
			if c.Email == "" {
				c.Email = vunescape(val)
			}
		case "TEL":
			if c.Phone == "" {
				c.Phone = vunescape(val)
			}
		case "ORG":
			if c.Org == "" {
				c.Org = vunescape(strings.Split(val, ";")[0])
			}
		}
	}
	if c.Name == "" {
		c.Name = strings.Join(strings.Fields(n), " ")
	}
	return c
}

func get(p []string, i int) string {
	if i < len(p) {
		return p[i]
	}
	return ""
}

// BuildVCard writes a contact as a vCard 3.0, which every address book takes.
func BuildVCard(c mailtypes.Contact, uid string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	b.WriteString("UID:" + uid + "\r\n")
	name := c.Name
	if name == "" {
		name = c.Email
	}
	b.WriteString("FN:" + vescape(name) + "\r\n")
	parts := strings.Fields(name)
	family, given := "", name
	if len(parts) > 1 {
		family, given = parts[len(parts)-1], strings.Join(parts[:len(parts)-1], " ")
	}
	b.WriteString("N:" + vescape(family) + ";" + vescape(given) + ";;;\r\n")
	if c.Email != "" {
		b.WriteString("EMAIL;TYPE=INTERNET:" + vescape(c.Email) + "\r\n")
	}
	if c.Phone != "" {
		b.WriteString("TEL:" + vescape(c.Phone) + "\r\n")
	}
	if c.Org != "" {
		b.WriteString("ORG:" + vescape(c.Org) + "\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}
