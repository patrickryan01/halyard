package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMboxSplitsAndUnquotes(t *testing.T) {
	in := "From a@x.test Sat Nov 21 10:00:00 2026\nSubject: one\nFrom: a@x.test\n\nbody one\n>From the start of a line\n\nFrom b@x.test Sat Nov 21 11:00:00 2026\nSubject: two\n\nbody two\n"
	var got []string
	if err := Mbox(strings.NewReader(in), func(raw []byte) error { got = append(got, string(raw)); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.Contains(got[0], "Subject: one") || !strings.Contains(got[0], "\r\nFrom the start of a line") || !strings.Contains(got[1], "Subject: two") {
		t.Fatalf("%q", got)
	}
	if strings.Contains(got[0], "Subject: two") {
		t.Fatal("messages ran together")
	}
}

func TestThunderbirdProfile(t *testing.T) {
	d := t.TempDir()
	w := func(p, c string) {
		full := filepath.Join(d, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	w("Mail/Local Folders/Inbox", "From x\nSubject: a\n\nb\n")
	w("Mail/Local Folders/Inbox.msf", "index")
	w("Mail/Local Folders/Projects.sbd/2024", "From x\nSubject: c\n\nd\n")
	w("ImapMail/imap.example.com/INBOX", "From x\nSubject: e\n\nf\n")
	fs, err := Thunderbird(d)
	if err != nil || len(fs) != 3 {
		t.Fatalf("%v %v", fs, err)
	}
	names := fs[0].Name + "," + fs[1].Name + "," + fs[2].Name
	if names != "Inbox,Projects/2024,imap.example.com/INBOX" {
		t.Fatalf("names: %s", names)
	}
}
