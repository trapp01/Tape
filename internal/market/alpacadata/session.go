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

// Session folds one day's one-minute bars into the session a call is graded
// against. On the free IEX feed these are one venue's prints, not the official
// opening and closing auction prices; SIP carries those.
func (c *Client) Session(ctx context.Context, symbol, day string) (market.Session, error) {
	et := market.Eastern()
	d, err := time.ParseInLocation(market.DayLayout, day, et)
	if err != nil {
		return market.Session{}, fmt.Errorf("alpacadata: session %s on %q: day must be %s", symbol, day, market.DayLayout)
	}
	start := time.Date(d.Year(), d.Month(), d.Day(), openHour, openMinute, 0, 0, et)
	end := time.Date(d.Year(), d.Month(), d.Day(), closeHour, closeMinute, 59, 0, et)

	raw, err := call(ctx, func() ([]marketdata.Bar, error) {
		return c.data.GetBars(symbol, marketdata.GetBarsRequest{
			TimeFrame:  marketdata.OneMin,
			Adjustment: marketdata.AdjustmentSplit,
			Start:      start.UTC(),
			End:        end.UTC(),
			Feed:       c.feed,
			Sort:       marketdata.SortAsc,
		})
	})
	if err != nil {
		return market.Session{}, fmt.Errorf("alpacadata: session %s on %s (feed %s): %w", symbol, day, c.feed, err)
	}
	if len(raw) == 0 {
		return market.Session{}, fmt.Errorf("alpacadata: session %s on %s (feed %s): no prints between the bells", symbol, day, c.feed)
	}

	final := time.Date(d.Year(), d.Month(), d.Day(), finalHour, finalMinute, 0, 0, et)
	return foldSession(symbol, day, raw, final), nil
}

// foldSession reads the session off the bars, oldest first. A print at or after
// final can only come from a session that ran out.
func foldSession(symbol, day string, bars []marketdata.Bar, final time.Time) market.Session {
	first, last := bars[0], bars[len(bars)-1]
	s := market.Session{
		Symbol:   symbol,
		Day:      day,
		Open:     first.Open,
		High:     first.High,
		Low:      first.Low,
		Close:    last.Close,
		Complete: !last.Timestamp.Before(final),
	}
	for _, b := range bars {
		s.High = max(s.High, b.High)
		s.Low = min(s.Low, b.Low)
		s.Volume += float64(b.Volume)
	}
	return s
}
