package counterfactual

import (
	"math"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

const eps = 1e-9

// bar builds the minute bar opening n minutes after the 09:30 Eastern bell.
func bar(n int, open, high, low, close float64) market.Bar {
	return market.Bar{
		Time:   time.Date(2026, 8, 28, 9, 30+n, 0, 0, market.Eastern()),
		Open:   open,
		High:   high,
		Low:    low,
		Close:  close,
		Volume: 1000,
	}
}

// longProposal is 100 shares risking a dollar a share for two: entry 100, stop
// 99, target 102, so every R in the table reads straight off the exit price.
func longProposal() journal.Proposal {
	return journal.Proposal{
		ID:     7,
		Mode:   journal.ModePaper,
		Day:    "2026-08-28",
		Symbol: "SPY",
		Side:   "long",
		Entry:  100,
		Stop:   99,
		Target: 102,
		Qty:    100,
	}
}

func qtyPtr(v int) *int { return &v }

// want is the replay's whole result. Bar indexes stand in for timestamps so a
// case reads as "filled on bar 0, exited on bar 2".
type want struct {
	filled    bool
	fillPrice float64
	fillBar   int
	exitKind  string
	exitPrice float64
	exitBar   int
	ambiguous bool
	qty       int
	gross     float64
	costs     float64
	net       float64
	r         float64
}

func TestSimulate(t *testing.T) {
	touchOff := Rules{FillOnTouch: false, StopFirstOnAmbiguousBar: true}
	targetFirst := Rules{FillOnTouch: true, StopFirstOnAmbiguousBar: false}

	tests := []struct {
		name  string
		tweak func(*journal.Proposal)
		rules *Rules
		bars  []market.Bar
		want  want
	}{
		{
			name: "a session that never reaches the entry is not a trade",
			bars: []market.Bar{
				bar(0, 101.00, 101.50, 100.50, 101.00),
				bar(1, 101.00, 102.50, 100.80, 102.40),
			},
			// The target printed, but nothing was ever bought to sell into it.
			want: want{exitKind: journal.ExitNone, qty: 100},
		},
		{
			name: "fills, then a later bar takes the stop",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.20),
				bar(1, 100.10, 100.30, 98.80, 98.90),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitStop, exitPrice: 99, exitBar: 1,
				qty: 100, gross: -100, costs: 12.25, net: -112.25, r: -1,
			},
		},
		{
			name: "the fill bar can stop the trade out in its own minute",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 98.50, 98.70),
				bar(1, 98.70, 99.20, 98.60, 99.00),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitStop, exitPrice: 99, exitBar: 0,
				qty: 100, gross: -100, costs: 12.25, net: -112.25, r: -1,
			},
		},
		{
			name: "fills, then a later bar reaches the target",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.20),
				bar(1, 100.30, 102.40, 100.20, 102.20),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1,
				qty: 100, gross: 200, costs: 12.40, net: 187.60, r: 2,
			},
		},
		{
			name: "a bar spanning both levels stops out under the default rule",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.40),
				bar(1, 100.40, 102.50, 98.50, 101.00),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitStop, exitPrice: 99, exitBar: 1, ambiguous: true,
				qty: 100, gross: -100, costs: 12.25, net: -112.25, r: -1,
			},
		},
		{
			name:  "the same bar pays the target when the rule is flipped",
			rules: &targetFirst,
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.40),
				bar(1, 100.40, 102.50, 98.50, 101.00),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1, ambiguous: true,
				qty: 100, gross: 200, costs: 12.40, net: 187.60, r: 2,
			},
		},
		{
			name: "a bar opening under the stop exits at the open, not the level",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.20),
				bar(1, 98.00, 98.50, 97.50, 98.20),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitStop, exitPrice: 98, exitBar: 1,
				qty: 100, gross: -200, costs: 12.19, net: -212.19, r: -2,
			},
		},
		{
			name: "the fill bar's own high does not pay the target",
			bars: []market.Bar{
				bar(0, 100.50, 103.00, 100.00, 101.00),
				bar(1, 101.00, 101.20, 100.80, 101.10),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitClose, exitPrice: 101.10, exitBar: 1,
				qty: 100, gross: 110, costs: 12.355, net: 97.645, r: 1.1,
			},
		},
		{
			name: "neither level printed, so the trade is flat at the close",
			bars: []market.Bar{
				bar(0, 100.50, 100.70, 100.00, 100.30),
				bar(1, 100.30, 100.90, 99.50, 100.60),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitClose, exitPrice: 100.60, exitBar: 1,
				qty: 100, gross: 60, costs: 12.33, net: 47.67, r: 0.6,
			},
		},
		{
			name: "a dip through the limit still fills at the limit",
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 99.50, 100.20),
				bar(1, 100.20, 102.30, 100.10, 102.20),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1,
				qty: 100, gross: 200, costs: 12.40, net: 187.60, r: 2,
			},
		},
		{
			name: "a bar opening under the entry fills at the open",
			bars: []market.Bar{
				bar(0, 99.50, 99.80, 99.20, 99.60),
				bar(1, 99.60, 102.20, 99.50, 102.10),
			},
			want: want{
				filled: true, fillPrice: 99.50, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1,
				qty: 100, gross: 250, costs: 12.375, net: 237.625, r: 2.5,
			},
		},
		{
			name:  "without fill-on-touch the entry must trade a cent through",
			rules: &touchOff,
			bars: []market.Bar{
				bar(0, 100.40, 100.50, 100.00, 100.20),
				bar(1, 100.02, 100.30, 99.98, 100.10),
				bar(2, 100.10, 102.40, 100.00, 102.30),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 1,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 2,
				qty: 100, gross: 200, costs: 12.40, net: 187.60, r: 2,
			},
		},
		{
			name:  "a proposal nobody could size still scores its levels",
			tweak: func(p *journal.Proposal) { p.Qty = 0 },
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.20),
				bar(1, 100.30, 102.40, 100.20, 102.20),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1,
				qty: 0, r: 2,
			},
		},
		{
			name:  "the size the trader took beats the size the slate offered",
			tweak: func(p *journal.Proposal) { p.TakenQty = qtyPtr(10) },
			bars: []market.Bar{
				bar(0, 100.50, 100.60, 100.00, 100.20),
				bar(1, 100.30, 102.40, 100.20, 102.20),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 102, exitBar: 1,
				qty: 10, gross: 20, costs: 3.04, net: 16.96, r: 2,
			},
		},
		{
			name: "costs turn a full-R winner into a loss",
			tweak: func(p *journal.Proposal) {
				p.Stop, p.Target, p.Qty = 99.90, 100.10, 10
			},
			bars: []market.Bar{
				bar(0, 100.05, 100.06, 100.00, 100.02),
				bar(1, 100.05, 100.15, 100.02, 100.12),
			},
			want: want{
				filled: true, fillPrice: 100, fillBar: 0,
				exitKind: journal.ExitTarget, exitPrice: 100.10, exitBar: 1,
				qty: 10, gross: 1, costs: 3.0305, net: -2.0305, r: 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := longProposal()
			if tc.tweak != nil {
				tc.tweak(&p)
			}
			rules := DefaultRules()
			if tc.rules != nil {
				rules = *tc.rules
			}

			got, err := Simulate(p, tc.bars, costs.Default(), rules)
			if err != nil {
				t.Fatalf("Simulate: %v", err)
			}
			assertOutcome(t, got, tc.want, tc.bars)
		})
	}
}

func assertOutcome(t *testing.T, got journal.ProposalOutcome, w want, bars []market.Bar) {
	t.Helper()

	if got.Filled != w.filled {
		t.Errorf("Filled = %v, want %v", got.Filled, w.filled)
	}
	if got.ExitKind != w.exitKind {
		t.Errorf("ExitKind = %q, want %q", got.ExitKind, w.exitKind)
	}
	if got.Ambiguous != w.ambiguous {
		t.Errorf("Ambiguous = %v, want %v", got.Ambiguous, w.ambiguous)
	}
	if got.Qty != w.qty {
		t.Errorf("Qty = %d, want %d", got.Qty, w.qty)
	}

	if !w.filled {
		if got.FillPrice != nil || got.FilledAt != nil || got.ExitPrice != nil || got.ExitAt != nil {
			t.Errorf("an unfilled replay must leave every price and time nil: %+v", got)
		}
	} else {
		assertPrice(t, "FillPrice", got.FillPrice, w.fillPrice)
		assertTime(t, "FilledAt", got.FilledAt, bars[w.fillBar].Time)
		assertPrice(t, "ExitPrice", got.ExitPrice, w.exitPrice)
		assertTime(t, "ExitAt", got.ExitAt, bars[w.exitBar].Time)
	}

	for _, f := range []struct {
		name      string
		got, want float64
	}{
		{"GrossPL", got.GrossPL, w.gross},
		{"Costs", got.Costs, w.costs},
		{"NetPL", got.NetPL, w.net},
		{"RMultiple", got.RMultiple, w.r},
	} {
		if math.Abs(f.got-f.want) > eps {
			t.Errorf("%s = %v, want %v", f.name, f.got, f.want)
		}
	}
}

func assertPrice(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is nil, want %v", name, want)
	}
	if math.Abs(*got-want) > eps {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func assertTime(t *testing.T, name string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is nil, want %s", name, want)
	}
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}
