package finnhub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/calendar"
)

// earningsBody carries one report per hour code plus a symbol nobody asked for.
const earningsBody = `{
  "earningsCalendar": [
    {"date": "2026-10-29", "epsActual": null, "epsEstimate": 2.35, "hour": "amc",
     "quarter": 4, "revenueActual": null, "revenueEstimate": 102400000000, "symbol": "AAPL", "year": 2026},
    {"date": "2026-10-28", "epsActual": null, "epsEstimate": 3.61, "hour": "bmo",
     "quarter": 1, "revenueActual": null, "revenueEstimate": 74800000000, "symbol": "msft", "year": 2027},
    {"date": "2026-10-28", "epsActual": null, "epsEstimate": null, "hour": "dmh",
     "quarter": 3, "revenueActual": null, "revenueEstimate": null, "symbol": "NVDA", "year": 2026},
    {"date": "2026-10-29", "epsActual": null, "epsEstimate": 0.62, "hour": "amc",
     "quarter": 3, "revenueActual": null, "revenueEstimate": 26100000000, "symbol": "TSLA", "year": 2026}
  ]
}`

func eastLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func newTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	p, err := New("secret-token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.baseURL = srv.URL + "/api/v1/calendar/earnings"
	p.delay = 0
	return p
}

func TestNewWithoutKeyNamesTheEnvVar(t *testing.T) {
	_, err := New("")
	if !errors.Is(err, calendar.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(err.Error(), "FINNHUB_API_KEY") {
		t.Errorf("err = %q, want it to name FINNHUB_API_KEY", err)
	}
}

func TestEarningsSendsDocumentedQuery(t *testing.T) {
	et := eastLoc(t)
	var gotPath string
	var gotQuery map[string]string
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = map[string]string{}
		for k, v := range r.URL.Query() {
			gotQuery[k] = v[0]
		}
		io.WriteString(w, earningsBody)
	})

	from := time.Date(2026, time.October, 26, 0, 0, 0, 0, et)
	to := time.Date(2026, time.October, 30, 23, 0, 0, 0, et)
	if _, err := p.Earnings(context.Background(), []string{"AAPL"}, from, to); err != nil {
		t.Fatalf("Earnings: %v", err)
	}

	if gotPath != "/api/v1/calendar/earnings" {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{"from": "2026-10-26", "to": "2026-10-30", "token": "secret-token"}
	for k, v := range want {
		if gotQuery[k] != v {
			t.Errorf("query %s = %q, want %q", k, gotQuery[k], v)
		}
	}
	if _, ok := gotQuery["symbol"]; ok {
		t.Error("query carries a symbol filter; the endpoint takes none")
	}
}

func TestEarningsFiltersToWatchlistAndMapsHours(t *testing.T) {
	et := eastLoc(t)
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, earningsBody)
	})

	from := time.Date(2026, time.October, 26, 0, 0, 0, 0, et)
	to := time.Date(2026, time.October, 30, 0, 0, 0, 0, et)
	events, err := p.Earnings(context.Background(), []string{"aapl", "MSFT", "NVDA"}, from, to)
	if err != nil {
		t.Fatalf("Earnings: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (TSLA is off the watchlist): %+v", len(events), events)
	}

	bySymbol := map[string]calendar.Event{}
	for _, e := range events {
		bySymbol[e.Symbol] = e
	}

	aapl := bySymbol["AAPL"]
	// 4:30 PM ET on 29 October 2026 is daylight time, so 20:30 UTC.
	wantAAPL := time.Date(2026, time.October, 29, 20, 30, 0, 0, time.UTC)
	if !aapl.At.Equal(wantAAPL) {
		t.Errorf("AAPL At = %s, want %s", aapl.At.Format(time.RFC3339), wantAAPL.Format(time.RFC3339))
	}
	if aapl.AllDay {
		t.Error("AAPL AllDay = true, want false")
	}
	if aapl.Kind != calendar.KindEarnings || aapl.Impact != calendar.ImpactHigh {
		t.Errorf("AAPL kind/impact = %s/%s", aapl.Kind, aapl.Impact)
	}
	if aapl.Title != "AAPL earnings" || aapl.Source != "finnhub.io" {
		t.Errorf("AAPL title/source = %q/%q", aapl.Title, aapl.Source)
	}
	if aapl.Detail != "after close, Q4 2026, EPS est 2.35, revenue est $102.4B" {
		t.Errorf("AAPL Detail = %q", aapl.Detail)
	}

	msft := bySymbol["MSFT"]
	wantMSFT := time.Date(2026, time.October, 28, 11, 0, 0, 0, time.UTC)
	if !msft.At.Equal(wantMSFT) {
		t.Errorf("MSFT At = %s, want %s", msft.At.Format(time.RFC3339), wantMSFT.Format(time.RFC3339))
	}
	if !strings.HasPrefix(msft.Detail, "before open, Q1 2027, EPS est 3.61") {
		t.Errorf("MSFT Detail = %q", msft.Detail)
	}

	nvda := bySymbol["NVDA"]
	if !nvda.AllDay {
		t.Error("NVDA AllDay = false, want true for an intraday report")
	}
	if nvda.At.UTC().Format("2006-01-02") != "2026-10-28" {
		t.Errorf("NVDA At = %s, want a 2026-10-28 UTC day", nvda.At.Format(time.RFC3339))
	}
	if nvda.Detail != "during market hours, Q3 2026" {
		t.Errorf("NVDA Detail = %q", nvda.Detail)
	}
}

func TestEarningsWithoutSymbolsSkipsTheRequest(t *testing.T) {
	et := eastLoc(t)
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, earningsBody)
	})

	from := time.Date(2026, time.October, 26, 0, 0, 0, 0, et)
	events, err := p.Earnings(context.Background(), nil, from, from)
	if err != nil {
		t.Fatalf("Earnings: %v", err)
	}
	if len(events) != 0 || calls != 0 {
		t.Errorf("got %d events after %d calls, want 0 and 0", len(events), calls)
	}
}

func TestEarningsRetriesOnceAfter429(t *testing.T) {
	et := eastLoc(t)
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, "API limit reached")
			return
		}
		io.WriteString(w, earningsBody)
	})

	from := time.Date(2026, time.October, 26, 0, 0, 0, 0, et)
	to := time.Date(2026, time.October, 30, 0, 0, 0, 0, et)
	events, err := p.Earnings(context.Background(), []string{"AAPL"}, from, to)
	if err != nil {
		t.Fatalf("Earnings: %v", err)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2", calls)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}
}

func TestEarningsErrorKeepsTheTokenOut(t *testing.T) {
	et := eastLoc(t)
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "Invalid API key")
	})

	from := time.Date(2026, time.October, 26, 0, 0, 0, 0, et)
	_, err := p.Earnings(context.Background(), []string{"AAPL"}, from, from)
	if err == nil {
		t.Fatal("Earnings: want an error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("err = %q leaks the token", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %q, want the status", err)
	}
}

func TestCompactRevenue(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{102400000000, "$102.4B"},
		{945000000, "$945M"},
		{1500, "$1500"},
	}
	for _, c := range cases {
		if got := compact(c.in); got != c.want {
			t.Errorf("compact(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
