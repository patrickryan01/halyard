package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.0.0", "1.0.1", true}, {"1.0.9", "1.1.0", true}, {"1.2.0", "1.10.0", true}, {"1.0.0", "1.0.0", false}, {"2.0.0", "1.9.9", false}, {"dev", "1.0.0", true}} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("%s -> %s: want %v", c.a, c.b, c.want)
		}
	}
}

func feed(t *testing.T, installer []byte, hash string) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.0","body":"notes","assets":[{"name":"halyard-1.2.0-setup.exe","browser_download_url":"%s/exe"},{"name":"halyard-1.2.0-setup.exe.sha256","browser_download_url":"%s/sum"}]}`, srv.URL, srv.URL)
		case "/exe":
			w.Write(installer)
		case "/sum":
			fmt.Fprintf(w, "%s  halyard-1.2.0-setup.exe\n", hash)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The Windows build finds a newer signed release, and the download is kept only if its hash matches the release's.
func TestUpdateCheckVerifiesHash(t *testing.T) {
	exe := []byte("MZ pretend installer")
	sum := sha256.Sum256(exe)
	good := feed(t, exe, hex.EncodeToString(sum[:]))
	old := API
	defer func() { API = old }()
	API = good.URL + "/latest"
	in, err := Check(context.Background(), "1.0.0", "windows")
	if err != nil || !in.Newer || in.Latest != "1.2.0" || in.SHA256 == "" {
		t.Fatalf("%+v %v", in, err)
	}
	dir := t.TempDir()
	p, err := Download(context.Background(), in, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(exe) {
		t.Fatal("wrong file")
	}
	if cur, _ := Check(context.Background(), "1.2.0", "windows"); cur.Newer {
		t.Fatal("the same version was offered")
	}
	// a download that does not match is refused and nothing is left behind
	bad := feed(t, []byte("tampered"), hex.EncodeToString(sum[:]))
	API = bad.URL + "/latest"
	in, _ = Check(context.Background(), "1.0.0", "windows")
	dir2 := t.TempDir()
	if _, err := Download(context.Background(), in, dir2); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("a tampered installer was accepted: %v", err)
	}
	if es, _ := os.ReadDir(dir2); len(es) != 0 {
		t.Fatal("a rejected download was kept")
	}
	// a release with no hash is not offered
	nohash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.3.0","assets":[{"name":"halyard-1.3.0-setup.exe","browser_download_url":"http://x/exe"}]}`)
	}))
	defer nohash.Close()
	API = nohash.URL
	if in, err := Check(context.Background(), "1.0.0", "windows"); err == nil || in.Newer {
		t.Fatalf("a release without a hash was offered: %+v %v", in, err)
	}
}

// Arch and EmberRTOS Desktop do not update themselves: they say the package manager does, and make no network call.
func TestUpdateOnPackageManagerBuilds(t *testing.T) {
	old := API
	defer func() { API = old }()
	API = "http://127.0.0.1:1/never"
	for _, goos := range []string{"linux", "darwin"} {
		in, err := Check(context.Background(), "1.0.0", goos)
		if err != nil || !in.Managed || in.Newer || !strings.Contains(in.Statement, "package manager") || !strings.Contains(in.Statement, "pacman") || !strings.Contains(in.Statement, "zypper") {
			t.Fatalf("%s: %+v %v", goos, in, err)
		}
	}
}
