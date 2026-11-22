package platform

// FixGDKBackend keeps the window from dying when the program is started without a login session's variables (from a
// service unit, a script or runuser). The toolkit the window library uses forces the X11 backend when GDK_BACKEND is
// unset and XDG_SESSION_TYPE is empty, which fails on a machine with only Wayland. When Wayland is there and nothing
// chose a backend, ask for Wayland first and X11 second. Call it before the window starts.
func FixGDKBackend(getenv func(string) string, setenv func(k, v string)) {
	if getenv("GDK_BACKEND") == "" && getenv("WAYLAND_DISPLAY") != "" {
		setenv("GDK_BACKEND", "wayland,x11")
	}
}
