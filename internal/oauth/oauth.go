// Package oauth is OAuth 2.0 for desktop apps: authorization code with PKCE
// through a loopback redirect, and refresh. Google and Microsoft both support
// exactly this flow for installed applications.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/patrickryan01/halyard/internal/plugin"
)

type Config struct {
	ClientID     string
	ClientSecret string // desktop clients have one that is not confidential; may be empty
	AuthURL      string
	TokenURL     string
	Scopes       []string
	Extra        url.Values // extra authorize params, such as access_type=offline
	// Ports are the loopback ports to try, in order. Empty means any free port, which is what Desktop and public
	// clients allow. A Web client registers exact redirect URIs, so it needs one or more fixed ports.
	Ports []int
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	// ClientID is saved with the token so a token keeps working if the build's
	// default client changes.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

func (t Token) Valid() bool {
	return t.AccessToken != "" && time.Until(t.Expiry) > 60*time.Second
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Pending is a started authorization the UI shows as a link.
type Pending struct {
	URL      string
	redirect string
	verifier string
	state    string
	ln       net.Listener
	srv      *http.Server
	done     chan result
}

type result struct {
	code string
	err  error
}

// Start opens the loopback listener and builds the sign-in URL. The caller
// sends the person to URL in their browser and then calls Wait.
func Start(c Config) (*Pending, error) {
	var ln net.Listener
	var err error
	for _, port := range append(append([]int{}, c.Ports...), 0) {
		if ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			break
		}
		if port == 0 {
			return nil, err
		}
		if len(c.Ports) > 0 && port == c.Ports[len(c.Ports)-1] {
			return nil, fmt.Errorf("sign-in needs one of the ports %v on this computer, and all are busy", c.Ports)
		}
	}
	p := &Pending{ln: ln, verifier: randString(48), state: randString(16), done: make(chan result, 1)}
	p.redirect = fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)
	sum := sha256.Sum256([]byte(p.verifier))
	q := url.Values{}
	for k, v := range c.Extra {
		q[k] = v
	}
	q.Set("client_id", c.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", p.redirect)
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("state", p.state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	p.URL = c.AuthURL + "?" + q.Encode()
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("state") != p.state {
			http.Error(w, "state mismatch", 400)
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			fmt.Fprintf(w, "<h3>Sign-in failed</h3><p>%s</p>", html.EscapeString(e+" "+r.URL.Query().Get("error_description")))
			select {
			case p.done <- result{err: errors.New(e)}:
			default:
			}
			return
		}
		fmt.Fprint(w, "<h3>Signed in.</h3><p>You can close this tab and go back to the app.</p>")
		select {
		case p.done <- result{code: r.URL.Query().Get("code")}:
		default:
		}
	})
	p.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go p.srv.Serve(ln)
	return p, nil
}

// Wait blocks until the browser comes back, then trades the code for tokens.
func (p *Pending) Wait(ctx context.Context, c Config) (Token, error) {
	defer p.srv.Close()
	select {
	case <-ctx.Done():
		return Token{}, ctx.Err()
	case r := <-p.done:
		if r.err != nil {
			return Token{}, r.err
		}
		return exchange(ctx, c, url.Values{"grant_type": {"authorization_code"}, "code": {r.code}, "redirect_uri": {p.redirect}, "code_verifier": {p.verifier}})
	}
}

func (p *Pending) Cancel() { p.srv.Close() }

func exchange(ctx context.Context, c Config, form url.Values) (Token, error) {
	form.Set("client_id", c.ClientID)
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	_ = json.Unmarshal(b, &tr)
	if resp.StatusCode != 200 || tr.AccessToken == "" {
		return Token{}, &RefreshError{Code: tr.Error, Desc: tr.ErrorDesc, Status: resp.StatusCode}
	}
	return Token{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, Expiry: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		ClientID: c.ClientID, ClientSecret: c.ClientSecret}, nil
}

// RefreshError is what the token endpoint said. invalid_grant means the
// person revoked access or the token aged out, and they need to sign in again.
type RefreshError struct {
	Code, Desc string
	Status     int
}

func (e *RefreshError) Error() string {
	return fmt.Sprintf("token endpoint %d: %s %s", e.Status, e.Code, e.Desc)
}

func (e *RefreshError) NeedsSignIn() bool {
	return e.Code == "invalid_grant" || e.Code == "interaction_required"
}

// Source hands out valid access tokens and persists refreshed ones.
type Source struct {
	Cfg Config
	Sec plugin.Secrets
	mu  sync.Mutex
	tok Token
	has bool
}

func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has {
		b, err := s.Sec.Get()
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(b, &s.tok); err != nil {
			return "", fmt.Errorf("stored sign-in is unreadable: %w", err)
		}
		s.has = true
	}
	if s.tok.Valid() {
		return s.tok.AccessToken, nil
	}
	cfg := s.Cfg
	if s.tok.ClientID != "" {
		cfg.ClientID, cfg.ClientSecret = s.tok.ClientID, s.tok.ClientSecret
	}
	nt, err := exchange(ctx, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.tok.RefreshToken}})
	if err != nil {
		return "", err
	}
	if nt.RefreshToken == "" {
		nt.RefreshToken = s.tok.RefreshToken
	}
	s.tok = nt
	b, _ := json.Marshal(nt)
	if err := s.Sec.Put(b); err != nil {
		return "", err
	}
	return nt.AccessToken, nil
}

// Static is a token source that returns one fixed token. It is for helper
// commands and for service delegation, where something else mints tokens.
type Static string

func (s Static) Token(context.Context) (string, error) { return string(s), nil }

// TokenSource is what API clients take.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// ParsePorts reads "49157,49158" into ports. Anything that is not a port is ignored.
func ParsePorts(v string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		var n int
		if _, err := fmt.Sscan(f, &n); err == nil && n > 1023 && n < 65536 {
			out = append(out, n)
		}
	}
	return out
}
