package sieve

import (
	"strings"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func TestVacationRoundTrip(t *testing.T) {
	existing := "require [\"fileinto\"];\n# my rules\nif header :contains \"subject\" \"bill\" { fileinto \"Bills\"; }\n"
	v := mailtypes.Vacation{Enabled: true, Subject: `Away, "back" soon`, Body: "I am out.\n.Back on Monday.\nThanks", Start: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 12, 8, 0, 0, 0, 0, time.UTC)}
	s := WithVacation(existing, v)
	if !strings.Contains(s, "fileinto \"Bills\"") || !strings.Contains(s, `"vacation"`) {
		t.Fatalf("lost the user's rules or the require: %s", s)
	}
	// every require comes before any other command
	first := strings.Index(s, "if ")
	for _, r := range []string{`require ["vacation","date","relational"]`, `require ["fileinto"]`} {
		if i := strings.Index(s, r); i < 0 || i > first {
			t.Fatalf("require %q misplaced in:\n%s", r, s)
		}
	}
	got := ParseVacation(s)
	if !got.Enabled || got.Subject != v.Subject || got.Body != v.Body || !got.Start.Equal(v.Start) || !got.End.Equal(v.End) {
		t.Fatalf("round trip: %+v", got)
	}
	// replacing keeps exactly one block
	v.Body = "new text"
	s2 := WithVacation(s, v)
	if strings.Count(s2, vacBegin) != 1 || strings.Contains(s2, "Back on Monday") || !strings.Contains(s2, "fileinto") {
		t.Fatalf("replace: %s", s2)
	}
	// turning it off leaves the user's script as it was
	off := WithVacation(s2, mailtypes.Vacation{})
	if strings.TrimSpace(off) != strings.TrimSpace(existing) {
		t.Fatalf("off: %q want %q", off, existing)
	}
	if ParseVacation(off).Enabled {
		t.Fatal("still enabled")
	}
}
