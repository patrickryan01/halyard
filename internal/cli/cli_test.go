package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mbox = "From a@x.test Sat Nov 21 10:00:00 2026\r\nFrom: Ana <ana@x.test>\r\nTo: me@x.test\r\nSubject: Quarterly plan\r\nDate: Sat, 21 Nov 2026 10:00:00 +0000\r\nMessage-ID: <1@x.test>\r\n\r\nthe zebra budget\r\n\r\nFrom b@y.test Sat Nov 21 11:00:00 2026\r\nFrom: Bo <bo@y.test>\r\nSubject: Lunch\r\nDate: Sat, 21 Nov 2026 11:00:00 +0000\r\nMessage-ID: <2@y.test>\r\nList-Unsubscribe: <mailto:u@y.test>\r\n\r\ntacos\r\n"

func run(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := Main(args, &out, &errb); code != 0 {
		t.Fatalf("%v: exit %d: %s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

func TestCommandLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HALYARD_DATA", dir)
	t.Setenv("HALYARD_NO_KEYRING", "1")
	t.Setenv("HALYARD_CLI_STANDALONE", "1")
	mb := filepath.Join(dir, "old.mbox")
	if err := os.WriteFile(mb, []byte(mbox), 0o600); err != nil {
		t.Fatal(err)
	}
	if h := run(t, "help"); !strings.Contains(h, "send") || !strings.Contains(h, "search") || !strings.Contains(h, "list") {
		t.Fatal("help does not list the main commands")
	}
	for _, c := range Commands {
		if !IsCommand(c.Name) {
			t.Fatalf("%s is not a command", c.Name)
		}
	}
	if out := run(t, "import", "mbox", mb, "--folder", "Old"); !strings.Contains(out, "Imported") && !strings.Contains(out, "imported") {
		t.Fatalf("import: %s", out)
	}
	if out := run(t, "search", "zebra"); !strings.Contains(out, "Quarterly plan") || strings.Contains(out, "Lunch") {
		t.Fatalf("search: %s", out)
	}
	if out := run(t, "search", "has:unsubscribe"); !strings.Contains(out, "Lunch") || strings.Contains(out, "Quarterly") {
		t.Fatalf("operators: %s", out)
	}
	if out := run(t, "search", "from:ana", "--json"); !strings.Contains(out, `"subject": "Quarterly plan"`) {
		t.Fatalf("json: %s", out)
	}
	out := run(t, "search", "zebra")
	id := strings.Fields(strings.TrimLeft(out, "* "))[0]
	if r := run(t, "read", id); !strings.Contains(r, "the zebra budget") || !strings.Contains(r, "ana@x.test") {
		t.Fatalf("read: %s", r)
	}
	eml := filepath.Join(dir, "m.eml")
	rj := run(t, "read", id, "--json")
	mid := between(rj, `"id": "`, `"`)
	if o := run(t, "export", "eml", mid, eml); !strings.Contains(o, "saved") {
		t.Fatal(o)
	}
	if b, _ := os.ReadFile(eml); !strings.Contains(string(b), "Subject: Quarterly plan") {
		t.Fatalf("eml: %s", b)
	}
	run(t, "templates", "add", "--name", "thanks", "--body", "Thank you.")
	if o := run(t, "templates"); !strings.Contains(o, "thanks") {
		t.Fatal(o)
	}
	run(t, "searches", "add", "--name", "zeb", "--query", "zebra")
	if o := run(t, "searches"); !strings.Contains(o, "zeb") {
		t.Fatal(o)
	}
	run(t, "rules", "add", "--name", "tacos", "--if", "subject:contains:lunch", "--then", "star")
	if o := run(t, "rules"); !strings.Contains(o, "tacos") {
		t.Fatal(o)
	}
	run(t, "settings", "theme", "dark")
	if o := run(t, "settings"); !strings.Contains(o, "theme = dark") {
		t.Fatal(o)
	}
	run(t, "keys", "make", "--kind", "pgp", "--email", "me@x.test", "--name", "Me")
	if o := run(t, "keys"); !strings.Contains(o, "me@x.test") {
		t.Fatal(o)
	}
	if o := run(t, "accounts"); !strings.Contains(o, "local") {
		t.Fatal(o)
	}
	if v := run(t, "version"); !strings.Contains(v, "Halyard") {
		t.Fatal(v)
	}
	var out2, err2 bytes.Buffer
	if Main([]string{"nonsense"}, &out2, &err2) == 0 {
		t.Fatal("an unknown command succeeded")
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	return s[:strings.Index(s, b)]
}
