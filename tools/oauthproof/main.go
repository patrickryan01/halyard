// Command oauthproof runs the app's real browser sign-in (the same oauth.Start and oauth.Source the app uses) for one
// provider and then proves the token by calling the provider. It prints the sign-in URL, waits for the browser to
// come back to the loopback redirect, and prints the signed-in address. It is a proof tool, not part of the app.
//
//	oauthproof google|microsoft
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/providers/google"
	"github.com/patrickryan01/halyard/internal/providers/microsoft"
)

type mem struct{ b []byte }

func (m *mem) Get() ([]byte, error) { return m.b, nil }
func (m *mem) Put(b []byte) error   { m.b = b; return nil }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: oauthproof google|microsoft")
		os.Exit(2)
	}
	kind := os.Args[1]
	var cfg oauth.Config
	switch kind {
	case "google":
		cfg = google.OAuthConfig(nil)
	case "microsoft":
		cfg = microsoft.OAuthConfig(nil)
	default:
		fmt.Println("unknown provider")
		os.Exit(2)
	}
	if cfg.ClientID == "" {
		fmt.Println("no client id: build with the -X flags from packaging/common/build-flags.sh or set the account settings")
		os.Exit(1)
	}
	p, err := oauth.Start(cfg)
	if err != nil {
		fmt.Println("start:", err)
		os.Exit(1)
	}
	fmt.Println("URL", p.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tok, err := p.Wait(ctx, cfg)
	if err != nil {
		fmt.Println("FAIL sign-in:", err)
		os.Exit(1)
	}
	b, _ := json.Marshal(tok)
	sec := &mem{b: b}
	src := &oauth.Source{Cfg: cfg, Sec: sec}
	acct := mailtypes.Account{ID: "proof", Kind: kind, Settings: map[string]string{}}
	var email string
	switch kind {
	case "google":
		pr, err := google.NewWithSource(acct, src).Profile(ctx)
		if err != nil {
			fmt.Println("FAIL profile:", err)
			os.Exit(1)
		}
		email = pr.Email
	case "microsoft":
		pr, err := microsoft.NewWithSource(acct, src).Profile(ctx)
		if err != nil {
			fmt.Println("FAIL profile:", err)
			os.Exit(1)
		}
		email = pr.Email
	}
	// the refresh token must work too: expire the access token and ask again
	tok.Expiry = time.Now().Add(-time.Hour)
	b, _ = json.Marshal(tok)
	sec.b = b
	if _, err := src.Token(ctx); err != nil {
		fmt.Println("FAIL refresh:", err)
		os.Exit(1)
	}
	fmt.Println("OK signed in as", email, "and refreshed")
}
