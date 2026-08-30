// Package costs turns a frictionless paper fill into one a real broker would give.
// Paper venues fill at the quote with no commission; a small account trading
// through IBKR does not, and the gap is large enough to flip a strategy's sign.
package costs

import "github.com/trapp01/tape/internal/broker"

// Model holds per-fill friction. Defaults mirror IBKR Pro fixed pricing for US
// stocks plus the regulatory fees charged on sells.
type Model struct {
	// SlippageBps moves the fill against the trader by this many basis points.
	SlippageBps float64
	// CommissionPerShare, bounded below by CommissionMin and above by
	// CommissionMaxPct percent of trade value.
	CommissionPerShare float64
	CommissionMin      float64
	CommissionMaxPct   float64
	// SECFeePerDollar applies to sell proceeds only.
	SECFeePerDollar float64
	// FINRATAFPerShare applies to sells only, capped at FINRATAFMax per trade.
	FINRATAFPerShare float64
	FINRATAFMax      float64
}

type Result struct {
	ModeledPrice float64
	Commission   float64
	Fees         float64
}

// Total is everything the fill cost beyond the raw price times quantity.
func (r Result) Total() float64 {
	return r.Commission + r.Fees
}

// Default is IBKR Pro fixed pricing with the 2025 SEC and FINRA rates.
func Default() Model {
	return Model{
		SlippageBps:        5,
		CommissionPerShare: 0.005,
		CommissionMin:      1.00,
		CommissionMaxPct:   1.0,
		SECFeePerDollar:    27.80 / 1_000_000,
		FINRATAFPerShare:   0.000166,
		FINRATAFMax:        8.30,
	}
}

// Apply prices a fill of qty shares at rawPrice under this model.
// Implemented in apply.go.
func (m Model) Apply(side broker.Side, qty int, rawPrice float64) Result {
	return apply(m, side, qty, rawPrice)
}
