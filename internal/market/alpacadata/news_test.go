package alpacadata

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const newsBody = `{"news":[
	{
		"id": 40217123,
		"author": "Benzinga Newsdesk",
		"created_at": "2026-08-28T13:05:00Z",
		"updated_at": "2026-08-28T13:06:00Z",
		"headline": "Apple beats on revenue",
		"summary": "Services carried the quarter.",
		"url": "https://example.com/aapl-q3",
		"symbols": ["AAPL"]
	},
	{
		"id": 40217004,
		"author": "Reuters",
		"created_at": "2026-08-28T12:40:00Z",
		"updated_at": "2026-08-28T12:40:00Z",
		"headline": "Chip supply loosens",
		"summary": "Lead times shorten into the fall.",
		"url": "https://example.com/chips",
		"symbols": ["AAPL", "MSFT"]
	}
],"next_page_token":null}`

func TestNewsRequestShapeAndMapping(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta1/news" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, newsBody)
	})

	since := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	heads, err := c.News(context.Background(), []string{"AAPL", "MSFT"}, since, 5)
	if err != nil {
		t.Fatalf("News: %v", err)
	}

	for _, tc := range []struct{ param, want string }{
		{"symbols", "AAPL,MSFT"},
		{"start", "2026-08-28T12:00:00Z"},
		{"sort", "desc"},
		{"limit", "5"},
	} {
		if got := query.Get(tc.param); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.param, got, tc.want)
		}
	}
	if query.Get("include_content") != "" {
		t.Errorf("include_content = %q, want the article bodies left out", query.Get("include_content"))
	}

	if len(heads) != 2 {
		t.Fatalf("headlines = %d, want 2", len(heads))
	}
	first := heads[0]
	if first.ID != "40217123" {
		t.Errorf("ID = %q, want the numeric id as text", first.ID)
	}
	if first.Headline != "Apple beats on revenue" || first.Summary != "Services carried the quarter." {
		t.Errorf("first = %+v", first)
	}
	if first.Source != "Benzinga Newsdesk" {
		t.Errorf("Source = %q, want the byline", first.Source)
	}
	if first.URL != "https://example.com/aapl-q3" {
		t.Errorf("URL = %q", first.URL)
	}
	if len(first.Symbols) != 1 || first.Symbols[0] != "AAPL" {
		t.Errorf("Symbols = %v", first.Symbols)
	}
	if !first.CreatedAt.Equal(time.Date(2026, 8, 28, 13, 5, 0, 0, time.UTC)) {
		t.Errorf("CreatedAt = %v", first.CreatedAt)
	}
	if len(heads[1].Symbols) != 2 {
		t.Errorf("second Symbols = %v, want both tickers", heads[1].Symbols)
	}
}

func TestNewsWithoutSymbolsAsksForTheWholeWire(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"news":[],"next_page_token":null}`)
	})

	heads, err := c.News(context.Background(), nil, time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), 3)
	if err != nil {
		t.Fatalf("News: %v", err)
	}
	if _, ok := query["symbols"]; ok {
		t.Errorf("symbols = %q, want no filter", query.Get("symbols"))
	}
	if len(heads) != 0 {
		t.Fatalf("headlines = %+v, want empty", heads)
	}
}

func TestNewsRejectsNonPositiveLimit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the venue")
	})

	for _, limit := range []int{0, -5} {
		if _, err := c.News(context.Background(), nil, time.Now(), limit); err == nil {
			t.Errorf("limit = %d: expected an error", limit)
		}
	}
}

func TestNewsErrorNamesTheScope(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, `{"message":"forbidden"}`)
	})

	_, err := c.News(context.Background(), []string{"SPY"}, time.Now(), 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "alpacadata: news SPY since ") {
		t.Fatalf("error = %v", err)
	}
}
