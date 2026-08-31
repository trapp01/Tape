package alpacadata

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const moversBody = `{
	"gainers": [
		{"symbol":"AGRI","price":4.15,"change":2.46,"percent_change":145.56},
		{"symbol":"BTAI","price":12.30,"change":3.10,"percent_change":33.70}
	],
	"losers": [
		{"symbol":"MTACW","price":0.1502,"change":-0.26,"percent_change":-63.07}
	],
	"market_type": "stocks",
	"last_updated": "2026-08-28T17:53:30.088309839Z"
}`

const activesBody = `{
	"most_actives": [
		{"symbol":"AAPL","trade_count":639626,"volume":122709184},
		{"symbol":"TSLA","trade_count":881204,"volume":98450112}
	],
	"last_updated": "2026-08-28T17:58:03Z"
}`

func TestTopMoversParsesBothSides(t *testing.T) {
	var query url.Values
	var headers http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta1/screener/stocks/movers" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query, headers = r.URL.Query(), r.Header
		writeJSON(t, w, http.StatusOK, moversBody)
	})

	gainers, losers, err := c.TopMovers(context.Background(), 5)
	if err != nil {
		t.Fatalf("TopMovers: %v", err)
	}
	if query.Get("top") != "5" {
		t.Errorf("top = %q, want 5", query.Get("top"))
	}
	if headers.Get("APCA-API-KEY-ID") != "key" || headers.Get("APCA-API-SECRET-KEY") != "secret" {
		t.Errorf("credentials missing from headers: %v", headers)
	}

	if len(gainers) != 2 || len(losers) != 1 {
		t.Fatalf("gainers = %d, losers = %d", len(gainers), len(losers))
	}
	top := gainers[0]
	if top.Symbol != "AGRI" || top.Price != 4.15 || top.Change != 2.46 || top.PercentChg != 145.56 {
		t.Errorf("top gainer = %+v", top)
	}
	worst := losers[0]
	if worst.Symbol != "MTACW" || worst.Price != 0.1502 || worst.Change != -0.26 || worst.PercentChg != -63.07 {
		t.Errorf("top loser = %+v", worst)
	}
}

func TestMostActivesRanksByVolume(t *testing.T) {
	var query url.Values
	var headers http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta1/screener/stocks/most-actives" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query, headers = r.URL.Query(), r.Header
		writeJSON(t, w, http.StatusOK, activesBody)
	})

	actives, err := c.MostActives(context.Background(), 2)
	if err != nil {
		t.Fatalf("MostActives: %v", err)
	}
	if query.Get("by") != "volume" || query.Get("top") != "2" {
		t.Errorf("query = %v", query)
	}
	if headers.Get("APCA-API-KEY-ID") != "key" || headers.Get("APCA-API-SECRET-KEY") != "secret" {
		t.Errorf("credentials missing from headers: %v", headers)
	}

	if len(actives) != 2 {
		t.Fatalf("actives = %+v", actives)
	}
	if actives[0].Symbol != "AAPL" || actives[0].Volume != 122709184 || actives[0].TradeCount != 639626 {
		t.Errorf("first active = %+v", actives[0])
	}
}

func TestScreenerClampsTop(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, moversBody)
	})

	if _, _, err := c.TopMovers(context.Background(), 0); err != nil {
		t.Fatalf("TopMovers: %v", err)
	}
	if query.Get("top") != "10" {
		t.Errorf("top = %q, want the venue default 10", query.Get("top"))
	}

	if _, _, err := c.TopMovers(context.Background(), 500); err != nil {
		t.Fatalf("TopMovers: %v", err)
	}
	if query.Get("top") != "50" {
		t.Errorf("top = %q, want the movers ceiling 50", query.Get("top"))
	}
}

func TestScreenerRetriesOnceOnAServerFault(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeJSON(t, w, http.StatusInternalServerError, `{"message":"internal error"}`)
			return
		}
		writeJSON(t, w, http.StatusOK, activesBody)
	})

	actives, err := c.MostActives(context.Background(), 2)
	if err != nil {
		t.Fatalf("MostActives: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("requests = %d, want a first failure and one retry", got)
	}
	if len(actives) != 2 {
		t.Fatalf("actives = %+v", actives)
	}
}

func TestScreenerDoesNotRetryARejectedKey(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(t, w, http.StatusUnauthorized, `{"message":"access key verification failed"}`)
	})

	_, _, err := c.TopMovers(context.Background(), 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("requests = %d, want no retry on a rejected key", got)
	}
	for _, want := range []string{"alpacadata: top movers (top 5)", "401", "access key verification failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestScreenerHonoursACancelledContext(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the venue")
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.MostActives(ctx, 5); err == nil {
		t.Fatal("expected an error")
	}
}
