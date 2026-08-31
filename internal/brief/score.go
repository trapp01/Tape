package brief

import (
	"fmt"
	"math"
)

func score(c Call, open, close, defaultThresholdPct float64) (Outcome, error) {
	if open <= 0 {
		return Outcome{}, fmt.Errorf("brief: scoring the %s call: open must be positive, got %v", c.Instrument, open)
	}
	if close <= 0 {
		return Outcome{}, fmt.Errorf("brief: scoring the %s call: close must be positive, got %v", c.Instrument, close)
	}

	threshold := defaultThresholdPct
	if c.ThresholdPct != nil {
		threshold = *c.ThresholdPct
	}
	// A threshold of zero makes an unchanged close both up and down, and never
	// flat, so the three directions stop being one question.
	if threshold <= 0 {
		return Outcome{}, fmt.Errorf("brief: scoring the %s call: threshold must be positive, got %v%%", c.Instrument, threshold)
	}

	actual := (close - open) / open * 100
	out := Outcome{Open: open, Close: close, ActualPct: actual, ThresholdPct: threshold}

	// A move that lands exactly on the threshold counts for the direction that
	// claimed it, so "up" and "flat" can never both be right.
	switch c.Direction {
	case DirUp:
		out.Correct = actual >= threshold
	case DirDown:
		out.Correct = actual <= -threshold
	case DirFlat:
		out.Correct = math.Abs(actual) < threshold
	default:
		return Outcome{}, fmt.Errorf("brief: scoring the %s call: direction %q is not %q, %q or %q", c.Instrument, c.Direction, DirUp, DirDown, DirFlat)
	}
	return out, nil
}
