package trading

import (
	"context"
	"fmt"
	"time"
)

// Halted reports whether today's losses have stopped new entries, and why. The
// reason is the same sentence the refusal carries.
func (e *Engine) Halted(ctx context.Context) (bool, string, error) {
	if e.limits.MaxDailyLosses <= 0 {
		return false, "", nil
	}
	losers, err := e.losingPositions(ctx)
	if err != nil {
		return false, "", err
	}
	if losers < e.limits.MaxDailyLosses {
		return false, "", nil
	}
	return true, fmt.Sprintf("%d positions closed at a loss today, which is the daily limit of %d, so the day is done",
		losers, e.limits.MaxDailyLosses), nil
}

// losingPositions counts the symbols whose closed trades sum to a gross loss
// today. The rule is about being wrong on a position, so scaling out of one
// winner in clips the commission floor turns negative is not two losses, and a
// position exited in three clips is not three.
func (e *Engine) losingPositions(ctx context.Context) (int, error) {
	start, end := e.dayBounds()
	trades, err := e.jnl.ClosedTrades(ctx, start, end, e.mode)
	if err != nil {
		return 0, fmt.Errorf("reading today's closed trades to check the daily halt: %w", err)
	}

	gross := map[string]float64{}
	for _, t := range trades {
		gross[t.Symbol] += t.GrossPL
	}
	losers := 0
	for _, pl := range gross {
		if pl < 0 {
			losers++
		}
	}
	return losers, nil
}

// dayBounds is today in the engine's zone, the same window the day recap covers.
func (e *Engine) dayBounds() (time.Time, time.Time) {
	local := e.Today()
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, e.loc)
	return start, start.AddDate(0, 0, 1)
}
