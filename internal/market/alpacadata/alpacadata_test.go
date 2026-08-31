package alpacadata

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/market"
)

// newTestClient points the SDK data client and the screener client at h, which
// serves every data path from one handler.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := New(Options{
		APIKey:      "key",
		APISecret:   "secret",
		DataFeed:    "iex",
		dataBaseURL: srv.URL,
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
			_, err := New(Options{APIKey: tc.key, APISecret: tc.secret})
			if !errors.Is(err, market.ErrNotConfigured) {
				t.Fatalf("error = %v, want ErrNotConfigured", err)
			}
			for _, want := range []string{"ALPACA_API_KEY", "ALPACA_API_SECRET"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %s", err, want)
				}
			}
		})
	}
}

func TestNewDefaultsFeedToIEX(t *testing.T) {
	c, err := New(Options{APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.feed != "iex" {
		t.Fatalf("feed = %q, want iex", c.feed)
	}
	if c.dataBaseURL != defaultDataBaseURL {
		t.Fatalf("dataBaseURL = %q, want %q", c.dataBaseURL, defaultDataBaseURL)
	}
}

func TestNewKeepsTheConfiguredFeed(t *testing.T) {
	c, err := New(Options{APIKey: "key", APISecret: "secret", DataFeed: "sip"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.feed != "sip" {
		t.Fatalf("feed = %q, want sip", c.feed)
	}
}
