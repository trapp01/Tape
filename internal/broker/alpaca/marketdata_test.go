package alpaca

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

func TestQuotesUsesConfiguredFeed(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/stocks/quotes/latest" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{"quotes":{
			"AAPL": {"t":"2026-08-30T14:30:00Z","bp":190.00,"bs":3,"ap":190.10,"as":5},
			"MSFT": {"t":"2026-08-30T14:30:01Z","bp":410.00,"bs":1,"ap":410.20,"as":2}
		}}`)
	})

	quotes, err := c.Quotes(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if query.Get("feed") != "iex" {
		t.Fatalf("feed = %q, want iex", query.Get("feed"))
	}
	if query.Get("symbols") != "AAPL,MSFT" {
		t.Fatalf("symbols = %q", query.Get("symbols"))
	}
	if len(quotes) != 2 {
		t.Fatalf("quotes = %+v", quotes)
	}
	aapl := quotes["AAPL"]
	if aapl.Symbol != "AAPL" || aapl.Bid != 190.00 || aapl.Ask != 190.10 {
		t.Fatalf("AAPL = %+v", aapl)
	}
	if aapl.BidSize != 3 || aapl.AskSize != 5 {
		t.Fatalf("AAPL sizes = %+v", aapl)
	}
	if aapl.Last != 190.05 {
		t.Fatalf("AAPL last = %v, want the 190.05 midpoint", aapl.Last)
	}
	if !aapl.Timestamp.Equal(time.Date(2026, 8, 30, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("AAPL timestamp = %v", aapl.Timestamp)
	}
}

func TestQuoteMissingSymbol(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"quotes":{}}`)
	})

	_, err := c.Quote(context.Background(), "NOPE")
	if !errors.Is(err, ErrNoQuote) {
		t.Fatalf("error = %v, want ErrNoQuote", err)
	}
}

func TestQuotesEmptySymbolsSkipsRequest(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be made for an empty symbol list")
	})

	quotes, err := c.Quotes(context.Background(), nil)
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if len(quotes) != 0 {
		t.Fatalf("quotes = %+v, want empty", quotes)
	}
}

// StreamQuotes needs a websocket peer, so only its argument checks run offline.
// The subscribe-and-block path is exercised against the live feed by hand.
func TestStreamQuotesRejectsBadArguments(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the venue")
	})

	if err := c.StreamQuotes(context.Background(), nil, func(broker.Quote) {}); err == nil {
		t.Fatal("expected an error for no symbols")
	}
	if err := c.StreamQuotes(context.Background(), []string{"AAPL"}, nil); err == nil {
		t.Fatal("expected an error for a nil handler")
	}
}
