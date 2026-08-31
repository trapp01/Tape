package counterfactual

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// throughCents is how far a bar must trade past the entry when the rules do not
// fill on touch.
const throughCents = 0.01

// simulate replays one long proposal bar by bar. The geometry it validates —
// stop below entry, target above — is what makes it long-only; a short's levels
// fail it.
func simulate(p journal.Proposal, bars []market.Bar, model costs.Model, rules Rules) (journal.ProposalOutcome, error) {
	if err := validate(p); err != nil {
		return journal.ProposalOutcome{}, err
	}
	if len(bars) == 0 {
		return journal.ProposalOutcome{}, fmt.Errorf("%w for %s on %s", ErrNoBars, p.Symbol, p.Day)
	}
	// The replay walks the path price took, so the order is the replay's to fix,
	// not the feed's to promise.
	bars = inTimeOrder(bars)

	out := journal.ProposalOutcome{
		ProposalID: p.ID,
		Mode:       p.Mode,
		Day:        p.Day,
		ExitKind:   journal.ExitNone,
		Qty:        replayQty(p),
	}

	i, fillPrice, filled := findFill(p.Entry, bars, rules.FillOnTouch)
	if !filled {
		return out, nil
	}
	out.Filled = true
	out.FillPrice = &fillPrice
	out.FilledAt = &bars[i].Time

	exit := findExit(p, bars, i, rules.StopFirstOnAmbiguousBar)
	out.ExitKind = exit.kind
	out.ExitPrice = &exit.price
	out.ExitAt = &exit.at
	out.Ambiguous = exit.ambiguous

	priceOutcome(&out, p, fillPrice, exit.price, model)
	return out, nil
}

// inTimeOrder copies the bars oldest first. The sort is stable, so two prints
// stamped the same minute keep the order the feed gave them.
func inTimeOrder(bars []market.Bar) []market.Bar {
	out := slices.Clone(bars)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// validate rejects levels that cannot describe a long trade. A NaN fails the
// positive test, which no comparison against zero would catch.
func validate(p journal.Proposal) error {
	for _, level := range []struct {
		name  string
		value float64
	}{{"entry", p.Entry}, {"stop", p.Stop}, {"target", p.Target}} {
		if !(level.value > 0) || math.IsInf(level.value, 0) {
			return fmt.Errorf("counterfactual: proposal %d (%s): %s %v is not a tradable price", p.ID, p.Symbol, level.name, level.value)
		}
	}
	if p.Stop >= p.Entry {
		return fmt.Errorf("counterfactual: proposal %d (%s): stop %.2f is not below entry %.2f", p.ID, p.Symbol, p.Stop, p.Entry)
	}
	if p.Target <= p.Entry {
		return fmt.Errorf("counterfactual: proposal %d (%s): target %.2f is not above entry %.2f", p.ID, p.Symbol, p.Target, p.Entry)
	}
	return nil
}

// replayQty is what the trader took when they took less than the slate offered.
// An unsizeable proposal replays at no shares: the levels still score, the
// dollars are zero.
func replayQty(p journal.Proposal) int {
	qty := p.Qty
	if p.TakenQty != nil {
		qty = *p.TakenQty
	}
	if qty < 1 {
		return 0
	}
	return qty
}

// findFill returns the index and price of the first bar that reaches the entry.
// A resting buy limit pays its own price, or the open when the bar gapped
// underneath it.
func findFill(entry float64, bars []market.Bar, onTouch bool) (int, float64, bool) {
	limit := entry
	if !onTouch {
		limit = entry - throughCents
	}
	for i, b := range bars {
		if b.Low <= limit {
			return i, math.Min(entry, b.Open), true
		}
	}
	return 0, 0, false
}

type exitPoint struct {
	kind      string
	price     float64
	at        time.Time
	ambiguous bool
}

// findExit walks from the fill bar to the bell. The fill bar can stop the trade
// out but cannot reach the target: the limit was not resting there when the
// minute's high printed. It is still a minute that spanned both, so it is still
// ambiguous.
func findExit(p journal.Proposal, bars []market.Bar, from int, stopFirst bool) exitPoint {
	for i := from; i < len(bars); i++ {
		b := bars[i]
		hitStop := b.Low <= p.Stop
		spanned := b.High >= p.Target
		hitTarget := i > from && spanned
		if !hitStop && !hitTarget {
			continue
		}
		// One minute cannot say which level printed first, so the rule decides and
		// the outcome records that it had to.
		ambiguous := hitStop && spanned
		// A stop gapped through fills at the open, not at the level nobody offered.
		stopped := exitPoint{kind: journal.ExitStop, price: math.Min(p.Stop, b.Open), at: b.Time, ambiguous: ambiguous}
		hit := exitPoint{kind: journal.ExitTarget, price: p.Target, at: b.Time, ambiguous: ambiguous}
		switch {
		case hitStop && hitTarget:
			if stopFirst {
				return stopped
			}
			return hit
		case hitStop:
			return stopped
		case hitTarget:
			return hit
		}
	}
	last := bars[len(bars)-1]
	return exitPoint{kind: journal.ExitClose, price: last.Close, at: last.Time}
}

// priceOutcome fills the money fields. Gross is the raw round trip; costs are the
// commissions, the regulatory fees, and the slippage the model puts between them,
// so NetPL matches what the same round trip books in the journal.
func priceOutcome(out *journal.ProposalOutcome, p journal.Proposal, fillRaw, exitRaw float64, model costs.Model) {
	shares := float64(out.Qty)
	entry := model.Apply(broker.Buy, out.Qty, fillRaw)
	exit := model.Apply(broker.Sell, out.Qty, exitRaw)

	slippage := shares * ((entry.ModeledPrice - fillRaw) + (exitRaw - exit.ModeledPrice))
	out.GrossPL = shares * (exitRaw - fillRaw)
	out.Costs = entry.Total() + exit.Total() + slippage
	out.NetPL = out.GrossPL - out.Costs
	// R is per share, so a proposal nobody could size still scores its own levels.
	out.RMultiple = (exitRaw - fillRaw) / (p.Entry - p.Stop)
}
