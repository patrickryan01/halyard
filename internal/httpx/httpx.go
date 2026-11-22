// Package httpx is the small JSON-over-HTTP client the Google, Microsoft and
// JMAP plugins share: bearer auth, retry with backoff on 429 and 5xx, and
// errors that carry what the server said.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Client struct {
	HTTP  *http.Client
	Token func(ctx context.Context) (string, error)
}

type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("http %d: %s", e.Status, trunc(e.Body, 300)) }

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func New(tok func(context.Context) (string, error)) *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}, Token: tok}
}

// Do sends a request with a JSON body (or raw bytes when raw is set) and
// decodes a JSON reply into out when out is non-nil.
func (c *Client) Do(ctx context.Context, method, url string, body any, hdr map[string]string, out any) error {
	var payload []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		payload = b
	default:
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		tok, err := c.Token(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if payload != nil {
			if _, ok := body.([]byte); !ok {
				req.Header.Set("Content-Type", "application/json")
			}
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			last = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			sleep(ctx, backoff(attempt, ""))
			continue
		}
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			last = &Error{resp.StatusCode, string(rb)}
			sleep(ctx, backoff(attempt, resp.Header.Get("Retry-After")))
			continue
		}
		if resp.StatusCode >= 300 {
			return &Error{resp.StatusCode, string(rb)}
		}
		if out == nil || len(rb) == 0 {
			return nil
		}
		if raw, ok := out.(*[]byte); ok {
			*raw = rb
			return nil
		}
		return json.Unmarshal(rb, out)
	}
	return last
}

func backoff(attempt int, retryAfter string) time.Duration {
	if n, err := strconv.Atoi(retryAfter); err == nil && n > 0 && n < 120 {
		return time.Duration(n) * time.Second
	}
	return time.Duration(1<<attempt) * 500 * time.Millisecond
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func IsStatus(err error, code int) bool {
	e, ok := err.(*Error)
	return ok && e.Status == code
}
