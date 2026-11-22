// Command halyard is the desktop app: the Go core and the web UI in a
// native window.
//
// Why Wails v2 rather than a local web server in a separate webview: the
// window, the menu and the single instance lock come from one library on
// Linux (WebKitGTK) and Windows (WebView2), the UI reaches the core through
// the asset handler in the same process with no network port to protect, and
// the same UI runs unchanged in a plain browser against cmd/halyardd for
// testing.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/patrickryan01/halyard/internal/app"
	"github.com/patrickryan01/halyard/internal/brand"
	"github.com/patrickryan01/halyard/internal/cli"
	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/platform"
	"github.com/patrickryan01/halyard/internal/tray"
)

func mailtoArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(a), "mailto:") {
			return a
		}
	}
	return ""
}

func main() {
	// "halyard send ..." and the other commands run in the terminal; with no command the window opens
	if len(os.Args) > 1 && cli.IsCommand(os.Args[1]) {
		os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
	}
	dir, err := app.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	// a windowed program has no console: keep a log next to the data, for support and for proof
	if lf, err := os.OpenFile(filepath.Join(dir, "halyard.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(lf)
	}
	a, err := app.Start(dir, "")
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	var ctx context.Context
	startMailto := mailtoArg(os.Args[1:])
	a.API.OnEvent(func(ev engine.Event) {
		if ev.Type != "new" || a.Store.Setting("notifications", "true") != "true" {
			return
		}
		if err := platform.Notify(ev.From, ev.Subject); err != nil {
			log.Printf("desktop notification failed: %v", err)
		} else {
			log.Printf("desktop notification raised: %s", ev.Subject)
		}
	})

	trayOn := a.Store.Setting("tray", "true") == "true" && os.Getenv("HALYARD_NO_TRAY") == "" && tray.Available()
	var tr *tray.Tray
	updateTray := func() {
		if tr == nil {
			return
		}
		n := 0
		fs, _ := a.Store.Folders("")
		for _, f := range fs {
			if f.Role == "inbox" {
				n += f.Unread
			}
		}
		tr.SetUnread(n)
	}
	a.API.OnEvent(func(ev engine.Event) {
		switch ev.Type {
		case "synced", "new", "accounts":
			updateTray()
		}
	})

	err = wails.Run(&options.App{
		Title:             brand.Name,
		HideWindowOnClose: trayOn,
		StartHidden:       trayOn && a.Store.Setting("startMinimized", "false") == "true",
		Width:             1280,
		Height:            820,
		MinWidth:          900,
		MinHeight:         560,
		AssetServer:       &assetserver.Options{Handler: a.API.Handler()},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: brand.ID,
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				if ctx == nil {
					return
				}
				runtime.WindowUnminimise(ctx)
				runtime.Show(ctx)
				if m := mailtoArg(d.Args); m != "" {
					runtime.EventsEmit(ctx, "mailto", m)
				}
			},
		},
		OnStartup: func(c context.Context) {
			ctx = c
			if trayOn {
				show := func() { runtime.WindowUnminimise(c); runtime.Show(c) }
				tr = tray.Start(brand.Name, tray.Actions{
					Open:    show,
					Compose: func() { show(); runtime.EventsEmit(c, "mailto", "mailto:") },
					Check:   func() { a.Engine.SyncNow("") },
					Quit:    func() { runtime.Quit(c) },
				})
				updateTray()
			}
			if startMailto != "" {
				// the page is not loaded yet; it asks for this when it starts
				runtime.EventsOn(c, "ready", func(...interface{}) { runtime.EventsEmit(c, "mailto", startMailto) })
			}
		},
		Linux:   &linux.Options{ProgramName: brand.Slug, WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand},
		Windows: &windows.Options{WebviewIsTransparent: false, DisableWindowIcon: false},
	})
	if err != nil {
		log.Fatal(err)
	}
}
