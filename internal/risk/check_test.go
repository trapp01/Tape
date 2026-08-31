package risk

import (
	"errors"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name                      string
		minRewardRisk             float64
		maxDeviationPct           float64
		entry, stop, target, last float64
		wantErr                   string
		wantIs                    error
	}{
		{"a clean setup passes", 1.5, 5, 100, 98, 104, 100.50, "", nil},
		// 3 / 2 is exactly the 1.5R minimum, and the minimum is allowed.
		{"reward/risk exactly at the minimum", 1.5, 5, 100, 98, 103, 100, "", nil},
		// 105 sits exactly 5% above 100, and the limit is allowed.
		{"deviation exactly at the limit", 1.5, 5, 105, 100, 115, 100, "", nil},
		{"no last price skips the deviation rule", 1.5, 5, 500, 490, 520, 0, "", nil},
		{"a negative last skips the deviation rule", 1.5, 5, 500, 490, 520, -1, "", nil},

		{"no stop", 1.5, 5, 100, 0, 104, 100, "stop required", ErrNoStop},
		{"a stop above the entry", 1.5, 5, 100, 101, 104, 100, "stop below entry", ErrStopSide},
		{"a stop at the entry", 1.5, 5, 100, 100, 104, 100, "stop below entry", ErrStopSide},
		{"a target below the entry", 1.5, 5, 100, 98, 99, 100, "target above entry", nil},
		{"a target at the entry", 1.5, 5, 100, 98, 100, 100, "target above entry", nil},
		{"reward/risk just under the minimum", 1.5, 5, 100, 98, 102.99, 100, "reward/risk", nil},
		{"reward/risk of one", 1.5, 5, 100, 98, 102, 100, "reward/risk", nil},
		{"an entry too far above the last", 1.5, 5, 106, 100, 120, 100, "entry near last", nil},
		{"an entry too far below the last", 1.5, 5, 94, 90, 106, 100, "entry near last", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := limits()
			l.MinRewardRisk = tc.minRewardRisk
			l.MaxEntryDeviationPct = tc.maxDeviationPct

			err := Check(l, tc.entry, tc.stop, tc.target, tc.last)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Check accepted the entry, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not name the rule %q", err, tc.wantErr)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("error = %v, want %v", err, tc.wantIs)
			}
		})
	}
}

// A refusal has to be readable next to the chart: it names the numbers that
// broke the rule, not just the rule.
func TestCheckRefusalsNameTheNumbers(t *testing.T) {
	l := limits()

	err := Check(l, 100, 98, 102, 100)
	if err == nil {
		t.Fatal("a 1R setup must be refused under a 1.5R minimum")
	}
	for _, want := range []string{"100.00", "98.00", "102.00", "1.00R", "1.50R"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("reward/risk refusal %q does not name %q", err, want)
		}
	}

	err = Check(l, 110, 105, 130, 100)
	if err == nil {
		t.Fatal("an entry 10% off the last must be refused under a 5% limit")
	}
	for _, want := range []string{"110.00", "10.00%", "100.00", "5.00%"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("deviation refusal %q does not name %q", err, want)
		}
	}
}
