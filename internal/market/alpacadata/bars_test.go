package alpacadata

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fiveDailyBars is a week of sessions, oldest first, as the venue sends them.
const fiveDailyBars = `{"bars":{"AAPL":[
	{"t":"2026-08-24T04:00:00Z","o":220.0,"h":223.0,"l":219.0,"c":222.5,"v":41000000},
	{"t":"2026-08-25T04:00:00Z","o":222.5,"h":226.0,"l":222.0,"c":225.1,"v":43000000},
	{"t":"2026-08-26T04:00:00Z","o":225.0,"h":228.0,"l":224.0,"c":227.8,"v":45000000},
	{"t":"2026-08-27T04:00:00Z","o":228.0,"h":229.5,"l":226.0,"c":228.3,"v":48000000},
	{"t":"2026-08-28T04:00:00Z","o":228.0,"h":232.1,"l":227.5,"c":231.45,"v":52000000}
]},"next_page_token":null}`

func TestDailyBarsRequestShape(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/stocks/bars" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, fiveDailyBars)
	})

	if _, err := c.DailyBars(context.Background(), "AAPL", 3); err != nil {
		t.Fatalf("DailyBars: %v", err)
	}

	for _, tc := range []struct{ param, want string }{
		{"symbols", "AAPL"},
		{"timeframe", "1Day"},
		{"adjustment", "split"},
		{"sort", "asc"},
		{"feed", "iex"},
		{"limit", strconv.Itoa(calendarSpan(3))},
	} {
		if got := query.Get(tc.param); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.param, got, tc.want)
		}
	}

	start, err := time.Parse(time.RFC3339, query.Get("start"))
	if err != nil {
		t.Fatalf("start = %q: %v", query.Get("start"), err)
	}
	if back := time.Since(start).Hours() / 24; back < float64(3) {
		t.Errorf("start is %.1f days back, too close to cover 3 sessions", back)
	}
}

func TestDailyBarsTrimsToTheNewestDays(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, fiveDailyBars)
	})

	bars, err := c.DailyBars(context.Background(), "AAPL", 3)
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if len(bars) != 3 {
		t.Fatalf("bars = %d, want 3", len(bars))
	}

	want := []time.Time{
		time.Date(2026, 8, 26, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 27, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 28, 4, 0, 0, 0, time.UTC),
	}
	for i, w := range want {
		if !bars[i].Time.Equal(w) {
			t.Errorf("bars[%d].Time = %v, want %v (oldest first)", i, bars[i].Time, w)
		}
	}

	last := bars[2]
	if last.Open != 228.0 || last.High != 232.1 || last.Low != 227.5 || last.Close != 231.45 {
		t.Errorf("last bar = %+v", last)
	}
	if last.Volume != 52000000 {
		t.Errorf("last bar volume = %v", last.Volume)
	}
}

func TestDailyBarsReturnsFewerWhenTheVenueHasFewer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, fiveDailyBars)
	})

	bars, err := c.DailyBars(context.Background(), "AAPL", 20)
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if len(bars) != 5 {
		t.Fatalf("bars = %d, want the 5 the venue sent", len(bars))
	}
}

func TestDailyBarsRejectsNonPositiveDays(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the venue")
	})

	for _, days := range []int{0, -1} {
		if _, err := c.DailyBars(context.Background(), "AAPL", days); err == nil {
			t.Errorf("days = %d: expected an error", days)
		}
	}
}

func TestDailyBarsErrorNamesTheSymbol(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, `{"message":"forbidden"}`)
	})

	_, err := c.DailyBars(context.Background(), "AAPL", 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "alpacadata: daily bars AAPL x5") {
		t.Fatalf("error = %v", err)
	}
}

func TestCalendarSpanCoversTheSessions(t *testing.T) {
	// Five weekdays in seven calendar days, so the span must exceed days * 7/5.
	for _, days := range []int{1, 5, 20, 60, 250} {
		span := calendarSpan(days)
		if want := float64(days) * 7 / 5; float64(span) < want {
			t.Errorf("calendarSpan(%d) = %d, too short to hold %d sessions", days, span, days)
		}
	}
}
