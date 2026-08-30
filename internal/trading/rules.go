package trading

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/trapp01/tape/internal/broker"
)

// check runs the Phase 0 guardrails. Each refusal names the rule and the numbers,
// because a refusal the trader cannot verify is a refusal they will work around.
func (e *Engine) check(ctx context.Context, req broker.OrderRequest) error {
	if req.Symbol == "" {
		return fmt.Errorf("symbol is empty (rule: valid order)")
	}
	if req.Qty <= 0 {
		return fmt.Errorf("qty must be a positive whole number of shares, got %d (rule: valid order)", req.Qty)
	}
	// Both sides price off the limit when one is set, so both have to carry a real one.
	if req.LimitPrice != nil && *req.LimitPrice <= 0 {
		return fmt.Errorf("limit price must be positive, got %v (rule: valid order)", *req.LimitPrice)
	}
	switch req.Side {
	case broker.Buy:
		return e.checkOverspend(ctx, req)
	case broker.Sell:
		return e.checkShorting(ctx, req)
	default:
		return fmt.Errorf("side must be %q or %q, got %q (rule: valid order)", broker.Buy, broker.Sell, req.Side)
	}
}

// checkShorting keeps every sell covered by shares the tape ledger actually holds
// and has not already promised to a resting sell or a bracket leg.
func (e *Engine) checkShorting(ctx context.Context, req broker.OrderRequest) error {
	positions, err := e.jnl.OpenPositions(ctx, e.mode)
	if err != nil {
		return fmt.Errorf("reading tape positions: %w", err)
	}
	held := 0
	for _, p := range positions {
		if p.Symbol == req.Symbol {
			held = p.Qty
			break
		}
	}
	committed, err := e.committedSellQty(ctx, req.Symbol)
	if err != nil {
		return err
	}
	if free := held - committed; req.Qty > free {
		if committed > 0 {
			return fmt.Errorf("selling %d %s but the ledger holds %d with %d already committed to open sells, leaving %d (rule: no shorting)",
				req.Qty, req.Symbol, held, committed, free)
		}
		return fmt.Errorf("selling %d %s but the ledger holds %d (rule: no shorting)", req.Qty, req.Symbol, held)
	}
	return nil
}

// checkOverspend prices the buy off its limit, or the live ask, and measures it
// against the tape ledger's cash less what open buys already claim. Alpaca's
// paper balance is not the account.
func (e *Engine) checkOverspend(ctx context.Context, req broker.OrderRequest) error {
	price, err := e.entryPrice(ctx, req)
	if err != nil {
		return err
	}
	cost := e.estimatedCost(broker.Buy, req.Qty, price)
	led, err := e.jnl.Ledger(ctx, e.mode)
	if err != nil {
		return fmt.Errorf("reading tape ledger: %w", err)
	}
	committed, err := e.committedCash(ctx)
	if err != nil {
		return err
	}
	if free := led.Cash - committed; cost > free {
		if committed > 0 {
			return fmt.Errorf("ledger cash %s less %s committed to open orders leaves %s < cost %s for %d %s at %s (rule: no overspend)",
				usd(led.Cash), usd(committed), usd(free), usd(cost), req.Qty, req.Symbol, usd(price))
		}
		return fmt.Errorf("ledger cash %s < cost %s for %d %s at %s (rule: no overspend)",
			usd(led.Cash), usd(cost), req.Qty, req.Symbol, usd(price))
	}
	return nil
}

// estimatedCost prices an order the way the fill would be journalled: modeled
// price times quantity plus commission and fees, never the raw quote.
func (e *Engine) estimatedCost(side broker.Side, qty int, price float64) float64 {
	c := e.costs.Apply(side, qty, price)
	return float64(qty)*c.ModeledPrice + c.Commission + c.Fees
}

// entryPrice is the limit when one is set, otherwise the ask a market order would
// cross. check has already refused a non-positive limit.
func (e *Engine) entryPrice(ctx context.Context, req broker.OrderRequest) (float64, error) {
	if req.LimitPrice != nil {
		return *req.LimitPrice, nil
	}
	q, err := e.data.Quote(ctx, req.Symbol)
	if err != nil {
		return 0, fmt.Errorf("pricing %s to check the ledger: %w", req.Symbol, err)
	}
	switch {
	case q.Ask > 0:
		return q.Ask, nil
	case q.Last > 0:
		return q.Last, nil
	default:
		return 0, fmt.Errorf("no ask for %s, so the cost cannot be checked; pass a limit price (rule: no overspend)", req.Symbol)
	}
}

// usd renders money inside guardrail messages. User-facing tables format money
// through internal/cli/style.go; this exists so a refusal can quote its numbers.
func usd(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := strconv.FormatFloat(v, 'f', 2, 64)
	whole, frac, _ := strings.Cut(digits, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + "$" + b.String() + "." + frac
}
