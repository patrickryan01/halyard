//go:build linux

package tray

import "github.com/godbus/dbus/v5"

// Available says whether the desktop has somewhere to show the icon. On Linux the icon needs a StatusNotifier host
// (KDE, XFCE, a tray extension on GNOME); without one the window must never hide itself, or it could not be brought back.
func Available() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var has bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has); err != nil {
		return false
	}
	return has
}
