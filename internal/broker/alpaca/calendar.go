package alpaca

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/market"
)

var _ broker.SessionCalendar = (*Client)(nil)

// clockLayouts are the shapes Alpaca's calendar has used for a session's bells:
// wall-clock Eastern first, RFC 3339 as the fallback.
var clockLayouts = []string{"15:04", time.RFC3339}

// SessionHours returns the venue's own open and close for one session, so a
// half day (13:00 ET on the Friday after Thanksgiving, Christmas Eve) is graded
// against the bell it actually rang.
func (c *Client) SessionHours(ctx context.Context, day string) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation(market.DayLayout, day, market.Eastern())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("alpaca: session hours on %q: day must be %s", day, market.DayLayout)
	}
	days, err := call(ctx, func() ([]sdk.CalendarDay, error) {
		return c.trading.GetCalendar(sdk.GetCalendarRequest{Start: d, End: d})
	})
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("alpaca: session hours on %s: %w", day, err)
	}
	for _, cd := range days {
		if cd.Date != day {
			continue
		}
		open, err := sessionBell(d, cd.Open)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("alpaca: session hours on %s: open %q: %w", day, cd.Open, err)
		}
		closed, err := sessionBell(d, cd.Close)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("alpaca: session hours on %s: close %q: %w", day, cd.Close, err)
		}
		return open, closed, nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("alpaca: session hours on %s: the venue does not trade that day", day)
}

// sessionBell reads one bell off the calendar row. A wall-clock time is dated
// onto the session's own Eastern day; a full timestamp already carries one.
func sessionBell(day time.Time, value string) (time.Time, error) {
	for _, layout := range clockLayouts {
		t, err := time.ParseInLocation(layout, value, market.Eastern())
		if err != nil {
			continue
		}
		if layout == time.RFC3339 {
			return t, nil
		}
		return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, market.Eastern()), nil
	}
	return time.Time{}, fmt.Errorf("not a %s or RFC 3339 time", clockLayouts[0])
}
