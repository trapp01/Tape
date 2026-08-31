package risk

import (
	"fmt"
	"math"
)

// Check runs the limits a proposed entry must clear before it is sized: the
// target pays for the stop, and the entry sits near a price that exists. A last
// of zero skips the deviation rule, because nothing was quoted to compare with.
func Check(l Limits, entry, stop, target, last float64) error {
	if stop <= 0 {
		return fmt.Errorf("%w: entry %.2f (rule: stop required)", ErrNoStop, entry)
	}
	if stop >= entry {
		return fmt.Errorf("%w: stop %.2f is at or above entry %.2f (rule: stop below entry)", ErrStopSide, stop, entry)
	}
	if target <= entry {
		return fmt.Errorf("risk: target %.2f is not above entry %.2f (rule: target above entry)", target, entry)
	}

	rr := (target - entry) / (entry - stop)
	if rr < l.MinRewardRisk {
		return fmt.Errorf("risk: entry %.2f stop %.2f target %.2f is %.2fR, under the %.2fR minimum (rule: reward/risk)",
			entry, stop, target, rr, l.MinRewardRisk)
	}

	if last > 0 {
		deviation := math.Abs(entry-last) * 100 / last
		if deviation > l.MaxEntryDeviationPct {
			return fmt.Errorf("risk: entry %.2f is %.2f%% from the last price %.2f, over the %.2f%% limit (rule: entry near last)",
				entry, deviation, last, l.MaxEntryDeviationPct)
		}
	}
	return nil
}
