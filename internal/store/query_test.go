package store

import (
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func TestSearchOperators(t *testing.T) {
	s := open(t)
	old := time.Now().Add(-40 * 24 * time.Hour)
	m1 := msg("1", "t1", "Invoice for November", "please pay the invoice of 4200", time.Hour, "INBOX")
	m1.Attach = []mailtypes.Attachment{{ID: "x", Filename: "Report-Q4.pdf", Size: 3 << 20}}
	m1.To = []mailtypes.Address{{Email: "ops@x.test"}}
	m2 := msg("2", "t2", "Lunch plans", "tacos on friday, cats or dogs", time.Hour, "INBOX")
	m2.Read = true
	m3 := msg("3", "t3", "Archived thing", "old news about tacos", 0, "Archive")
	m3.Date = old
	m3.Starred = true
	m3.From = mailtypes.Address{Name: "Ana Ruiz", Email: "ana@y.test"}
	m4 := msg("4", "t4", "Newsletter", "buy things", time.Hour, "INBOX")
	m4.ListUnsubscribe = "<mailto:u@x.test>"
	m4.ICS = "BEGIN:VCALENDAR\nEND:VCALENDAR"
	if err := s.UpsertMessages([]mailtypes.Message{m1, m2, m3, m4}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		q    string
		want []string
	}{
		{"from:ana", []string{"a1/t3"}},
		{"from:\"Ana Ruiz\"", []string{"a1/t3"}},
		{"to:ops@x.test", []string{"a1/t1"}},
		{"subject:lunch", []string{"a1/t2"}},
		{"is:unread tacos", []string{"a1/t3"}},
		{"is:read", []string{"a1/t2"}},
		{"is:starred", []string{"a1/t3"}},
		{"has:attachment", []string{"a1/t1"}},
		{"filename:report", []string{"a1/t1"}},
		{"has:unsubscribe", []string{"a1/t4"}},
		{"has:invite", []string{"a1/t4"}},
		{"in:archive", []string{"a1/t3"}},
		{"older_than:30d", []string{"a1/t3"}},
		{"newer_than:30d tacos", []string{"a1/t2"}},
		{"larger:1M", []string{"a1/t1"}},
		{"smaller:1K invoice", nil},
		{"\"pay the invoice\"", []string{"a1/t1"}},
		{"tacos -friday", []string{"a1/t3"}},
		{"-from:pat tacos", []string{"a1/t3"}},
		{"cats OR invoice", []string{"a1/t1", "a1/t2"}},
		{"before:" + time.Now().Add(-24*time.Hour).Format("2006-01-02") + " tacos", []string{"a1/t3"}},
		{"after:" + time.Now().Add(-24*time.Hour).Format("2006/01/02") + " tacos", []string{"a1/t2"}},
		{"is:nonsense", nil},
		{"larger:abc", nil},
	}
	for _, c := range cases {
		got, err := s.ListThreads(ListQuery{Text: c.q})
		if err != nil {
			t.Fatalf("%q: %v", c.q, err)
		}
		set := map[string]bool{}
		for _, g := range got {
			set[g.ID] = true
		}
		if len(set) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.q, set, c.want)
			continue
		}
		for _, w := range c.want {
			if !set[w] {
				t.Errorf("%q: missing %s in %v", c.q, w, set)
			}
		}
	}
}
