package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/secret"
	"github.com/patrickryan01/halyard/internal/store"
)

func newServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := engine.New(st, &secret.Store{Dir: t.TempDir(), NoKeyring: true})
	return New(e, "test", nil), st
}

// An account that has not synced yet has no folders. The state must say so with
// empty lists, because the UI maps over them: a null here crashed the first run.
func TestStateWithAccountButNoFoldersYet(t *testing.T) {
	s, st := newServer(t)
	if err := st.PutAccount(mailtypes.Account{ID: "a", Kind: "imap", Email: "a@x.test"}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/state", nil))
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"folders", "accounts", "outbox"} {
		if string(out[k]) == "null" || out[k] == nil {
			t.Fatalf("%s must be a list, got %s", k, out[k])
		}
	}
}

func TestTokenRequiredInDaemonMode(t *testing.T) {
	s, _ := newServer(t)
	s.Token = "secret"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "127.0.0.1:1"
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("want 401 without the token, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "evil.example"
	req.Header.Set("X-Token", "secret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("want 403 for a foreign host (DNS rebinding), got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "127.0.0.1:1"
	req.Header.Set("X-Token", "secret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("want 200 with the token, got %d", rr.Code)
	}
}

// A customer brings their own sign-in app: saved under Settings, it is what the next sign-in uses, with no build default needed.
func TestSavedSignInAppIsUsed(t *testing.T) {
	s, st := newServer(t)
	if _, _, err := s.StartOAuth("google", "", "", "", false); err == nil {
		t.Fatal("with no sign-in app anywhere the answer must be an error, not a broken link")
	}
	_ = st.SetSetting("oauth_google_client_id", "customer-id.apps.googleusercontent.com")
	_ = st.SetSetting("oauth_google_client_secret", "customer-secret")
	id, u, err := s.StartOAuth("google", "", "", "", false)
	if err != nil || id == "" || !strings.Contains(u, "client_id=customer-id.apps.googleusercontent.com") {
		t.Fatalf("saved app not used: %v %s", err, u)
	}
	s.pending[id].p.Cancel()
	_ = st.SetSetting("oauth_microsoft_client_id", "ms-app-id")
	_ = st.SetSetting("oauth_microsoft_tenant", "contoso.onmicrosoft.com")
	id, u, err = s.StartOAuth("microsoft", "", "", "", false)
	if err != nil || !strings.Contains(u, "contoso.onmicrosoft.com") || !strings.Contains(u, "client_id=ms-app-id") {
		t.Fatalf("microsoft: %v %s", err, u)
	}
	s.pending[id].p.Cancel()
}
