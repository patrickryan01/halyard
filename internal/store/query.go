package store

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A search is words plus operators, in the style people know from Gmail and Thunderbird:
//
//	from:ana  to:ops  cc:  bcc:  subject:invoice  filename:report.pdf  label:work  in:inbox  folder:Archive
//	is:unread  is:read  is:starred  is:snoozed  has:attachment  has:invite  has:unsubscribe
//	before:2026-11-01  after:2026/10/01  older_than:7d  newer_than:2w  larger:5M  smaller:200K
//	"an exact phrase"  -notthis  -from:spam  cats OR dogs
//
// Words not covered by an operator are matched with the full text index.

type term struct {
	neg bool
	op  string
	val string
}

var operators = map[string]bool{
	"from": true, "to": true, "cc": true, "bcc": true, "subject": true, "filename": true, "label": true, "in": true, "folder": true,
	"is": true, "has": true, "before": true, "after": true, "older_than": true, "newer_than": true, "larger": true, "smaller": true,
	"list": true, "id": true,
}

func splitTerms(text string) []term {
	var out []term
	rs := []rune(text)
	i := 0
	for i < len(rs) {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		neg := false
		if rs[i] == '-' && i+1 < len(rs) && !unicode.IsSpace(rs[i+1]) {
			neg = true
			i++
		}
		var tok strings.Builder
		quoted := false
		op := ""
		for i < len(rs) && (quoted || !unicode.IsSpace(rs[i])) {
			c := rs[i]
			switch {
			case c == '"':
				quoted = !quoted
				if op == "" {
					tok.WriteRune(c) // keep the quotes of a bare phrase so it stays a phrase
				}
			case c == ':' && op == "" && !quoted && operators[strings.ToLower(tok.String())]:
				op = strings.ToLower(tok.String())
				tok.Reset()
			default:
				tok.WriteRune(c)
			}
			i++
		}
		v := tok.String()
		if op == "" && strings.EqualFold(v, "OR") {
			out = append(out, term{op: "or"})
			continue
		}
		if v == "" && op == "" {
			continue
		}
		out = append(out, term{neg: neg, op: op, val: v})
	}
	return out
}

// searchSQL turns a search into where clauses on the messages table (alias m). ok is false when the search can match
// nothing at all, for example an operator with an impossible value.
func searchSQL(text string) (where []string, args []any, ok bool) {
	terms := splitTerms(text)
	if len(terms) == 0 {
		return nil, nil, true
	}
	var fts []string // joined by AND, with OR groups folded in
	var negFts []string
	pendingOr := false
	add := func(expr string) {
		if pendingOr && len(fts) > 0 {
			fts[len(fts)-1] = "(" + fts[len(fts)-1] + " OR " + expr + ")"
			pendingOr = false
			return
		}
		fts = append(fts, expr)
	}
	like := func(col, v string) (string, string) {
		return `LOWER(` + col + `) LIKE ?`, "%" + strings.ToLower(v) + "%"
	}
	for _, t := range terms {
		if t.op == "or" {
			pendingOr = true
			continue
		}
		var clause string
		var a []any
		switch t.op {
		case "":
			w := strings.Trim(t.val, `"`)
			phrase := strings.HasPrefix(t.val, `"`)
			w = ftsClean(w)
			if w == "" {
				continue
			}
			expr := `"` + w + `"`
			if !phrase {
				expr += "*"
			}
			if t.neg {
				negFts = append(negFts, expr)
			} else {
				add(expr)
			}
			continue
		case "from":
			c1, v := like("m.from_email", t.val)
			c2, _ := like("m.from_name", t.val)
			clause, a = "("+c1+" OR "+c2+")", []any{v, v}
		case "to":
			c, v := like("m.to_json", t.val)
			clause, a = c, []any{v}
		case "cc":
			c, v := like("m.cc_json", t.val)
			clause, a = c, []any{v}
		case "bcc":
			c, v := like("m.bcc_json", t.val)
			clause, a = c, []any{v}
		case "subject":
			c, v := like("m.subject", t.val)
			clause, a = c, []any{v}
		case "filename":
			clause, a = `LOWER(m.attach_json) LIKE ?`, []any{`%"filename":"%` + strings.ToLower(t.val) + `%`}
		case "label", "in", "folder":
			clause = `EXISTS(SELECT 1 FROM msg_folders mf JOIN folders f ON f.account_id=mf.account_id AND f.remote_id=mf.folder_remote
			  WHERE mf.msg_id=m.id AND (f.role=? OR LOWER(f.name)=? OR LOWER(f.remote_id)=?))`
			v := strings.ToLower(t.val)
			a = []any{v, v, v}
		case "list":
			c, v := like("m.extra", t.val)
			clause, a = c, []any{v}
		case "id":
			clause, a = `(m.message_id=? OR m.message_id=?)`, []any{t.val, "<" + strings.Trim(t.val, "<>") + ">"}
		case "is":
			switch strings.ToLower(t.val) {
			case "unread":
				clause = `m.is_read=0`
			case "read":
				clause = `m.is_read=1`
			case "starred":
				clause = `m.is_starred=1`
			case "snoozed":
				clause, a = `m.snoozed>?`, []any{time.Now().Unix()}
			case "draft":
				clause = `m.is_draft=1`
			default:
				return nil, nil, false
			}
		case "has":
			switch strings.ToLower(t.val) {
			case "attachment", "attachments":
				clause = `m.has_attach=1`
			case "invite", "calendar", "ics":
				clause = `m.extra LIKE '%"i":"%'`
			case "unsubscribe":
				clause = `m.extra LIKE '%"u":"%'`
			default:
				return nil, nil, false
			}
		case "before", "after":
			d, good := parseDay(t.val)
			if !good {
				return nil, nil, false
			}
			if t.op == "before" {
				clause, a = `m.date<?`, []any{d.Unix()}
			} else {
				clause, a = `m.date>=?`, []any{d.Unix()}
			}
		case "older_than", "newer_than":
			d, good := parseAge(t.val)
			if !good {
				return nil, nil, false
			}
			cut := time.Now().Add(-d).Unix()
			if t.op == "older_than" {
				clause, a = `m.date<?`, []any{cut}
			} else {
				clause, a = `m.date>=?`, []any{cut}
			}
		case "larger", "smaller":
			n, good := parseSize(t.val)
			if !good {
				return nil, nil, false
			}
			if t.op == "larger" {
				clause, a = `m.size>?`, []any{n}
			} else {
				clause, a = `m.size<?`, []any{n}
			}
		}
		if clause == "" {
			continue
		}
		if t.neg {
			clause = "NOT (" + clause + ")"
		}
		where = append(where, clause)
		args = append(args, a...)
	}
	if len(fts) > 0 {
		where = append(where, `m.rowid IN (SELECT rowid FROM search WHERE search MATCH ?)`)
		args = append(args, strings.Join(fts, " AND "))
	}
	for _, n := range negFts {
		where = append(where, `m.rowid NOT IN (SELECT rowid FROM search WHERE search MATCH ?)`)
		args = append(args, n)
	}
	return where, args, true
}

func ftsClean(w string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '"' || r == '*' || r == '(' || r == ')' || r == ':' || r == '^' {
			return -1
		}
		return r
	}, w))
}

func parseDay(v string) (time.Time, bool) {
	v = strings.ReplaceAll(v, "/", "-")
	for _, l := range []string{"2006-1-2", "2006-01-02"} {
		if t, err := time.ParseInLocation(l, v, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseAge reads 3d, 2w, 6m, 1y (hours with h).
func parseAge(v string) (time.Duration, bool) {
	if len(v) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch unicode.ToLower(rune(v[len(v)-1])) {
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, true
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, true
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, true
	}
	return 0, false
}

// parseSize reads 500, 10K, 5M, 1G.
func parseSize(v string) (int64, bool) {
	if v == "" {
		return 0, false
	}
	mult := int64(1)
	switch unicode.ToUpper(rune(v[len(v)-1])) {
	case 'K':
		mult, v = 1<<10, v[:len(v)-1]
	case 'M':
		mult, v = 1<<20, v[:len(v)-1]
	case 'G':
		mult, v = 1<<30, v[:len(v)-1]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * mult, true
}
