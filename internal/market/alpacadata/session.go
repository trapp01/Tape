package alpacadata

import (
	"context"
	"fmt"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/trapp01/tape/internal/market"
)

// The regular session runs 09:30 to 16:00 Eastern. The last one-minute bar opens
// at 15:59, so the window ends a second before the bell.
const (
	openHour, openMinute   = 9, 30
	closeHour, closeMinute = 15, 59
	// finalHour and finalMinute are the earliest print that can only exist in a
	// session that ran to the end.
	finalHour, finalMinute = 15, 55
)

// window is one session's bell-to-bell span. final is the timestamp a print must
// reach for the session to have run out.
type window struct {
	start, end, final time.Time
}

func sessionWindow(day string) (window, error) {
	et := market.Eastern()
	d, err := time.ParseInLocation(market.DayLayout, day, et)
	if err != nil {
		return window{}, err
	}
	at := func(h, m, s int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, s, 0, et)
	}
	return window{
		start: at(openHour, openMinute, 0),
		end:   at(closeHour, closeMinute, 59),
		final: at(finalHour, finalMinute, 0),
	}, nil
}

// SessionBars returns one day's regular-hours one-minute bars, oldest first, so a
// proposal can be replayed against the path the price actually took. On the free
// IEX feed these are one venue's prints, not the consolidated tape.
func (c *Client) SessionBars(ctx context.Context, symbol, day string) ([]market.Bar, error) {
	bars, _, err := c.sessionBars(ctx, symbol, day)
	return bars, err
}

// Session folds the same minute bars into the session a call is graded against.
// The open and close are the first and last regular prints, not the auctions;
// SIP carries those.
func (c *Client) Session(ctx context.Context, symbol, day string) (market.Session, error) {
	bars, w, err := c.sessionBars(ctx, symbol, day)
	if err != nil {
		return market.Session{}, err
	}
	return foldSession(symbol, day, bars, w.final), nil
}

// sessionBars is the one fetch both callers share, so the window can never drift
// between the bars a replay reads and the session a call is graded against.
func (c *Client) sessionBars(ctx context.Context, symbol, day string) ([]market.Bar, window, error) {
	w, err := sessionWindow(day)
	if err != nil {
		return nil, w, fmt.Errorf("alpacadata: session %s on %q: day must be %s", symbol, day, market.DayLayout)
	}

	raw, err := call(ctx, func() ([]marketdata.Bar, error) {
		return c.data.GetBars(symbol, marketdata.GetBarsRequest{
			TimeFrame:  marketdata.OneMin,
			Adjustment: marketdata.AdjustmentSplit,
			Start:      w.start.UTC(),
			End:        w.end.UTC(),
			Feed:       c.feed,
			Sort:       marketdata.SortAsc,
		})
	})
	if err != nil {
		return nil, w, fmt.Errorf("alpacadata: session %s on %s (feed %s): %w", symbol, day, c.feed, err)
	}
	if len(raw) == 0 {
		return nil, w, fmt.Errorf("alpacadata: session %s on %s (feed %s): no prints between the bells", symbol, day, c.feed)
	}
	return toBars(raw), w, nil
}

// foldSession reads the session off the bars, oldest first. A print at or after
// final can only come from a session that ran out.
func foldSession(symbol, day string, bars []market.Bar, final time.Time) market.Session {
	first, last := bars[0], bars[len(bars)-1]
	s := market.Session{
		Symbol:    symbol,
		Day:       day,
		Open:      first.Open,
		High:      first.High,
		Low:       first.Low,
		Close:     last.Close,
		Complete:  !last.Time.Before(final),
		LastBarAt: last.Time,
	}
	for _, b := range bars {
		s.High = max(s.High, b.High)
		s.Low = min(s.Low, b.Low)
		s.Volume += b.Volume
	}
	return s
}
