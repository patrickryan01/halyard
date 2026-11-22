// Command mailcored runs the mail client without a window: the same core and
// the same web UI on a loopback port, behind a one time token. It is how the
// client is tested end to end, and how it runs on a machine with no desktop.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/patrickryan01/halyard/internal/app"
	"github.com/patrickryan01/halyard/internal/brand"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "loopback address to serve on")
	dataDir := flag.String("data", "", "data directory (default: the user config directory)")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println(brand.Version)
		return
	}
	dir := *dataDir
	if dir == "" {
		var err error
		if dir, err = app.DataDir(); err != nil {
			log.Fatal(err)
		}
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		log.Fatal("the daemon only serves on loopback")
	}
	token := app.NewToken()
	a, err := app.Start(dir, token)
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("open http://%s/?token=%s\n", ln.Addr(), token)
	srv := &http.Server{Handler: a.API.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
