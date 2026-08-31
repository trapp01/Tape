package fomc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/calendar"
)

func eastLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func TestEconomicResolvesDecisionDayToUTC(t *testing.T) {
	et := eastLoc(t)
	from := time.Date(2026, time.March, 1, 0, 0, 0, 0, et)
	to := time.Date(2026, time.March, 31, 23, 59, 0, 0, et)

	events, err := New().Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}

	got := events[0]
	// 18 March 2026 is daylight time, so 2:00 PM ET is 18:00 UTC.
	want := time.Date(2026, time.March, 18, 18, 0, 0, 0, time.UTC)
	if !got.At.Equal(want) {
		t.Errorf("At = %s, want %s", got.At.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got.Kind != calendar.KindFOMC || got.Impact != calendar.ImpactHigh {
		t.Errorf("kind/impact = %s/%s, want fomc/high", got.Kind, got.Impact)
	}
	if got.Source != "federalreserve.gov" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.AllDay {
		t.Error("AllDay = true, want false")
	}
	if got.Detail != "statement 2:00 PM ET, Summary of Economic Projections" {
		t.Errorf("Detail = %q", got.Detail)
	}
}

func TestEconomicUsesStandardTimeInDecember(t *testing.T) {
	et := eastLoc(t)
	from := time.Date(2026, time.December, 9, 0, 0, 0, 0, et)
	to := time.Date(2026, time.December, 9, 23, 0, 0, 0, et)

	events, err := New().Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	want := time.Date(2026, time.December, 9, 19, 0, 0, 0, time.UTC)
	if !events[0].At.Equal(want) {
		t.Errorf("At = %s, want %s", events[0].At.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestEconomicRangeIsInclusiveByDay(t *testing.T) {
	et := eastLoc(t)
	// Midnight to midnight on the decision day still contains the 2:00 PM statement.
	from := time.Date(2026, time.April, 29, 0, 0, 0, 0, et)
	to := time.Date(2026, time.April, 29, 0, 0, 0, 0, et)

	events, err := New().Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestEconomicEmptyRange(t *testing.T) {
	et := eastLoc(t)
	from := time.Date(2026, time.February, 2, 0, 0, 0, 0, et)
	to := time.Date(2026, time.February, 28, 0, 0, 0, 0, et)

	events, err := New().Economic(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Economic: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0: %+v", len(events), events)
	}
}

func TestNextSkipsPastMeetings(t *testing.T) {
	et := eastLoc(t)

	got, err := Next(time.Date(2026, time.June, 18, 0, 0, 0, 0, et))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	if !got.At.Equal(want) {
		t.Errorf("At = %s, want %s", got.At.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got.Title != "FOMC rate decision" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestNextIncludesTheMeetingInProgress(t *testing.T) {
	et := eastLoc(t)
	// 8:00 AM on decision day is before the 2:00 PM statement.
	got, err := Next(time.Date(2026, time.January, 28, 8, 0, 0, 0, et))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, time.January, 28, 19, 0, 0, 0, time.UTC)
	if !got.At.Equal(want) {
		t.Errorf("At = %s, want %s", got.At.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestNextPastTheTable(t *testing.T) {
	_, err := Next(time.Date(2028, time.January, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrNoMeeting) {
		t.Fatalf("err = %v, want ErrNoMeeting", err)
	}
}

func TestTableIsSortedAndComplete(t *testing.T) {
	et := eastLoc(t)
	if len(meetings) != 16 {
		t.Fatalf("got %d meetings, want 16 (eight per year for 2026 and 2027)", len(meetings))
	}
	var prev time.Time
	for _, m := range meetings {
		if m.year != 2026 && m.year != 2027 {
			t.Errorf("meeting in %d is outside the published table", m.year)
		}
		at := m.at(et)
		if !prev.IsZero() && !at.After(prev) {
			t.Errorf("meeting %s is not after %s; Next relies on the order",
				at.Format(time.RFC3339), prev.Format(time.RFC3339))
		}
		prev = at
	}
}
