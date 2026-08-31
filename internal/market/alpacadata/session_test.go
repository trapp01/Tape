package alpacadata

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// minuteBars is a short session: the open, a middle print, and a bar late enough
// to prove the session ran to the bell.
const minuteBars = `{"bars":{"SPY":[
	{"t":"2026-08-28T13:30:00Z","o":510.00,"h":510.80,"l":509.70,"c":510.40,"v":900000},
	{"t":"2026-08-28T17:00:00Z","o":510.40,"h":513.20,"l":508.10,"c":511.90,"v":400000},
	{"t":"2026-08-28T19:59:00Z","o":511.90,"h":512.70,"l":511.60,"c":512.55,"v":700000}
]},"next_page_token":null}`

// truncatedBars stops at 14:00 Eastern, which is a session still in progress.
const truncatedBars = `{"bars":{"SPY":[
	{"t":"2026-08-28T13:30:00Z","o":510.00,"h":510.80,"l":509.70,"c":510.40,"v":900000},
	{"t":"2026-08-28T18:00:00Z","o":510.40,"h":511.20,"l":510.10,"c":511.00,"v":400000}
]},"next_page_token":null}`

func TestSessionRequestsTheBellToBellWindow(t *testing.T) {
	cases := []struct {
		name, day, start, end string
	}{
		{"daylight time", "2026-08-28", "2026-08-28T13:30:00Z", "2026-08-28T19:59:59Z"},
		{"standard time", "2026-01-05", "2026-01-05T14:30:00Z", "2026-01-05T20:59:59Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var query url.Values
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/stocks/bars" {
					t.Errorf("path = %q", r.URL.Path)
				}
				query = r.URL.Query()
				writeJSON(t, w, http.StatusOK, minuteBars)
			})

			if _, err := c.Session(context.Background(), "SPY", tc.day); err != nil {
				t.Fatalf("Session: %v", err)
			}
			for _, want := range []struct{ param, value string }{
				{"symbols", "SPY"},
				{"timeframe", "1Min"},
				{"adjustment", "split"},
				{"sort", "asc"},
				{"feed", "iex"},
				{"start", tc.start},
				{"end", tc.end},
			} {
				if got := query.Get(want.param); got != want.value {
					t.Errorf("%s = %q, want %q", want.param, got, want.value)
				}
			}
		})
	}
}

func TestSessionFoldsTheMinuteBars(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, minuteBars)
	})

	s, err := c.Session(context.Background(), "SPY", "2026-08-28")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if s.Symbol != "SPY" || s.Day != "2026-08-28" {
		t.Fatalf("session = %+v", s)
	}
	if s.Open != 510.00 || s.Close != 512.55 {
		t.Errorf("open/close = %v/%v, want the first bar's open and the last bar's close", s.Open, s.Close)
	}
	if s.High != 513.20 || s.Low != 508.10 {
		t.Errorf("high/low = %v/%v, want 513.20/508.10", s.High, s.Low)
	}
	if s.Volume != 2000000 {
		t.Errorf("volume = %v, want the sum 2000000", s.Volume)
	}
	if !s.Complete {
		t.Error("a session with a 15:59 print has run to the bell")
	}
}

// A session whose prints stop at midday is still running; grading a call against
// it would score the morning and call it the day.
func TestSessionIsNotCompleteBeforeTheBell(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, truncatedBars)
	})

	s, err := c.Session(context.Background(), "SPY", "2026-08-28")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if s.Complete {
		t.Fatalf("session stopping at 14:00 ET must not be complete: %+v", s)
	}
	if s.Close != 511.00 {
		t.Errorf("close = %v, want the last print 511.00", s.Close)
	}
}

func TestSessionRejectsABadDay(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("no request should reach the venue")
	})

	if _, err := c.Session(context.Background(), "SPY", "28/08/2026"); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "2006-01-02") {
		t.Fatalf("error must name the layout, got: %v", err)
	}
}

func TestSessionWithoutPrintsNamesTheSymbolAndDay(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `{"bars":{},"next_page_token":null}`)
	})

	_, err := c.Session(context.Background(), "SPY", "2026-08-28")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SPY", "2026-08-28", "iex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
