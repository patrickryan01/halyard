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
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/beeep"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/patrickryan01/halyard/internal/app"
	"github.com/patrickryan01/halyard/internal/brand"
	"github.com/patrickryan01/halyard/internal/engine"
)

//go:embed icon.png
var appIcon []byte

func mailtoArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(a), "mailto:") {
			return a
		}
	}
	return ""
}

func main() {
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
	beeep.AppName = brand.Name

	var ctx context.Context
	startMailto := mailtoArg(os.Args[1:])
	a.API.OnEvent(func(ev engine.Event) {
		if ev.Type != "new" || a.Store.Setting("notifications", "true") != "true" {
			return
		}
		if err := beeep.Notify(ev.From, ev.Subject, appIcon); err != nil {
			log.Printf("desktop notification failed: %v", err)
		} else {
			log.Printf("desktop notification raised: %s", ev.Subject)
		}
	})

	err = wails.Run(&options.App{
		Title:       brand.Name,
		Width:       1280,
		Height:      820,
		MinWidth:    900,
		MinHeight:   560,
		AssetServer: &assetserver.Options{Handler: a.API.Handler()},
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
