//go:build !linux

package tray

// Available says whether the desktop has somewhere to show the icon. Windows always has the notification area.
func Available() bool { return true }
