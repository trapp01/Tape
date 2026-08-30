package costs

import (
	"math"

	"github.com/trapp01/tape/internal/broker"
)

// apply prices one fill: slippage moves the price against the trader, commission
// is squeezed between the minimum and the percent-of-value cap, and the
// regulatory fees fall on sells only.
func apply(m Model, side broker.Side, qty int, rawPrice float64) Result {
	if qty <= 0 {
		return Result{ModeledPrice: rawPrice}
	}
	// A non-positive or non-finite price is not a fill. Pricing one would hand back
	// a negative commission and credit the ledger, so it prices to nothing at all.
	if rawPrice <= 0 || math.IsNaN(rawPrice) || math.IsInf(rawPrice, 0) {
		return Result{}
	}
	shares := float64(qty)

	modeled := rawPrice
	if m.SlippageBps != 0 {
		frac := m.SlippageBps / 10_000
		if side == broker.Sell {
			modeled = rawPrice * (1 - frac)
		} else {
			modeled = rawPrice * (1 + frac)
		}
	}
	value := shares * modeled

	commission := shares * m.CommissionPerShare
	if commission < m.CommissionMin {
		commission = m.CommissionMin
	}
	// The percent cap outranks the minimum: one share of a $5 stock costs $0.05,
	// not the $1.00 floor. A zero cap means uncapped.
	if m.CommissionMaxPct > 0 {
		capped := math.Max(value*m.CommissionMaxPct/100, 0)
		if commission > capped {
			commission = capped
		}
	}

	var fees float64
	if side == broker.Sell {
		taf := shares * m.FINRATAFPerShare
		if m.FINRATAFMax > 0 {
			taf = math.Min(taf, m.FINRATAFMax)
		}
		// Each regulatory fee is billed to the cent on its own before they add up.
		fees = roundCents(value*m.SECFeePerDollar) + roundCents(taf)
	}

	return Result{
		ModeledPrice: modeled,
		Commission:   roundCents(commission),
		Fees:         roundCents(fees),
	}
}

// roundCents rounds half away from zero. Every value it sees is a non-negative
// cost, so that is half up.
func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}
