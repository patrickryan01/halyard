package ical

import (
	"strings"
	"testing"
	"time"
)

const invite = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\nBEGIN:VTIMEZONE\r\nTZID:America/New_York\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:abc-123\r\nDTSTART;TZID=America/New_York:20261203T140000\r\nDTEND;TZID=America/New_York:20261203T150000\r\nSUMMARY:Plant review\\, week 49\r\nLOCATION:Room 4\r\nDESCRIPTION:Bring the\r\n  numbers\\nand coffee\r\nORGANIZER;CN=\"Ana Ruiz\":mailto:ana@y.test\r\nATTENDEE;CN=Me;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:me@x.test\r\nRRULE:FREQ=WEEKLY;COUNT=4\r\nBEGIN:VALARM\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestParseInvite(t *testing.T) {
	evs, err := Parse(invite)
	if err != nil || len(evs) != 1 {
		t.Fatal(evs, err)
	}
	e := evs[0]
	if e.UID != "abc-123" || e.Method != "REQUEST" || e.Summary != "Plant review, week 49" || e.Location != "Room 4" {
		t.Fatalf("%+v", e)
	}
	if e.Organizer.Email != "ana@y.test" || e.Organizer.Name != "Ana Ruiz" || len(e.Attendees) != 1 || !e.Attendees[0].RSVP {
		t.Fatalf("people %+v", e)
	}
	if e.Start.UTC().Format(time.RFC3339) != "2026-12-03T19:00:00Z" || e.End.Sub(e.Start) != time.Hour {
		t.Fatalf("times %v %v", e.Start, e.End)
	}
	if !strings.Contains(e.Description, "numbers\nand coffee") || e.Recurrence != "FREQ=WEEKLY;COUNT=4" {
		t.Fatalf("description or rule: %q %q", e.Description, e.Recurrence)
	}
}

func TestAllDayAndDuration(t *testing.T) {
	evs, err := Parse("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:1\nDTSTART;VALUE=DATE:20261224\nSUMMARY:Off\nEND:VEVENT\nBEGIN:VEVENT\nUID:2\nDTSTART:20261224T100000Z\nDURATION:PT1H30M\nEND:VEVENT\nEND:VCALENDAR")
	if err != nil || len(evs) != 2 || !evs[0].AllDay || evs[0].End.Sub(evs[0].Start) != 24*time.Hour || evs[1].End.Sub(evs[1].Start) != 90*time.Minute {
		t.Fatal(evs, err)
	}
}

func TestReplyAndRoundTrip(t *testing.T) {
	evs, _ := Parse(invite)
	txt, err := Reply(evs[0], Attendee{Name: "Me", Email: "me@x.test"}, "ACCEPTED")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "METHOD:REPLY") || !strings.Contains(txt, "PARTSTAT=ACCEPTED") || !strings.Contains(txt, "UID:abc-123") {
		t.Fatal(txt)
	}
	back, err := Parse(txt)
	if err != nil || back[0].Method != "REPLY" || back[0].Attendees[0].PartStat != "ACCEPTED" || !back[0].Start.Equal(evs[0].Start) {
		t.Fatalf("%+v %v", back, err)
	}
	if _, err := Reply(evs[0], Attendee{Email: "me@x.test"}, "MAYBE"); err == nil {
		t.Fatal("a bad answer was accepted")
	}
	for _, l := range strings.Split(txt, "\r\n") {
		if len(l) > 75 {
			t.Fatalf("line not folded: %d", len(l))
		}
	}
}
