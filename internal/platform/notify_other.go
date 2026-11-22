//go:build !windows

package platform

import "github.com/gen2brain/beeep"

// Notify raises a desktop notification through the system's notification service.
func Notify(title, body string) error { return beeep.Notify(title, body, "") }
