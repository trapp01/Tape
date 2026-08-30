package trading

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// Positions starts from tape's own fill history and decorates it with the venue's
// current price. Quantity and cost basis never come from the broker; only the
// price does.
func (e *Engine) Positions(ctx context.Context) ([]PositionView, error) {
	held, err := e.jnl.OpenPositions(ctx, e.mode)
	if err != nil {
		return nil, fmt.Errorf("reading tape positions: %w", err)
	}
	if len(held) == 0 {
		return nil, nil
	}

	prices, err := e.currentPrices(ctx, held)
	if err != nil {
		return nil, err
	}

	out := make([]PositionView, 0, len(held))
	for _, p := range held {
		v := PositionView{
			Symbol:        p.Symbol,
			Qty:           p.Qty,
			AvgEntryPrice: p.AvgEntryPrice,
			CostBasis:     p.CostBasis,
			OpenedAt:      p.OpenedAt,
		}
		if price := prices[p.Symbol]; price > 0 {
			v.Priced = true
			v.CurrentPrice = price
			v.MarketValue = price * float64(p.Qty)
			v.UnrealizedPL = (price - p.AvgEntryPrice) * float64(p.Qty)
			if p.CostBasis != 0 {
				v.UnrealizedPct = v.UnrealizedPL / math.Abs(p.CostBasis) * 100
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// currentPrices prefers the venue's own mark and falls back to the quote feed for
// symbols the broker has no position in.
func (e *Engine) currentPrices(ctx context.Context, held []journal.OpenPosition) (map[string]float64, error) {
	prices := make(map[string]float64, len(held))
	positions, err := e.broker.Positions(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading broker positions: %w", err)
	}
	for _, p := range positions {
		if p.CurrentPrice > 0 {
			prices[p.Symbol] = p.CurrentPrice
		}
	}

	var missing []string
	for _, p := range held {
		if prices[p.Symbol] == 0 {
			missing = append(missing, p.Symbol)
		}
	}
	if len(missing) == 0 {
		return prices, nil
	}

	quotes, err := e.data.Quotes(ctx, missing)
	if err != nil {
		return nil, fmt.Errorf("quoting %s: %w", strings.Join(missing, ", "), err)
	}
	for symbol, q := range quotes {
		if q.Last > 0 {
			prices[symbol] = q.Last
		}
	}
	return prices, nil
}

// Today is the current trading day in the engine's configured zone. Day-boundary
// stats use it instead of a bare time.Now.
func (e *Engine) Today() time.Time {
	return e.now().In(e.loc)
}
