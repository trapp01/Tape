package brief

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// fakeCalendar answers for the days it was given and refuses the rest, the way
// a venue calendar refuses a holiday.
type fakeCalendar struct {
	days map[string][2]time.Time
	err  error
}

func (c *fakeCalendar) SessionHours(_ context.Context, day string) (time.Time, time.Time, error) {
	if c.err != nil {
		return time.Time{}, time.Time{}, c.err
	}
	h, ok := c.days[day]
	if !ok {
		return time.Time{}, time.Time{}, errors.New("the venue does not trade that day")
	}
	return h[0], h[1], nil
}

func etAt(day string, hour, minute int) time.Time {
	d, err := time.ParseInLocation(market.DayLayout, day, market.Eastern())
	if err != nil {
		panic(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, market.Eastern())
}

// halfDayFeed is a session whose last print is 12:58 ET: the whole of a 13:00
// close, and two thirds of a normal one.
func halfDayFeed(day string) *fakeFeed {
	f := sessionFeed(map[string][2]float64{day: {510, 512.55}})
	s := f.sessions[sessionKey("SPY", day)]
	s.Complete = false
	s.LastBarAt = etAt(day, 12, 58)
	f.sessions[sessionKey("SPY", day)] = s
	return f
}

// The Friday after Thanksgiving closes at 13:00. Without the venue calendar its
// prints stop long before the fixed 15:55 floor, so the call could never grade.
func TestAnEarlyCloseGradesAgainstTheVenuesOwnBell(t *testing.T) {
	const day = "2026-11-27"
	st := testJournal(t)
	fileCallOn(t, st, day, "SPY", "up", 0.3)

	d := callDeps(st, halfDayFeed(day))
	d.Calendar = &fakeCalendar{days: map[string][2]time.Time{
		day: {etAt(day, 9, 30), etAt(day, 13, 0)},
	}}

	report, err := ScoreDue(context.Background(), d, day)
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 1 || len(report.Skipped) != 0 {
		t.Fatalf("a 13:00 close is a whole session, report = %+v", report)
	}
	if got := report.Scored[0].Outcome; got.Open != 510 || got.Close != 512.55 {
		t.Fatalf("graded against %v/%v, want the half day's own open and close", got.Open, got.Close)
	}
}

// The same prints on a 16:00 day are a session that stopped short, and grading
// them would score the morning and call it the day.
func TestTheSameBarsOnAFullDayDoNotGrade(t *testing.T) {
	const day = "2026-11-24"
	st := testJournal(t)
	fileCallOn(t, st, day, "SPY", "up", 0.3)

	d := callDeps(st, halfDayFeed(day))
	d.Calendar = &fakeCalendar{days: map[string][2]time.Time{
		day: {etAt(day, 9, 30), etAt(day, 16, 0)},
	}}

	report, err := ScoreDue(context.Background(), d, day)
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("prints ending at 12:58 do not finish a 16:00 session, report = %+v", report)
	}
	if !strings.Contains(report.Skipped[0], "session not final yet") {
		t.Fatalf("skip reason = %q", report.Skipped[0])
	}
	due, err := st.UnscoredCalls(context.Background(), journal.ModePaper, day)
	if err != nil {
		t.Fatalf("UnscoredCalls: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("the call must stay open for the next run, %d due", len(due))
	}
}

// A venue that cannot answer leaves the fixed floor in charge rather than
// grading on nothing.
func TestAnUnreadableCalendarFallsBackToTheFixedFloor(t *testing.T) {
	const day = "2026-11-27"
	st := testJournal(t)
	fileCallOn(t, st, day, "SPY", "up", 0.3)

	d := callDeps(st, halfDayFeed(day))
	d.Calendar = &fakeCalendar{err: errors.New("calendar unavailable")}

	report, err := ScoreDue(context.Background(), d, day)
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("without a calendar the 15:55 floor stands, report = %+v", report)
	}
}
