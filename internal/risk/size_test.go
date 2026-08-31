package risk

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// limits is the shipped default: 0.5% of the ledger per trade, 1.5R minimum,
// entries within 5% of the last price.
func limits() Limits {
	return Limits{
		RequireStop:                 true,
		PerTradePct:                 0.5,
		MaxPositions:                3,
		MaxDailyLosses:              2,
		NoEntriesBeforeCloseMinutes: 30,
		MinRewardRisk:               1.5,
		MaxEntryDeviationPct:        5,
	}
}

func near(got, want float64) bool { return math.Abs(got-want) < 0.005 }

func TestSize(t *testing.T) {
	tests := []struct {
		name                            string
		equity                          float64
		perTradePct                     float64
		entry, stop, target             float64
		wantQty                         int
		wantRisk, wantNotional, wantRWR float64
	}{
		// The desk's worked example: $25 of risk over a $1.50 stop buys 16 shares.
		{"the desk example", 5000, 0.5, 128.40, 126.90, 131.40, 16, 24, 2054.40, 2},
		{"no target leaves reward/risk at zero", 5000, 0.5, 128.40, 126.90, 0, 16, 24, 2054.40, 0},
		{"a wide stop buys fewer shares", 5000, 0.5, 100, 95, 110, 5, 25, 500, 2},
		{"a tight stop buys more", 10000, 1, 50, 49.50, 51.50, 200, 100, 10000, 3},
		// The budget rounds down: 25 / 2 is 12.5 shares, and half a share is not a share.
		{"the odd share is dropped", 5000, 0.5, 20, 18, 26, 12, 24, 240, 3},
		// One share exactly: the budget equals the stop distance.
		{"a budget of exactly one share", 5000, 0.5, 100, 75, 150, 1, 25, 100, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := limits()
			l.PerTradePct = tc.perTradePct
			got, err := Size(tc.equity, l, tc.entry, tc.stop, tc.target)
			if err != nil {
				t.Fatalf("Size: %v", err)
			}
			if got.Qty != tc.wantQty {
				t.Errorf("qty = %d, want %d", got.Qty, tc.wantQty)
			}
			if !near(got.RiskUSD, tc.wantRisk) {
				t.Errorf("risk = %v, want %v", got.RiskUSD, tc.wantRisk)
			}
			if !near(got.Notional, tc.wantNotional) {
				t.Errorf("notional = %v, want %v", got.Notional, tc.wantNotional)
			}
			if !near(got.RewardRisk, tc.wantRWR) {
				t.Errorf("reward/risk = %v, want %v", got.RewardRisk, tc.wantRWR)
			}
		})
	}
}

func TestSizeRefusals(t *testing.T) {
	tests := []struct {
		name                string
		equity              float64
		entry, stop, target float64
		want                error
		wantMentions        []string
	}{
		{"no stop", 5000, 128.40, 0, 131.40, ErrNoStop, []string{"128.40"}},
		{"a negative stop", 5000, 128.40, -1, 131.40, ErrNoStop, nil},
		{"a stop above the entry", 5000, 128.40, 129.00, 131.40, ErrStopSide, []string{"129.00", "128.40"}},
		{"a stop at the entry", 5000, 128.40, 128.40, 131.40, ErrStopSide, []string{"128.40"}},
		// $25 of budget cannot buy a share whose stop is $30 away.
		{"one share costs more than the budget", 5000, 500, 470, 560, ErrUnsizeable, []string{"25.00", "30.00", "500.00", "470.00"}},
		{"an empty ledger sizes nothing", 0, 100, 95, 110, nil, []string{"equity"}},
		{"a negative ledger sizes nothing", -100, 100, 95, 110, nil, []string{"equity"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Size(tc.equity, limits(), tc.entry, tc.stop, tc.target)
			if err == nil {
				t.Fatal("Size accepted the entry, want a refusal")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			for _, want := range tc.wantMentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// A 0.5% stop on a $512 share sizes 16 shares against a $5,000 ledger, which is
// $8,192 of stock the account cannot pay for. The cash ceiling is what stops the
// slate proposing a trade the overspend rule would refuse at the venue.
func TestSizeWithinCapsOnCash(t *testing.T) {
	// 2% under $5,000 of free cash is the $4,900 ceiling, and $4,900 / $512 is 9.
	got, err := SizeWithin(5000, 4900, limits(), 512.00, 510.50, 517.00)
	if err != nil {
		t.Fatalf("SizeWithin: %v", err)
	}
	if got.Qty != 9 {
		t.Fatalf("qty = %d, want 9", got.Qty)
	}
	if !got.CashCapped {
		t.Fatal("the plan must say the cash ceiling bit, not just quietly shrink")
	}
	if !near(got.Notional, 4608) || !near(got.RiskUSD, 13.50) {
		t.Fatalf("plan = %+v, want $4,608.00 of stock risking $13.50", got)
	}
}

// A cash ceiling above what the risk budget buys changes nothing, and the plan
// does not claim it was capped.
func TestSizeWithinLeavesTheRiskSizeAlone(t *testing.T) {
	got, err := SizeWithin(5000, 4900, limits(), 128.40, 126.90, 131.40)
	if err != nil {
		t.Fatalf("SizeWithin: %v", err)
	}
	if got.Qty != 16 || got.CashCapped {
		t.Fatalf("plan = %+v, want the unchanged 16 shares", got)
	}
}

// Cash that cannot buy one share is a refusal that names the cash, not a plan
// for zero shares.
func TestSizeWithinRefusesWhenCashBuysNothing(t *testing.T) {
	_, err := SizeWithin(5000, 400, limits(), 512.00, 510.50, 517.00)
	if !errors.Is(err, ErrUnsizeable) {
		t.Fatalf("error = %v, want ErrUnsizeable", err)
	}
	for _, want := range []string{"400.00", "512.00"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// A ceiling of zero or less is "no ceiling known", so the risk size stands.
func TestSizeWithinIgnoresAnUnknownCeiling(t *testing.T) {
	for _, ceiling := range []float64{0, -1} {
		got, err := SizeWithin(5000, ceiling, limits(), 128.40, 126.90, 131.40)
		if err != nil {
			t.Fatalf("SizeWithin(%v): %v", ceiling, err)
		}
		if got.Qty != 16 || got.CashCapped {
			t.Fatalf("SizeWithin(%v) = %+v, want the unchanged 16 shares", ceiling, got)
		}
	}
}

// The plan is what the trade risks, so the numbers have to agree with each other
// and stay inside the per-trade budget.
func TestSizeStaysInsideTheBudget(t *testing.T) {
	l := limits()
	for _, entry := range []float64{12.34, 55.10, 128.40, 401.75} {
		for _, stop := range []float64{0.51, 1.5, 3.25} {
			p, err := Size(5000, l, entry, entry-stop, entry+stop*2)
			if err != nil {
				continue
			}
			budget := 5000 * l.PerTradePct / 100
			if p.RiskUSD > budget+1e-9 {
				t.Errorf("entry %v stop %v: risk $%.2f is over the $%.2f budget", entry, stop, p.RiskUSD, budget)
			}
			if !near(p.RiskUSD, float64(p.Qty)*stop) {
				t.Errorf("entry %v stop %v: risk $%.2f is not qty %d x $%.2f", entry, stop, p.RiskUSD, p.Qty, stop)
			}
			if !near(p.Notional, float64(p.Qty)*entry) {
				t.Errorf("entry %v: notional $%.2f is not qty %d x $%.2f", entry, p.Notional, p.Qty, entry)
			}
			if !near(p.RewardRisk, 2) {
				t.Errorf("entry %v stop %v: reward/risk = %v, want 2", entry, stop, p.RewardRisk)
			}
		}
	}
}
