package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

// Ledger is tape's own account view for one mode. RealizedPL is net of the costs
// allocated to closed trades; Commissions and Fees are lifetime totals over every
// fill, closed or not.
func (s *Store) Ledger(ctx context.Context, mode string) (Ledger, error) {
	fills, err := s.Fills(ctx, time.Time{}, time.Time{}, mode)
	if err != nil {
		return Ledger{}, fmt.Errorf("journal: ledger for mode %q: %w", mode, err)
	}

	led := Ledger{StartingEquity: s.startingEquity, Cash: s.startingEquity}
	for _, f := range fills {
		notional := float64(f.Qty) * f.ModeledPrice
		if f.Side == string(broker.Sell) {
			led.Cash += notional
		} else {
			led.Cash -= notional
		}
		led.Cash -= f.Commission + f.Fees
		led.Commissions += f.Commission
		led.Fees += f.Fees
	}

	trades, positions := matchFIFO(fills)
	for _, t := range trades {
		led.RealizedPL += t.NetPL
	}
	led.OpenPositions = positions
	return led, nil
}

// ClosedTrades returns round trips closed in [from, to), oldest first. A zero
// from or to drops that bound.
func (s *Store) ClosedTrades(ctx context.Context, from, to time.Time, mode string) ([]Trade, error) {
	// FIFO needs every earlier fill to know what a sell is closing, so match the
	// whole history up to `to` and filter on the way out.
	fills, err := s.Fills(ctx, time.Time{}, to, mode)
	if err != nil {
		return nil, fmt.Errorf("journal: closed trades for mode %q: %w", mode, err)
	}
	trades, _ := matchFIFO(fills)

	out := make([]Trade, 0, len(trades))
	for _, t := range trades {
		if !from.IsZero() && t.ClosedAt.Before(from) {
			continue
		}
		if !to.IsZero() && !t.ClosedAt.Before(to) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// OpenPositions returns what the fill history leaves open, sorted by symbol.
func (s *Store) OpenPositions(ctx context.Context, mode string) ([]OpenPosition, error) {
	fills, err := s.Fills(ctx, time.Time{}, time.Time{}, mode)
	if err != nil {
		return nil, fmt.Errorf("journal: open positions for mode %q: %w", mode, err)
	}
	_, positions := matchFIFO(fills)
	return positions, nil
}

// DayRecap summarises one calendar day in loc. The user trades US markets from
// Mountain time, so the day boundary is local, not UTC. A nil loc means UTC.
func (s *Store) DayRecap(ctx context.Context, day time.Time, loc *time.Location, mode string) (DayRecap, error) {
	if loc == nil {
		loc = time.UTC
	}
	local := day.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)

	trades, err := s.ClosedTrades(ctx, start, end, mode)
	if err != nil {
		return DayRecap{}, fmt.Errorf("journal: day recap for %s: %w", start.Format("2006-01-02"), err)
	}

	rec := DayRecap{Day: start, Trades: trades}
	for _, t := range trades {
		rec.GrossPL += t.GrossPL
		rec.Costs += t.Costs
		rec.NetPL += t.NetPL
		switch {
		case t.NetPL > 0:
			rec.Wins++
		case t.NetPL < 0:
			rec.Losses++
		}
	}

	// Commissions on a position held overnight belong to today even though no trade
	// closed, so a day of entries never reads as a day that cost nothing.
	fills, err := s.Fills(ctx, start, end, mode)
	if err != nil {
		return DayRecap{}, fmt.Errorf("journal: day recap for %s: %w", start.Format("2006-01-02"), err)
	}
	for _, f := range fills {
		rec.FillCosts += f.Commission + f.Fees
	}

	rec.OrdersCount, err = s.countOrders(ctx, start, end, mode)
	if err != nil {
		return DayRecap{}, fmt.Errorf("journal: day recap for %s: %w", start.Format("2006-01-02"), err)
	}
	return rec, nil
}

func (s *Store) countOrders(ctx context.Context, from, to time.Time, mode string) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE submitted_at >= ? AND submitted_at < ?`
	args := []any{formatTime(from), formatTime(to)}
	if mode != "" {
		query += ` AND mode = ?`
		args = append(args, mode)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting orders: %w", err)
	}
	return n, nil
}
