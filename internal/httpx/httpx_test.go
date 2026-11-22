package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRetriesAndAuth(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		if atomic.AddInt32(&n, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(func(context.Context) (string, error) { return "tok", nil })
	var out struct{ OK bool }
	if err := c.Do(context.Background(), "GET", srv.URL, nil, nil, &out); err != nil || !out.OK {
		t.Fatalf("%v %v", out, err)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
	if err := c.Do(context.Background(), "GET", srv.URL+"/x", nil, map[string]string{"Authorization": "Bearer bad"}, nil); !IsStatus(err, 401) {
		t.Fatalf("want 401, got %v", err)
	}
}
