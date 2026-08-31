package brief

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/trapp01/tape/internal/playbook"
)

const (
	// MaxWatchNotes and MaxRisks bound the reply so a briefing fits one screen.
	MaxWatchNotes = 12
	MaxRisks      = 5
	// MaxThresholdPct is the widest bar a call may set for itself, in percent.
	MaxThresholdPct = 5.0
)

var biases = []string{"bullish", "bearish", "neutral"}

// Validate range-checks model output in Go. The schema is what the model was
// asked for; this is what the journal accepts.
func Validate(o Output) error {
	if strings.TrimSpace(o.MarketRead) == "" {
		return errors.New("brief: market_read is empty")
	}
	if err := validateCall(o.Call); err != nil {
		return err
	}
	if len(o.Watchlist) > MaxWatchNotes {
		return fmt.Errorf("brief: %d watchlist notes, at most %d allowed", len(o.Watchlist), MaxWatchNotes)
	}
	for i, w := range o.Watchlist {
		if strings.TrimSpace(w.Symbol) == "" {
			return fmt.Errorf("brief: watchlist note %d has no symbol", i)
		}
		if !slices.Contains(biases, w.Bias) {
			return fmt.Errorf("brief: watchlist note %d (%s): bias %q is not one of %s", i, w.Symbol, w.Bias, strings.Join(biases, ", "))
		}
	}
	if len(o.Risks) > MaxRisks {
		return fmt.Errorf("brief: %d risks, at most %d allowed", len(o.Risks), MaxRisks)
	}
	return validateProposals(o.Proposals)
}

// validateProposals is the shape of the trade ideas: long only, every price
// present and on its own side of the entry, one idea per symbol. What the ideas
// are allowed to name needs the briefing's data and is in ValidateAgainst.
func validateProposals(ps []Proposal) error {
	if len(ps) > MaxProposals {
		return fmt.Errorf("brief: %d proposals, at most %d allowed", len(ps), MaxProposals)
	}
	seen := make([]string, 0, len(ps))
	for i, p := range ps {
		if err := validateProposal(i, p); err != nil {
			return err
		}
		if slices.Contains(seen, p.Symbol) {
			return fmt.Errorf("brief: proposal %d repeats %s; one idea per symbol", i+1, p.Symbol)
		}
		seen = append(seen, p.Symbol)
	}
	return nil
}

func validateProposal(i int, p Proposal) error {
	n := i + 1
	if strings.TrimSpace(p.Symbol) == "" {
		return fmt.Errorf("brief: proposal %d has no symbol", n)
	}
	if p.Symbol != strings.ToUpper(p.Symbol) {
		return fmt.Errorf("brief: proposal %d: symbol %q is not uppercase", n, p.Symbol)
	}
	if p.Side != SideLong {
		return fmt.Errorf("brief: proposal %d (%s): side %q is not %q; shorting is not enabled", n, p.Symbol, p.Side, SideLong)
	}
	if strings.TrimSpace(p.SetupID) == "" {
		return fmt.Errorf("brief: proposal %d (%s) cites no setup id", n, p.Symbol)
	}
	if p.Entry <= 0 {
		return fmt.Errorf("brief: proposal %d (%s): entry must be positive, got %v", n, p.Symbol, p.Entry)
	}
	if p.Stop <= 0 {
		return fmt.Errorf("brief: proposal %d (%s): stop must be positive, got %v", n, p.Symbol, p.Stop)
	}
	if p.Target <= 0 {
		return fmt.Errorf("brief: proposal %d (%s): target must be positive, got %v", n, p.Symbol, p.Target)
	}
	if p.Stop >= p.Entry {
		return fmt.Errorf("brief: proposal %d (%s): stop %v is not below entry %v", n, p.Symbol, p.Stop, p.Entry)
	}
	if p.Target <= p.Entry {
		return fmt.Errorf("brief: proposal %d (%s): target %v is not above entry %v", n, p.Symbol, p.Target, p.Entry)
	}
	if strings.TrimSpace(p.Thesis) == "" {
		return fmt.Errorf("brief: proposal %d (%s) has no thesis", n, p.Symbol)
	}
	if strings.TrimSpace(p.Invalidation) == "" {
		return fmt.Errorf("brief: proposal %d (%s) has no invalidation", n, p.Symbol)
	}
	if !slices.Contains(confidences, p.Confidence) {
		return fmt.Errorf("brief: proposal %d (%s): confidence %q is not one of %s", n, p.Symbol, p.Confidence, confidenceList())
	}
	return nil
}

// ValidateAgainst is Validate plus the checks that need the briefing's own data.
// A call on a symbol the model was never shown cannot be graded against a
// session it was never about, and a call with no invalidation is not falsifiable.
func ValidateAgainst(o Output, in Input) error {
	if err := Validate(o); err != nil {
		return err
	}
	if strings.TrimSpace(o.Call.Rationale) == "" {
		return fmt.Errorf("brief: call %s has no rationale", o.Call.Instrument)
	}
	if strings.TrimSpace(o.Call.Invalidation) == "" {
		return fmt.Errorf("brief: call %s has no invalidation", o.Call.Instrument)
	}

	shown := shownSymbols(in)
	if !slices.Contains(shown, o.Call.Instrument) {
		return fmt.Errorf("brief: call instrument %s is not in the briefing's data (%s)",
			o.Call.Instrument, strings.Join(shown, ", "))
	}
	for i, w := range o.Watchlist {
		symbol := strings.ToUpper(strings.TrimSpace(w.Symbol))
		if !slices.Contains(shown, symbol) {
			return fmt.Errorf("brief: watchlist note %d names %s, which is not in the briefing's data (%s)",
				i, w.Symbol, strings.Join(shown, ", "))
		}
	}

	// N-rules are no-trade conditions: they are playbook headings, but a proposal
	// citing one argues for the trade its own rule forbids.
	setups := playbook.EntrySetupIDs(in.Playbook)
	known := strings.Join(setups, ", ")
	if known == "" {
		known = "the playbook defines none"
	}
	for i, p := range o.Proposals {
		if !slices.Contains(shown, p.Symbol) {
			return fmt.Errorf("brief: proposal %d names %s, which is not in the briefing's data (%s)",
				i+1, p.Symbol, strings.Join(shown, ", "))
		}
		if !slices.Contains(setups, p.SetupID) {
			return fmt.Errorf("brief: proposal %d (%s) cites setup %s, which is not a playbook setup (%s)",
				i+1, p.Symbol, p.SetupID, known)
		}
	}
	return nil
}

// shownSymbols is every symbol the briefing carried a quote for, indexes first.
func shownSymbols(in Input) []string {
	out := make([]string, 0, len(in.Indexes)+len(in.Watchlist))
	for _, r := range slices.Concat(in.Indexes, in.Watchlist) {
		symbol := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if symbol != "" && !slices.Contains(out, symbol) {
			out = append(out, symbol)
		}
	}
	return out
}

func validateCall(c Call) error {
	if strings.TrimSpace(c.Instrument) == "" {
		return errors.New("brief: call has no instrument")
	}
	if c.Instrument != strings.ToUpper(c.Instrument) {
		return fmt.Errorf("brief: call instrument %q is not uppercase", c.Instrument)
	}
	switch c.Direction {
	case DirUp, DirDown, DirFlat:
	default:
		return fmt.Errorf("brief: call %s: direction %q is not %q, %q or %q", c.Instrument, c.Direction, DirUp, DirDown, DirFlat)
	}
	// A threshold of zero makes an unchanged close both up and down, and never
	// flat. Null is the way to ask for the desk's default.
	if c.ThresholdPct != nil && (*c.ThresholdPct <= 0 || *c.ThresholdPct > MaxThresholdPct) {
		return fmt.Errorf("brief: call %s: threshold %v%% is outside 0 (exclusive) to %v%%", c.Instrument, *c.ThresholdPct, MaxThresholdPct)
	}
	return nil
}
