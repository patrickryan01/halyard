package platform

import "testing"

func run(env map[string]string) map[string]string {
	FixGDKBackend(func(k string) string { return env[k] }, func(k, v string) { env[k] = v })
	return env
}

func TestFixGDKBackend(t *testing.T) {
	// a launch from a unit: Wayland is there, the session type is empty
	if e := run(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "XDG_SESSION_TYPE": ""}); e["GDK_BACKEND"] != "wayland,x11" {
		t.Fatalf("%v", e)
	}
	// somebody chose a backend: it stays
	if e := run(map[string]string{"WAYLAND_DISPLAY": "wayland-1", "GDK_BACKEND": "x11"}); e["GDK_BACKEND"] != "x11" {
		t.Fatalf("%v", e)
	}
	// no Wayland: nothing is set
	if e := run(map[string]string{"DISPLAY": ":0"}); e["GDK_BACKEND"] != "" {
		t.Fatalf("%v", e)
	}
}
