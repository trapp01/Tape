// Package fomc serves FOMC decision days from a compiled-in table. It needs no
// API key and never reaches the network.
package fomc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/calendar"
)

const (
	dateLayout = "2006-01-02"
	source     = "federalreserve.gov"
)

// ErrNoMeeting reports that the table ends before the requested time, which is
// the signal to add the next year from the Fed's calendar page.
var ErrNoMeeting = errors.New("fomc: no scheduled meeting in the published calendar")

var _ calendar.EconomicProvider = Provider{}

// meeting is the second day of a two-day FOMC meeting, when the statement lands.
type meeting struct {
	year        int
	month       time.Month
	day         int
	projections bool
}

// meetings holds every scheduled decision day for 2026 and 2027.
// Source: https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm,
// retrieved 2026-08-30. The Fed marks each date tentative until the Committee
// confirms it at the meeting immediately before it, so 2027 can still move.
var meetings = []meeting{
	{2026, time.January, 28, false},
	{2026, time.March, 18, true},
	{2026, time.April, 29, false},
	{2026, time.June, 17, true},
	{2026, time.July, 29, false},
	{2026, time.September, 16, true},
	{2026, time.October, 28, false},
	{2026, time.December, 9, true},
	{2027, time.January, 27, false},
	{2027, time.March, 17, true},
	{2027, time.April, 28, false},
	{2027, time.June, 9, true},
	{2027, time.July, 28, false},
	{2027, time.September, 15, true},
	{2027, time.October, 27, false},
	{2027, time.December, 8, true},
}

// Provider reads the table above.
type Provider struct{}

func New() Provider { return Provider{} }

func (Provider) Name() string { return "FOMC" }

// Economic returns the decision days whose Eastern calendar date falls inside
// [from, to]. The range is read as days, not instants.
func (Provider) Economic(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	et, err := eastern()
	if err != nil {
		return nil, err
	}

	first, last := from.In(et).Format(dateLayout), to.In(et).Format(dateLayout)
	var out []calendar.Event
	for _, m := range meetings {
		at := m.at(et)
		if day := at.In(et).Format(dateLayout); day < first || day > last {
			continue
		}
		out = append(out, m.event(at))
	}
	return out, nil
}

// Next returns the first decision at or after the given time, or ErrNoMeeting
// once the table runs out.
func Next(after time.Time) (calendar.Event, error) {
	et, err := eastern()
	if err != nil {
		return calendar.Event{}, err
	}
	for _, m := range meetings {
		if at := m.at(et); !at.Before(after) {
			return m.event(at), nil
		}
	}
	return calendar.Event{}, ErrNoMeeting
}

// at is the statement release, 2:00 PM Eastern on the decision day.
func (m meeting) at(et *time.Location) time.Time {
	return time.Date(m.year, m.month, m.day, 14, 0, 0, 0, et).UTC()
}

func (m meeting) event(at time.Time) calendar.Event {
	detail := "statement 2:00 PM ET"
	if m.projections {
		detail += ", Summary of Economic Projections"
	}
	return calendar.Event{
		Kind:   calendar.KindFOMC,
		Title:  "FOMC rate decision",
		At:     at,
		Impact: calendar.ImpactHigh,
		Source: source,
		Detail: detail,
	}
}

func eastern() (*time.Location, error) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("fomc: loading America/New_York: %w", err)
	}
	return et, nil
}
