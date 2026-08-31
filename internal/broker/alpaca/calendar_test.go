package alpaca

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/market"
)

func TestSessionHoursReadsTheVenuesOwnBells(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/calendar" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `[{"date":"2026-11-27","open":"09:30","close":"13:00"}]`)
	})

	open, closed, err := c.SessionHours(context.Background(), "2026-11-27")
	if err != nil {
		t.Fatalf("SessionHours: %v", err)
	}
	if query.Get("start") != "2026-11-27" || query.Get("end") != "2026-11-27" {
		t.Errorf("asked for %s..%s, want the one day", query.Get("start"), query.Get("end"))
	}
	if !open.Equal(easternAt(t, 2026, 11, 27, 9, 30)) {
		t.Errorf("open = %s, want 09:30 Eastern", open)
	}
	if !closed.Equal(easternAt(t, 2026, 11, 27, 13, 0)) {
		t.Errorf("close = %s, want the 13:00 half-day bell", closed)
	}
}

func TestSessionHoursAcceptsATimestampedCalendar(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK,
			`[{"date":"2026-11-27","open":"2026-11-27T09:30:00-05:00","close":"2026-11-27T13:00:00-05:00"}]`)
	})

	_, closed, err := c.SessionHours(context.Background(), "2026-11-27")
	if err != nil {
		t.Fatalf("SessionHours: %v", err)
	}
	if !closed.Equal(easternAt(t, 2026, 11, 27, 13, 0)) {
		t.Errorf("close = %s, want the 13:00 half-day bell", closed)
	}
}

func TestSessionHoursRefusesADayTheVenueDoesNotTrade(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, `[]`)
	})

	_, _, err := c.SessionHours(context.Background(), "2026-11-26")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "2026-11-26") {
		t.Fatalf("error must name the day, got: %v", err)
	}
}

func TestSessionHoursRejectsABadDay(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("no request should reach the venue")
	})

	_, _, err := c.SessionHours(context.Background(), "27/11/2026")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "2006-01-02") {
		t.Fatalf("error must name the layout, got: %v", err)
	}
}

func easternAt(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	return time.Date(year, month, day, hour, minute, 0, 0, market.Eastern())
}
