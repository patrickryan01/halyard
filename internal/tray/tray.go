// Package tray puts the app in the notification area: an icon with a menu to open the window, write a message, check
// for mail and quit, and a count of unread mail in its tooltip.
package tray

import (
	_ "embed"
	"runtime"
	"sync"

	"fyne.io/systray"
)

//go:embed icon.png
var iconPNG []byte

//go:embed icon.ico
var iconICO []byte

// Actions are what the menu does.
type Actions struct {
	Open    func()
	Compose func()
	Check   func()
	Quit    func()
}

// Menu is the menu's content, kept apart from the system call so it can be checked without a desktop.
type Menu struct {
	Items []Item
}

type Item struct {
	Key      string
	Label    string
	Disabled bool
	Separate bool
}

// Build returns the menu for a given unread count.
func Build(name string, unread int) Menu {
	status := "No unread mail"
	switch {
	case unread == 1:
		status = "1 unread message"
	case unread > 1:
		status = itoa(unread) + " unread messages"
	}
	return Menu{Items: []Item{
		{Key: "status", Label: status, Disabled: true},
		{Key: "open", Label: "Open " + name},
		{Key: "compose", Label: "New message"},
		{Key: "check", Label: "Check mail now"},
		{Separate: true},
		{Key: "quit", Label: "Quit " + name},
	}}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Tooltip is the hover text.
func Tooltip(name string, unread int) string {
	if unread > 0 {
		return name + ": " + itoa(unread) + " unread"
	}
	return name
}

// Icon returns the picture the system wants: an .ico on Windows, a PNG elsewhere.
func Icon() []byte {
	if runtime.GOOS == "windows" {
		return iconICO
	}
	return iconPNG
}

// Tray is a running icon.
type Tray struct {
	name   string
	mu     sync.Mutex
	status *systray.MenuItem
	ready  chan struct{}
}

// Start shows the icon. It returns at once; the system loop runs in the background. SetUnread updates the count.
func Start(name string, a Actions) *Tray {
	t := &Tray{name: name, ready: make(chan struct{})}
	go systray.Run(func() {
		systray.SetIcon(Icon())
		systray.SetTitle(name)
		systray.SetTooltip(Tooltip(name, 0))
		m := Build(name, 0)
		items := map[string]*systray.MenuItem{}
		for _, it := range m.Items {
			if it.Separate {
				systray.AddSeparator()
				continue
			}
			mi := systray.AddMenuItem(it.Label, "")
			if it.Disabled {
				mi.Disable()
			}
			items[it.Key] = mi
		}
		t.mu.Lock()
		t.status = items["status"]
		t.mu.Unlock()
		close(t.ready)
		run := func(f func()) {
			if f != nil {
				f()
			}
		}
		go func() {
			for {
				select {
				case <-items["open"].ClickedCh:
					run(a.Open)
				case <-items["compose"].ClickedCh:
					run(a.Compose)
				case <-items["check"].ClickedCh:
					run(a.Check)
				case <-items["quit"].ClickedCh:
					run(a.Quit)
					return
				}
			}
		}()
		systray.SetOnTapped(func() { run(a.Open) })
	}, func() {})
	return t
}

// SetUnread updates the tooltip and the status line.
func (t *Tray) SetUnread(n int) {
	select {
	case <-t.ready:
	default:
		return
	}
	systray.SetTooltip(Tooltip(t.name, n))
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status != nil {
		t.status.SetTitle(Build(t.name, n).Items[0].Label)
	}
}

// Stop removes the icon.
func (t *Tray) Stop() { systray.Quit() }
