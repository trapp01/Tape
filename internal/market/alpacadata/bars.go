package alpacadata

import (
	"context"
	"fmt"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/trapp01/tape/internal/market"
)

// DailyBars returns up to `days` split-adjusted daily bars, oldest first. During
// regular hours the last bar is the session in progress; a caller that needs only
// completed sessions trims it against the calendar.
func (c *Client) DailyBars(ctx context.Context, symbol string, days int) ([]market.Bar, error) {
	if days <= 0 {
		return nil, fmt.Errorf("alpacadata: daily bars %s: days must be positive, got %d", symbol, days)
	}
	span := calendarSpan(days)

	raw, err := call(ctx, func() ([]marketdata.Bar, error) {
		return c.data.GetBars(symbol, marketdata.GetBarsRequest{
			TimeFrame:  marketdata.OneDay,
			Adjustment: marketdata.AdjustmentSplit,
			Start:      time.Now().UTC().AddDate(0, 0, -span),
			Feed:       c.feed,
			Sort:       marketdata.SortAsc,
			// A calendar span always holds fewer sessions than days, so the limit
			// caps the response without ever cutting the window short.
			TotalLimit: span,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("alpacadata: daily bars %s x%d (feed %s): %w", symbol, days, c.feed, err)
	}

	if len(raw) > days {
		raw = raw[len(raw)-days:]
	}
	out := make([]market.Bar, 0, len(raw))
	for _, b := range raw {
		out = append(out, market.Bar{
			Time:   b.Timestamp,
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: float64(b.Volume),
		})
	}
	return out, nil
}

// calendarSpan is how far back to ask for `days` sessions. Weekends cost two days
// in seven and the margin covers holiday weeks.
func calendarSpan(days int) int {
	return int(float64(days)*1.6) + 10
}
