// Package rules decides whether a filter rule matches a message. What a matching rule does is the engine's job.
package rules

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// Fields a condition can test.
var Fields = []string{"from", "to", "cc", "subject", "body", "has_attachment", "has_unsubscribe", "size_over", "size_under"}

// Ops a condition can use on text.
var Ops = []string{"contains", "not_contains", "equals", "starts", "ends", "matches"}

func addrText(as []mailtypes.Address) string {
	var b strings.Builder
	for _, a := range as {
		b.WriteString(a.Name + " <" + a.Email + "> ")
	}
	return b.String()
}

func fieldText(f string, m mailtypes.Message) (string, bool) {
	switch f {
	case "from":
		return m.From.Name + " <" + m.From.Email + ">", true
	case "to":
		return addrText(m.To), true
	case "cc":
		return addrText(m.Cc), true
	case "subject":
		return m.Subject, true
	case "body":
		if m.BodyText != "" {
			return m.BodyText, true
		}
		return m.Snippet, true
	}
	return "", false
}

// Condition tests one condition.
func Condition(c mailtypes.RuleCondition, m mailtypes.Message) bool {
	switch c.Field {
	case "has_attachment":
		has := false
		for _, a := range m.Attach {
			if !a.Inline {
				has = true
			}
		}
		return has == (c.Value == "" || strings.EqualFold(c.Value, "true") || c.Value == "1")
	case "has_unsubscribe":
		return (m.ListUnsubscribe != "") == (c.Value == "" || strings.EqualFold(c.Value, "true") || c.Value == "1")
	case "size_over", "size_under":
		n, err := strconv.ParseInt(c.Value, 10, 64)
		if err != nil {
			return false
		}
		size := m.Size
		if size == 0 {
			size = int64(len(m.BodyText) + len(m.BodyHTML))
		}
		if c.Field == "size_over" {
			return size > n
		}
		return size < n
	}
	text, ok := fieldText(c.Field, m)
	if !ok {
		return false
	}
	t, v := strings.ToLower(text), strings.ToLower(c.Value)
	switch c.Op {
	case "contains", "":
		return strings.Contains(t, v)
	case "not_contains":
		return !strings.Contains(t, v)
	case "equals":
		return strings.TrimSpace(t) == v || (c.Field == "from" && strings.EqualFold(m.From.Email, c.Value))
	case "starts":
		return strings.HasPrefix(t, v)
	case "ends":
		return strings.HasSuffix(t, v)
	case "matches":
		re, err := regexp.Compile("(?i)" + c.Value)
		return err == nil && re.MatchString(text)
	}
	return false
}

// Match says whether the rule's conditions hold for the message. A rule with no conditions matches nothing, so an
// unfinished rule never moves mail.
func Match(r mailtypes.Rule, m mailtypes.Message) bool {
	if !r.Enabled || len(r.Conditions) == 0 {
		return false
	}
	if r.AccountID != "" && r.AccountID != m.AccountID {
		return false
	}
	any := r.Match == "any"
	for _, c := range r.Conditions {
		ok := Condition(c, m)
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	return !any
}

// Validate says what is wrong with a rule, or nothing.
func Validate(r mailtypes.Rule) string {
	if strings.TrimSpace(r.Name) == "" {
		return "a rule needs a name"
	}
	if len(r.Conditions) == 0 {
		return "a rule needs at least one condition"
	}
	if len(r.Actions) == 0 {
		return "a rule needs at least one action"
	}
	for _, c := range r.Conditions {
		known := false
		for _, f := range Fields {
			if f == c.Field {
				known = true
			}
		}
		if !known {
			return "unknown condition: " + c.Field
		}
		if c.Op == "matches" {
			if _, err := regexp.Compile(c.Value); err != nil {
				return "that pattern is not valid: " + err.Error()
			}
		}
	}
	for _, a := range r.Actions {
		switch a.Kind {
		case "move", "label":
			if a.FolderID == "" {
				return "choose a folder for " + a.Kind
			}
		case "forward":
			if a.To == "" {
				return "choose who to forward to"
			}
		case "archive", "read", "star", "trash", "spam":
		default:
			return "unknown action: " + a.Kind
		}
	}
	return ""
}
