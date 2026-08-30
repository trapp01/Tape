package alpaca

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points both the trading and data clients at h. The test server
// serves the trading and market-data paths from the same mux.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := New(Options{
		APIKey:         "key",
		APISecret:      "secret",
		Paper:          true,
		DataFeed:       "iex",
		tradingBaseURL: srv.URL,
		dataBaseURL:    srv.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("writing response: %v", err)
	}
}

func TestNewRejectsMissingKeys(t *testing.T) {
	for _, tc := range []struct{ name, key, secret string }{
		{"no key", "", "secret"},
		{"no secret", "key", ""},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Options{APIKey: tc.key, APISecret: tc.secret, Paper: true})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "ALPACA_API_KEY / ALPACA_API_SECRET not set") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewRefusesLive(t *testing.T) {
	_, err := New(Options{APIKey: "key", APISecret: "secret", Paper: false})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "live trading is not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewDefaultsFeedToIEX(t *testing.T) {
	c, err := New(Options{APIKey: "key", APISecret: "secret", Paper: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.feed != "iex" {
		t.Fatalf("feed = %q, want iex", c.feed)
	}
	if c.Name() != "alpaca" {
		t.Fatalf("Name() = %q, want alpaca", c.Name())
	}
}

func TestContextCancellationBeatsTheRequest(t *testing.T) {
	release := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		writeJSON(t, w, http.StatusOK, `{"id":"acct-1","equity":"1","cash":"1","buying_power":"1"}`)
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Account(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
