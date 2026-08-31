package alpacadata

import (
	"context"
	"fmt"
	"strings"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/trapp01/tape/internal/market"
)

// Snapshots returns the latest picture of each symbol. A symbol the feed does not
// carry is absent from the map rather than an error, so one bad ticker on the
// watchlist does not cost the briefing the rest.
func (c *Client) Snapshots(ctx context.Context, symbols []string) (map[string]market.Snapshot, error) {
	if len(symbols) == 0 {
		return map[string]market.Snapshot{}, nil
	}
	raw, err := call(ctx, func() (map[string]*marketdata.Snapshot, error) {
		return c.data.GetSnapshots(symbols, marketdata.GetSnapshotRequest{Feed: c.feed})
	})
	if err != nil {
		return nil, fmt.Errorf("alpacadata: snapshots %s (feed %s): %w", strings.Join(symbols, ","), c.feed, err)
	}

	out := make(map[string]market.Snapshot, len(raw))
	for symbol, s := range raw {
		if s == nil {
			continue
		}
		out[symbol] = toSnapshot(symbol, s)
	}
	return out, nil
}

// toSnapshot fills in whichever parts the venue sent; every sub-bar is optional.
func toSnapshot(symbol string, s *marketdata.Snapshot) market.Snapshot {
	out := market.Snapshot{Symbol: symbol}
	if t := s.LatestTrade; t != nil {
		out.Last = t.Price
		out.LastAt = t.Timestamp
	}
	if q := s.LatestQuote; q != nil {
		out.Bid = q.BidPrice
		out.Ask = q.AskPrice
	}
	if p := s.PrevDailyBar; p != nil {
		out.PrevClose = p.Close
	}
	// DailyBar carries the session in progress, so TodayOpen and Volume stay zero
	// until the feed prints the first trade of the day.
	if d := s.DailyBar; d != nil {
		out.TodayOpen = d.Open
		out.Volume = float64(d.Volume)
	}
	return out
}
