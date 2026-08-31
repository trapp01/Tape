package brief

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
