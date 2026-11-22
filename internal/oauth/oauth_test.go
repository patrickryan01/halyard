package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type mem struct {
	mu sync.Mutex
	b  []byte
}

func (m *mem) Get() ([]byte, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.b, nil }
func (m *mem) Put(b []byte) error   { m.mu.Lock(); defer m.mu.Unlock(); m.b = b; return nil }

func TestPKCEFlowAndRefresh(t *testing.T) {
	var gotVerifier, gotChallenge string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			gotVerifier = r.Form.Get("code_verifier")
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a1", "refresh_token": "r1", "expires_in": 3600})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "r1" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a2", "expires_in": 3600})
		}
	}))
	defer ts.Close()
	cfg := Config{ClientID: "cid", AuthURL: ts.URL + "/auth", TokenURL: ts.URL + "/token", Scopes: []string{"s1", "s2"}}
	p, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(p.URL)
	q := u.Query()
	gotChallenge = q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "cid" || q.Get("scope") != "s1 s2" || gotChallenge == "" {
		t.Fatalf("authorize url: %s", p.URL)
	}
	// the browser comes back to the loopback redirect
	go func() {
		time.Sleep(100 * time.Millisecond)
		bad, _ := http.Get(q.Get("redirect_uri") + "?state=wrong&code=x")
		if bad != nil {
			bad.Body.Close()
		}
		resp, err := http.Get(q.Get("redirect_uri") + "?state=" + q.Get("state") + "&code=thecode")
		if err == nil {
			resp.Body.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := p.Wait(ctx, cfg)
	if err != nil || tok.AccessToken != "a1" || tok.RefreshToken != "r1" {
		t.Fatalf("%+v %v", tok, err)
	}
	if gotVerifier == "" || strings.Contains(gotVerifier, "=") {
		t.Fatalf("verifier %q", gotVerifier)
	}
	// expired token refreshes and is stored
	tok.Expiry = time.Now().Add(-time.Hour)
	b, _ := json.Marshal(tok)
	sec := &mem{b: b}
	src := &Source{Cfg: cfg, Sec: sec}
	a, err := src.Token(ctx)
	if err != nil || a != "a2" {
		t.Fatalf("refresh: %q %v", a, err)
	}
	var saved Token
	json.Unmarshal(sec.b, &saved)
	if saved.AccessToken != "a2" || saved.RefreshToken != "r1" {
		t.Fatalf("stored %+v", saved)
	}
}

func TestFixedPorts(t *testing.T) {
	// a Web client registers exact redirect addresses: the flow must use the port asked for, and say so when none is free
	cfg := Config{ClientID: "c", AuthURL: "http://x/auth", TokenURL: "http://x/token", Ports: []int{47311, 47312}}
	p1, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p1.Cancel()
	if !strings.Contains(p1.URL, url.QueryEscape("http://127.0.0.1:47311/callback")) {
		t.Fatalf("first port not used: %s", p1.URL)
	}
	p2, err := Start(cfg) // the first is taken: the second one
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Cancel()
	if !strings.Contains(p2.URL, url.QueryEscape("http://127.0.0.1:47312/callback")) {
		t.Fatalf("second port not used: %s", p2.URL)
	}
	if _, err := Start(cfg); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("with every port taken the error must say so, got %v", err)
	}
	if got := ParsePorts("49157, 49158;80;x"); len(got) != 2 || got[0] != 49157 {
		t.Fatalf("ParsePorts: %v", got)
	}
}
