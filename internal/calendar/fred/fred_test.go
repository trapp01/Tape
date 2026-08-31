package fred

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

// releaseDatesBody mixes two curated releases with one FRED carries but Tape
// does not show, so the filter has something to drop.
const releaseDatesBody = `{
  "realtime_start": "2026-09-10",
  "realtime_end": "2026-09-12",
  "order_by": "release_date",
  "sort_order": "asc",
  "count": 4,
  "offset": 0,
  "limit": 1000,
  "release_dates": [
    {"release_id": 10, "release_name": "Consumer Price Index", "date": "2026-09-11"},
    {"release_id": 192, "release_name": "Job Openings and Labor Turnover Survey", "date": "2026-09-10"},
    {"release_id": 375, "release_name": "Wilshire Indexes", "date": "2026-09-11"},
    {"release_id": 10, "release_name": "Consumer Price Index", "date": "2026-10-13"}
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

	p, err := New("secret-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.baseURL = srv.URL + "/fred/releases/dates"
	p.delay = 0
	return p
}

func TestNewWithoutKeyNamesTheEnvVar(t *testing.T) {
	_, err := New("")
	if !errors.Is(err, calendar.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if !strings.Contains(err.Error(), "FRED_API_KEY") {
		t.Errorf("err = %q, want it to name FRED_API_KEY", err)
	}
}

func TestEconomicSendsDocumentedQuery(t *testing.T) {
	et := eastLoc(t)
	var got struct {
		path  string
		query map[string]string
	}
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.query = map[string]string{}
		for k, v := range r.URL.Query() {
			got.query[k] = v[0]
		}
		io.WriteString(w, releaseDatesBody)
	})

	from := time.Date(2026, time.September, 10, 0, 0, 0, 0, et)
	to := time.Date(2026, time.September, 12, 23, 0, 0, 0, et)
	if _, err := p.Economic(context.Background(), from, to); err != nil {
		t.Fatalf("Economic: %v", err)
	}

	if got.path != "/fred/releases/dates" {
		t.Errorf("path = %q", got.path)
	}
	want := map[string]string{
		"api_key":                            "secret-key",
		"file_type":                          "json",
		"realtime_start":                     "2026-09-10",
		"realtime_end":                       "2026-09-12",
		"include_release_dates_with_no_data": "true",
		"sort_order":                         "asc",
	}
	for k, v := range want {
		if got.query[k] != v {
			t.Errorf("query %s = %q, want %q", k, got.query[k], v)
		}
	}
}

func TestEconomicMapsReleasesAndDropsUnknownIDs(t *testing.T) {
	et := eastLoc(t)
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, releaseDatesBody)
	})

	from := time.Date(2026, time.September, 10, 0, 0, 0, 0, et)
	to := time.Date(2026, time.September, 12, 23, 0, 0, 0, et)
	events, err := p.Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	// Wilshire is not curated and the October CPI is outside the range.
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}

	byTitle := map[string]calendar.Event{}
	for _, e := range events {
		byTitle[e.Title] = e
	}

	cpi, ok := byTitle["Consumer Price Index (CPI)"]
	if !ok {
		t.Fatalf("CPI missing from %+v", events)
	}
	// 8:30 AM ET on 11 September 2026 is daylight time, so 12:30 UTC.
	wantAt := time.Date(2026, time.September, 11, 12, 30, 0, 0, time.UTC)
	if !cpi.At.Equal(wantAt) {
		t.Errorf("CPI At = %s, want %s", cpi.At.Format(time.RFC3339), wantAt.Format(time.RFC3339))
	}
	if cpi.Kind != calendar.KindEconomic || cpi.Impact != calendar.ImpactHigh {
		t.Errorf("CPI kind/impact = %s/%s", cpi.Kind, cpi.Impact)
	}
	if cpi.Source != "fred.stlouisfed.org" {
		t.Errorf("CPI Source = %q", cpi.Source)
	}
	if cpi.Detail != "8:30 AM ET" {
		t.Errorf("CPI Detail = %q, want 8:30 AM ET", cpi.Detail)
	}
	if cpi.AllDay {
		t.Error("CPI AllDay = true, want false")
	}

	jolts, ok := byTitle["Job Openings and Labor Turnover (JOLTS)"]
	if !ok {
		t.Fatalf("JOLTS missing from %+v", events)
	}
	wantJOLTS := time.Date(2026, time.September, 10, 14, 0, 0, 0, time.UTC)
	if !jolts.At.Equal(wantJOLTS) {
		t.Errorf("JOLTS At = %s, want %s", jolts.At.Format(time.RFC3339), wantJOLTS.Format(time.RFC3339))
	}
	if jolts.Impact != calendar.ImpactMedium {
		t.Errorf("JOLTS Impact = %s, want medium", jolts.Impact)
	}
}

func TestEconomicRetriesOnceAfter429(t *testing.T) {
	et := eastLoc(t)
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error_message":"too many requests"}`)
			return
		}
		io.WriteString(w, releaseDatesBody)
	})

	from := time.Date(2026, time.September, 10, 0, 0, 0, 0, et)
	to := time.Date(2026, time.September, 12, 0, 0, 0, 0, et)
	events, err := p.Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2", calls)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

func TestEconomicGivesUpAfterTwoFailures(t *testing.T) {
	et := eastLoc(t)
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "upstream is down")
	})

	from := time.Date(2026, time.September, 10, 0, 0, 0, 0, et)
	_, err := p.Economic(context.Background(), from, from)
	if err == nil {
		t.Fatal("Economic: want an error")
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2", calls)
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream is down") {
		t.Errorf("err = %q, want the status and a body excerpt", err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Errorf("err = %q leaks the api key", err)
	}
}

func TestEconomicDoesNotRetryOn400(t *testing.T) {
	et := eastLoc(t)
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error_message":"Bad Request. api_key is not valid"}`)
	})

	from := time.Date(2026, time.September, 10, 0, 0, 0, 0, et)
	if _, err := p.Economic(context.Background(), from, from); err == nil {
		t.Fatal("Economic: want an error")
	}
	if calls != 1 {
		t.Errorf("made %d calls, want 1", calls)
	}
}

func TestNewEventWithoutAPublishedTimeIsAllDay(t *testing.T) {
	et := eastLoc(t)
	day := time.Date(2026, time.September, 11, 0, 0, 0, 0, et)

	got := newEvent(release{title: "Beige Book", impact: calendar.ImpactMedium}, day, et)
	if !got.AllDay {
		t.Fatal("AllDay = false, want true")
	}
	if got.Detail != "" {
		t.Errorf("Detail = %q, want empty", got.Detail)
	}
	// Noon Eastern so the UTC day still reads as the release day.
	if got.At.UTC().Format("2006-01-02") != "2026-09-11" {
		t.Errorf("At = %s, want a 2026-09-11 UTC day", got.At.Format(time.RFC3339))
	}
}
