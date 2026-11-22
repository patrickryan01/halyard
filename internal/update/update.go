// Package update checks the signed release on GitHub for a newer version, and on Windows downloads the installer and
// checks its hash before it is offered. On Arch and EmberRTOS Desktop the package manager owns updates, and says so.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// API is the release feed; tests point it at their own server.
var API = "https://api.github.com/repos/patrickryan01/halyard/releases/latest"

var HTTP = &http.Client{Timeout: 60 * time.Second}

// Info is what the check found.
type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Newer     bool   `json:"newer"`
	URL       string `json:"url,omitempty"`    // the installer, Windows only
	SHA256    string `json:"sha256,omitempty"` // from the release's own .sha256 file
	Notes     string `json:"notes,omitempty"`
	Managed   bool   `json:"managed"` // true when the package manager does updates
	Statement string `json:"statement,omitempty"`
}

// ManagerStatement is what the Arch and RPM builds say.
const ManagerStatement = "Updates for this build come from your package manager (pacman -Syu on EmberArch, zypper dup on EmberRTOS Desktop). Halyard does not replace itself."

var verRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Newer says whether b is a later version than a.
func Newer(a, b string) bool {
	x, y := verRe.FindStringSubmatch(a), verRe.FindStringSubmatch(b)
	if y == nil {
		return false
	}
	if x == nil {
		return true
	}
	for i := 1; i <= 3; i++ {
		p, _ := strconv.Atoi(x[i])
		q, _ := strconv.Atoi(y[i])
		if q != p {
			return q > p
		}
	}
	return false
}

type release struct {
	Tag    string `json:"tag_name"`
	Body   string `json:"body"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the release server answered %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// Check looks for a newer release. goos is runtime.GOOS in the program and a value in tests. Anything but Windows is
// told its updates come from the package manager, without a network call.
func Check(ctx context.Context, current, goos string) (Info, error) {
	in := Info{Current: current}
	if goos != "windows" {
		in.Managed, in.Statement = true, ManagerStatement
		return in, nil
	}
	b, err := get(ctx, API)
	if err != nil {
		return in, err
	}
	var r release
	if err := json.Unmarshal(b, &r); err != nil {
		return in, err
	}
	in.Latest, in.Notes = strings.TrimPrefix(r.Tag, "v"), r.Body
	in.Newer = Newer(current, in.Latest)
	if !in.Newer {
		return in, nil
	}
	var setup, sum string
	for _, a := range r.Assets {
		switch {
		case strings.HasSuffix(a.Name, "-setup.exe"):
			setup = a.URL
		case strings.HasSuffix(a.Name, "-setup.exe.sha256"):
			sum = a.URL
		}
	}
	if setup == "" || sum == "" {
		in.Newer = false // a release without its installer and hash is not offered
		return in, errors.New("the newest release has no installer with a hash yet")
	}
	hb, err := get(ctx, sum)
	if err != nil {
		in.Newer = false
		return in, fmt.Errorf("could not read the release hash: %w", err)
	}
	f := strings.Fields(string(hb))
	if len(f) == 0 || len(f[0]) != 64 {
		in.Newer = false
		return in, errors.New("the release hash is not valid")
	}
	in.URL, in.SHA256 = setup, strings.ToLower(f[0])
	return in, nil
}

// Download fetches the installer into dir and checks its SHA-256 against the release's hash. A file that does not match
// is deleted and never returned.
func Download(ctx context.Context, in Info, dir string) (string, error) {
	if in.URL == "" || in.SHA256 == "" {
		return "", errors.New("there is nothing to download")
	}
	b, err := get(ctx, in.URL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != in.SHA256 {
		return "", errors.New("the download does not match the release hash and was discarded")
	}
	path := filepath.Join(dir, "halyard-"+in.Latest+"-setup.exe")
	if err := os.WriteFile(path, b, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

// Here is Check for the running program.
func Here(ctx context.Context, current string) (Info, error) {
	return Check(ctx, current, runtime.GOOS)
}
