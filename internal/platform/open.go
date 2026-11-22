// Package platform holds the few things that differ by operating system.
package platform

import (
	"os"
	"os/exec"
	"runtime"
)

// OpenURL opens a link in the person's default browser.
func OpenURL(u string) error {
	if os.Getenv("HALYARD_NO_BROWSER") != "" {
		return nil // tests drive the sign-in page themselves
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		return exec.Command("open", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}
