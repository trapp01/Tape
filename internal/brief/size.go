package brief

import (
	"slices"
	"strings"

	"github.com/trapp01/tape/internal/risk"
)

// Sized is one proposal with the numbers Go computed for it. Reject is empty
// when the idea can be traded; otherwise it names the rule that refused it and
// Plan is zero. A refused idea is still journaled: it was still proposed.
type Sized struct {
	Proposal
	Plan   risk.Plan
	Reject string
}

// Sizeable reports whether the idea cleared the limits.
func (s Sized) Sizeable() bool { return s.Reject == "" }

// cashHeadroom is the share of free cash a single idea may commit. The 2% that
// is left over absorbs slippage and the commission, which the entry price does
// not carry but the ledger pays.
const cashHeadroom = 0.98

// CashCeiling is the most one idea may commit out of free cash, which is what
// the slate was sized against.
func CashCeiling(freeCash float64) float64 {
	if freeCash <= 0 {
		return 0
	}
	return freeCash * cashHeadroom
}

// SizeProposals turns the model's ideas into share counts. The model's own
// numbers are only prices; the size, the risk, and the reward/risk ratio are
// computed here from the ledger and the limits. A freeCash of zero or less means
// the figure was unavailable, and only the risk budget bounds the size.
func SizeProposals(in Input, o Output, equity, freeCash float64, l risk.Limits) []Sized {
	last := lastPrices(in)
	maxNotional := CashCeiling(freeCash)
	out := make([]Sized, 0, len(o.Proposals))
	for _, p := range o.Proposals {
		s := Sized{Proposal: p}
		if err := risk.Check(l, p.Entry, p.Stop, p.Target, last[p.Symbol]); err != nil {
			s.Reject = err.Error()
			out = append(out, s)
			continue
		}
		plan, err := risk.SizeWithin(equity, maxNotional, l, p.Entry, p.Stop, p.Target)
		if err != nil {
			s.Reject = err.Error()
			out = append(out, s)
			continue
		}
		s.Plan = plan
		out = append(out, s)
	}
	return out
}

// lastPrices is every quote the briefing carried, keyed by symbol. A symbol with
// no quote maps to zero, which skips the entry-deviation rule.
func lastPrices(in Input) map[string]float64 {
	out := make(map[string]float64, len(in.Indexes)+len(in.Watchlist))
	for _, r := range slices.Concat(in.Indexes, in.Watchlist) {
		symbol := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if symbol != "" && r.Last > 0 {
			out[symbol] = r.Last
		}
	}
	return out
}
