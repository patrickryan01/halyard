package packaging

import (
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The program is offered as the handler for mailto: links on every system it is packaged for.
func TestPackagesRegisterTheMailtoHandler(t *testing.T) {
	if d := read(t, "common/halyard.desktop"); !strings.Contains(d, "x-scheme-handler/mailto") || !strings.Contains(d, "Exec=halyard %u") {
		t.Fatal("the Linux desktop entry does not handle mailto links")
	}
	n := read(t, "windows/halyard.nsi")
	for _, want := range []string{`URLAssociations" "mailto"`, `Halyard.Url.mailto\shell\open\command`} {
		if !strings.Contains(n, want) {
			t.Fatalf("the Windows installer does not register %q", want)
		}
	}
}

// The command line tool is in every package.
func TestPackagesShipTheCommandLine(t *testing.T) {
	for _, p := range []string{"arch/PKGBUILD", "rpm/halyard.spec", "windows/halyard.nsi", "build.sh"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if !strings.Contains(read(t, p), "halyardctl") {
			t.Errorf("%s does not ship halyardctl", p)
		}
	}
}
