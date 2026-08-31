// Package risk holds the position-sizing math and the limits every entry is
// checked against. Pure functions: the numbers come from config and the ledger,
// never from the model.
package risk

import "errors"

var (
	ErrNoStop     = errors.New("risk: entry has no stop")
	ErrStopSide   = errors.New("risk: stop is not below entry")
	ErrUnsizeable = errors.New("risk: risk budget buys less than one share")
)

// Limits is the [risk] section of config, resolved into the values the
// guardrails enforce. The json names are what an archived briefing carries, so
// an old proposal's sizing basis still reads back.
type Limits struct {
	// RequireStop refuses any entry submitted without a stop-loss.
	RequireStop bool `json:"require_stop"`
	// PerTradePct is the share of ledger equity one trade may lose at its stop.
	PerTradePct float64 `json:"per_trade_pct"`
	// MaxPositions bounds open positions plus pending entries.
	MaxPositions int `json:"max_positions"`
	// MaxDailyLosses halts new entries for the day once this many positions have
	// closed at a loss.
	MaxDailyLosses int `json:"max_daily_losses"`
	// NoEntriesBeforeCloseMinutes refuses new entries inside this window before
	// the session close, so flat-by-close is not a scramble.
	NoEntriesBeforeCloseMinutes int `json:"no_entries_before_close_minutes"`
	// MinRewardRisk is the smallest target distance, in multiples of the stop
	// distance, a proposal may carry.
	MinRewardRisk float64 `json:"min_reward_risk"`
	// MaxEntryDeviationPct bounds how far a proposed entry may sit from the last
	// price, so a stale or invented level is caught in Go.
	MaxEntryDeviationPct float64 `json:"max_entry_deviation_pct"`
}

// Plan is one sized entry: what the trade risks if the stop is hit.
type Plan struct {
	Qty      int
	RiskUSD  float64
	Notional float64
	// RewardRisk is (target − entry) / (entry − stop); zero when there is no target.
	RewardRisk float64
	// CashCapped is true when the cash ceiling, not the risk budget, set Qty.
	CashCapped bool
}

// Size turns a risk budget into a share count. Implemented in size.go.
func Size(equity float64, l Limits, entry, stop, target float64) (Plan, error) {
	return size(equity, l, entry, stop, target)
}

// SizeWithin is Size with a ceiling on what the position may cost: risk first,
// then cash. A maxNotional of zero or less means no ceiling is known.
// Implemented in size.go.
func SizeWithin(equity, maxNotional float64, l Limits, entry, stop, target float64) (Plan, error) {
	return sizeWithin(equity, maxNotional, l, entry, stop, target)
}
