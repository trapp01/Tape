package risk

import (
	"fmt"
	"math"
)

func size(equity float64, l Limits, entry, stop, target float64) (Plan, error) {
	if stop <= 0 {
		return Plan{}, fmt.Errorf("%w: entry %.2f", ErrNoStop, entry)
	}
	if stop >= entry {
		return Plan{}, fmt.Errorf("%w: stop %.2f is at or above entry %.2f", ErrStopSide, stop, entry)
	}
	if entry <= 0 {
		return Plan{}, fmt.Errorf("risk: entry must be positive, got %.2f", entry)
	}
	if equity <= 0 {
		return Plan{}, fmt.Errorf("risk: sizing %.2f: ledger equity must be positive, got %.2f", entry, equity)
	}

	perShare := entry - stop
	budget := equity * l.PerTradePct / 100
	qty := int(math.Floor(budget / perShare))
	if qty < 1 {
		return Plan{}, fmt.Errorf("%w: %.2f%% of $%.2f is $%.2f and one share risks $%.2f (entry %.2f, stop %.2f)",
			ErrUnsizeable, l.PerTradePct, equity, budget, perShare, entry, stop)
	}

	return plan(qty, perShare, entry, target), nil
}

// sizeWithin risk-sizes first, then trims the share count until the position
// costs no more than maxNotional. The risk budget bounds the loss; the cash
// ceiling bounds the bill, and a $5,000 ledger cannot buy $8,000 of stock.
func sizeWithin(equity, maxNotional float64, l Limits, entry, stop, target float64) (Plan, error) {
	p, err := size(equity, l, entry, stop, target)
	if err != nil || maxNotional <= 0 || p.Notional <= maxNotional {
		return p, err
	}

	qty := int(math.Floor(maxNotional / entry))
	if qty < 1 {
		return Plan{}, fmt.Errorf("%w: $%.2f of free cash buys no shares at %.2f", ErrUnsizeable, maxNotional, entry)
	}
	capped := plan(qty, entry-stop, entry, target)
	capped.CashCapped = true
	return capped, nil
}

func plan(qty int, perShare, entry, target float64) Plan {
	p := Plan{
		Qty:      qty,
		RiskUSD:  float64(qty) * perShare,
		Notional: float64(qty) * entry,
	}
	if target > 0 {
		p.RewardRisk = (target - entry) / perShare
	}
	return p
}
