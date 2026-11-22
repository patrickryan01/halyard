package rules

import (
	"testing"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func TestMatch(t *testing.T) {
	m := mailtypes.Message{AccountID: "a1", Subject: "Invoice 42", From: mailtypes.Address{Name: "Ana", Email: "ana@y.test"}, BodyText: "pay now",
		Attach: []mailtypes.Attachment{{Filename: "x.pdf"}}, ListUnsubscribe: "<mailto:u@y.test>", Size: 5000}
	r := func(match string, cs ...mailtypes.RuleCondition) mailtypes.Rule {
		return mailtypes.Rule{Enabled: true, Match: match, Conditions: cs}
	}
	cc := func(f, op, v string) mailtypes.RuleCondition { return mailtypes.RuleCondition{Field: f, Op: op, Value: v} }
	cases := []struct {
		name string
		r    mailtypes.Rule
		want bool
	}{
		{"contains", r("all", cc("subject", "contains", "invoice")), true},
		{"not contains", r("all", cc("subject", "not_contains", "invoice")), false},
		{"from equals email", r("all", cc("from", "equals", "ANA@y.test")), true},
		{"starts", r("all", cc("subject", "starts", "inv")), true},
		{"ends", r("all", cc("subject", "ends", "42")), true},
		{"regex", r("all", cc("subject", "matches", `invoice \d+`)), true},
		{"all fails on one", r("all", cc("subject", "contains", "invoice"), cc("body", "contains", "nothing")), false},
		{"any passes on one", r("any", cc("subject", "contains", "zzz"), cc("body", "contains", "pay")), true},
		{"attachment", r("all", cc("has_attachment", "", "true")), true},
		{"no attachment wanted", r("all", cc("has_attachment", "", "false")), false},
		{"unsubscribe", r("all", cc("has_unsubscribe", "", "true")), true},
		{"size over", r("all", cc("size_over", "", "4000")), true},
		{"size under", r("all", cc("size_under", "", "4000")), false},
		{"disabled", mailtypes.Rule{Match: "all", Conditions: []mailtypes.RuleCondition{cc("subject", "contains", "invoice")}}, false},
		{"other account", mailtypes.Rule{Enabled: true, AccountID: "a2", Conditions: []mailtypes.RuleCondition{cc("subject", "contains", "invoice")}}, false},
		{"no conditions", mailtypes.Rule{Enabled: true}, false},
	}
	for _, c := range cases {
		if got := Match(c.r, m); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if Validate(mailtypes.Rule{Name: "x", Conditions: []mailtypes.RuleCondition{cc("subject", "matches", "(")}, Actions: []mailtypes.RuleAction{{Kind: "read"}}}) == "" {
		t.Error("a bad pattern passed validation")
	}
	if Validate(mailtypes.Rule{Name: "x", Conditions: []mailtypes.RuleCondition{cc("subject", "contains", "a")}, Actions: []mailtypes.RuleAction{{Kind: "move"}}}) == "" {
		t.Error("a move with no folder passed validation")
	}
}
