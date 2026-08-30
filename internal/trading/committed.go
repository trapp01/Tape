package trading

import (
	"context"
	"fmt"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// committedCash is what the journal's open buy orders will take out of the ledger
// when they fill. Cash a resting order already claims cannot be spent again.
func (e *Engine) committedCash(ctx context.Context) (float64, error) {
	open, err := e.openOrders(ctx)
	if err != nil {
		return 0, err
	}

	var (
		buys     []journal.Order
		unpriced []string
	)
	for _, o := range open {
		if o.Side != string(broker.Buy) || remainingQty(o) == 0 {
			continue
		}
		buys = append(buys, o)
		if o.LimitPrice == nil {
			unpriced = append(unpriced, o.Symbol)
		}
	}
	if len(buys) == 0 {
		return 0, nil
	}

	quotes := map[string]broker.Quote{}
	if len(unpriced) > 0 {
		if quotes, err = e.data.Quotes(ctx, unpriced); err != nil {
			return 0, fmt.Errorf("pricing %d open buy order(s) against the ledger: %w", len(unpriced), err)
		}
	}

	total := 0.0
	for _, o := range buys {
		price, err := committedPrice(o, quotes)
		if err != nil {
			return 0, err
		}
		total += e.estimatedCost(broker.Buy, remainingQty(o), price)
	}
	return total, nil
}

// committedSellQty is how many shares of symbol the journal's open sells will
// deliver, bracket legs included. An unfilled bracket claims its shares twice —
// only one leg can trade — which keeps the rule strict rather than wrong.
func (e *Engine) committedSellQty(ctx context.Context, symbol string) (int, error) {
	open, err := e.openOrders(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, o := range open {
		if o.Side == string(broker.Sell) && o.Symbol == symbol {
			total += remainingQty(o)
		}
	}
	return total, nil
}

func (e *Engine) openOrders(ctx context.Context) ([]journal.Order, error) {
	open, err := e.jnl.ListOrders(ctx, journal.ListFilter{Mode: e.mode, OpenOnly: true})
	if err != nil {
		return nil, fmt.Errorf("reading open orders to measure what they commit: %w", err)
	}
	return open, nil
}

// committedPrice is the open order's limit, or the ask a market order would cross.
// An order tape cannot price is an order it cannot let another one outspend.
func committedPrice(o journal.Order, quotes map[string]broker.Quote) (float64, error) {
	if o.LimitPrice != nil {
		return *o.LimitPrice, nil
	}
	q := quotes[o.Symbol]
	switch {
	case q.Ask > 0:
		return q.Ask, nil
	case q.Last > 0:
		return q.Last, nil
	}
	return 0, fmt.Errorf("open order #%d has no limit and no quote for %s, so the cash it commits cannot be measured (rule: no overspend)", o.ID, o.Symbol)
}

// remainingQty is what an order can still take, never negative.
func remainingQty(o journal.Order) int {
	if rem := o.Qty - o.FilledQty; rem > 0 {
		return rem
	}
	return 0
}
