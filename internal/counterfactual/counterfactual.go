// Package counterfactual replays a proposal against the session's minute bars:
// would the entry have filled, and would the stop, the target, or the close
// have ended it. The same replay grades a pass, an expiry, and a take.
package counterfactual

import (
	"errors"

	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

var ErrNoBars = errors.New("counterfactual: no session bars")

// Rules fix the ambiguities a bar cannot resolve. They are conservative on
// purpose: a bar that touches both the stop and the target counts as a stop.
type Rules struct {
	// FillOnTouch fills a long limit when a bar's low reaches the entry; when
	// false the low must trade through it by one cent.
	FillOnTouch bool
	// StopFirstOnAmbiguousBar decides a bar that spans both stop and target.
	StopFirstOnAmbiguousBar bool
}

func DefaultRules() Rules {
	return Rules{FillOnTouch: true, StopFirstOnAmbiguousBar: true}
}

// Simulate replays p over bars (regular hours, oldest first) with the cost
// model applied to the fill and the exit. Implemented in simulate.go.
func Simulate(p journal.Proposal, bars []market.Bar, model costs.Model, rules Rules) (journal.ProposalOutcome, error) {
	return simulate(p, bars, model, rules)
}
