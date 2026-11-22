// Package ical reads and writes the small part of iCalendar (RFC 5545) and iTIP (RFC 5546) a mail client needs: show an
// invitation, answer it, and exchange events with a calendar server.
package ical

import (
	"fmt"
	"strings"
	"time"
)

type Attendee struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email"`
	PartStat string `json:"partStat,omitempty"` // NEEDS-ACTION, ACCEPTED, DECLINED, TENTATIVE
	Role     string `json:"role,omitempty"`
	RSVP     bool   `json:"rsvp,omitempty"`
}

// Event is one VEVENT.
type Event struct {
	UID         string     `json:"uid"`
	Method      string     `json:"method,omitempty"` // REQUEST, REPLY, CANCEL, PUBLISH
	Summary     string     `json:"summary"`
	Description string     `json:"description,omitempty"`
	Location    string     `json:"location,omitempty"`
	Start       time.Time  `json:"start"`
	End         time.Time  `json:"end"`
	AllDay      bool       `json:"allDay,omitempty"`
	Organizer   Attendee   `json:"organizer"`
	Attendees   []Attendee `json:"attendees,omitempty"`
	Sequence    int        `json:"sequence,omitempty"`
	Recurrence  string     `json:"recurrence,omitempty"` // the RRULE value, shown as written
	Status      string     `json:"status,omitempty"`
}

type prop struct {
	name   string
	params map[string]string
	value  string
}

// unfold joins continuation lines and splits into content lines.
func unfold(s string) []string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if len(l) > 0 && (l[0] == ' ' || l[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

func parseProp(l string) (prop, bool) {
	// the name and parameters end at the first colon that is not inside quotes
	inq, ci := false, -1
	for i, r := range l {
		if r == '"' {
			inq = !inq
		}
		if r == ':' && !inq {
			ci = i
			break
		}
	}
	if ci < 0 {
		return prop{}, false
	}
	head, val := l[:ci], l[ci+1:]
	parts := splitQ(head, ';')
	p := prop{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: val}
	for _, kv := range parts[1:] {
		if i := strings.Index(kv, "="); i > 0 {
			p.params[strings.ToUpper(kv[:i])] = strings.Trim(kv[i+1:], `"`)
		}
	}
	return p, true
}

func splitQ(s string, sep rune) []string {
	var out []string
	inq, start := false, 0
	for i, r := range s {
		if r == '"' {
			inq = !inq
		}
		if r == sep && !inq {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, ",", `\,`, ";", `\;`)
	return r.Replace(strings.ReplaceAll(s, "\r", ""))
}

func parseTime(p prop) (time.Time, bool, error) {
	v := p.value
	loc := time.UTC
	if tz := p.params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	return t, false, err
}

func attendee(p prop) Attendee {
	a := Attendee{Email: strings.TrimPrefix(strings.TrimPrefix(p.value, "mailto:"), "MAILTO:"), Name: p.params["CN"], PartStat: p.params["PARTSTAT"],
		Role: p.params["ROLE"], RSVP: strings.EqualFold(p.params["RSVP"], "TRUE")}
	return a
}

// Parse reads every VEVENT in an iCalendar text. The METHOD of the calendar goes on each event.
func Parse(ics string) ([]Event, error) {
	var evs []Event
	var cur *Event
	method := ""
	depth := 0
	for _, l := range unfold(ics) {
		if l == "" {
			continue
		}
		u := strings.ToUpper(l)
		switch {
		case u == "BEGIN:VEVENT":
			cur = &Event{}
			depth = 0
			continue
		case u == "END:VEVENT":
			if cur != nil {
				if cur.End.IsZero() {
					if cur.AllDay {
						cur.End = cur.Start.AddDate(0, 0, 1)
					} else {
						cur.End = cur.Start
					}
				}
				evs = append(evs, *cur)
			}
			cur = nil
			continue
		case strings.HasPrefix(u, "BEGIN:") && cur != nil:
			depth++ // a VALARM inside the event
			continue
		case strings.HasPrefix(u, "END:") && cur != nil && depth > 0:
			depth--
			continue
		}
		p, ok := parseProp(l)
		if !ok {
			continue
		}
		if cur == nil {
			if p.name == "METHOD" {
				method = strings.ToUpper(p.value)
			}
			continue
		}
		if depth > 0 {
			continue
		}
		switch p.name {
		case "UID":
			cur.UID = p.value
		case "SUMMARY":
			cur.Summary = unescape(p.value)
		case "DESCRIPTION":
			cur.Description = unescape(p.value)
		case "LOCATION":
			cur.Location = unescape(p.value)
		case "STATUS":
			cur.Status = strings.ToUpper(p.value)
		case "RRULE":
			cur.Recurrence = p.value
		case "SEQUENCE":
			fmt.Sscan(p.value, &cur.Sequence)
		case "DTSTART":
			t, allDay, err := parseTime(p)
			if err != nil {
				return nil, fmt.Errorf("bad DTSTART %q: %w", p.value, err)
			}
			cur.Start, cur.AllDay = t, allDay
		case "DTEND":
			t, _, err := parseTime(p)
			if err != nil {
				return nil, fmt.Errorf("bad DTEND %q: %w", p.value, err)
			}
			cur.End = t
		case "DURATION":
			if d, ok := parseDuration(p.value); ok {
				cur.End = cur.Start.Add(d)
			}
		case "ORGANIZER":
			cur.Organizer = attendee(p)
		case "ATTENDEE":
			cur.Attendees = append(cur.Attendees, attendee(p))
		}
	}
	for i := range evs {
		evs[i].Method = method
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("no event in the calendar text")
	}
	return evs, nil
}

func parseDuration(v string) (time.Duration, bool) {
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0, false
	}
	v = v[1:]
	var d time.Duration
	num := 0
	inTime := false
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			num = num*10 + int(r-'0')
		case r == 'T':
			inTime = true
		case r == 'W':
			d += time.Duration(num) * 7 * 24 * time.Hour
			num = 0
		case r == 'D':
			d += time.Duration(num) * 24 * time.Hour
			num = 0
		case r == 'H' && inTime:
			d += time.Duration(num) * time.Hour
			num = 0
		case r == 'M' && inTime:
			d += time.Duration(num) * time.Minute
			num = 0
		case r == 'S' && inTime:
			d += time.Duration(num) * time.Second
			num = 0
		}
	}
	if neg {
		d = -d
	}
	return d, true
}

func fmtTime(t time.Time, allDay bool) string {
	if allDay {
		return ";VALUE=DATE:" + t.Format("20060102")
	}
	return ":" + t.UTC().Format("20060102T150405Z")
}

func fold(l string) string {
	var b strings.Builder
	for len(l) > 74 {
		cut := 74
		for cut > 0 && l[cut]&0xC0 == 0x80 { // never split a UTF-8 sequence
			cut--
		}
		b.WriteString(l[:cut] + "\r\n ")
		l = l[cut:]
	}
	b.WriteString(l)
	return b.String()
}

// Build writes one event as a calendar. method is empty for a plain calendar object (what a calendar server stores).
func Build(e Event, method string) string {
	var lines []string
	add := func(s string) { lines = append(lines, fold(s)) }
	add("BEGIN:VCALENDAR")
	add("VERSION:2.0")
	add("PRODID:-//Halyard//Mail//EN")
	if method != "" {
		add("METHOD:" + method)
	}
	add("BEGIN:VEVENT")
	add("UID:" + e.UID)
	add("DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z"))
	add("DTSTART" + fmtTime(e.Start, e.AllDay))
	add("DTEND" + fmtTime(e.End, e.AllDay))
	add(fmt.Sprintf("SEQUENCE:%d", e.Sequence))
	if e.Summary != "" {
		add("SUMMARY:" + escape(e.Summary))
	}
	if e.Location != "" {
		add("LOCATION:" + escape(e.Location))
	}
	if e.Description != "" {
		add("DESCRIPTION:" + escape(e.Description))
	}
	if e.Recurrence != "" {
		add("RRULE:" + e.Recurrence)
	}
	if e.Status != "" {
		add("STATUS:" + e.Status)
	}
	if e.Organizer.Email != "" {
		cn := ""
		if e.Organizer.Name != "" {
			cn = ";CN=" + quote(e.Organizer.Name)
		}
		add("ORGANIZER" + cn + ":mailto:" + e.Organizer.Email)
	}
	for _, a := range e.Attendees {
		s := "ATTENDEE"
		if a.Name != "" {
			s += ";CN=" + quote(a.Name)
		}
		if a.Role != "" {
			s += ";ROLE=" + a.Role
		}
		if a.PartStat != "" {
			s += ";PARTSTAT=" + a.PartStat
		}
		if a.RSVP {
			s += ";RSVP=TRUE"
		}
		add(s + ":mailto:" + a.Email)
	}
	add("END:VEVENT")
	add("END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, "") + `"` }

// Reply builds the iTIP REPLY an attendee sends to the organizer: status is ACCEPTED, DECLINED or TENTATIVE. The
// reply carries only the replying attendee.
func Reply(e Event, who Attendee, status string) (string, error) {
	switch status {
	case "ACCEPTED", "DECLINED", "TENTATIVE":
	default:
		return "", fmt.Errorf("answer must be ACCEPTED, DECLINED or TENTATIVE")
	}
	if e.Organizer.Email == "" {
		return "", fmt.Errorf("the invitation names no organizer to answer")
	}
	r := e
	r.Attendees = []Attendee{{Name: who.Name, Email: who.Email, PartStat: status}}
	return Build(r, "REPLY"), nil
}

// Overlaps reports whether two events share any time.
func Overlaps(a, b Event) bool { return a.Start.Before(b.End) && b.Start.Before(a.End) }
