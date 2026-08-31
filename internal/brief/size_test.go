package brief

import (
	"math"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/risk"
)

// deskLimits is the shipped default: 0.5% of the ledger per trade, 1.5R minimum,
// entries within 5% of the last price.
func deskLimits() risk.Limits {
	return risk.Limits{
		RequireStop:          true,
		PerTradePct:          0.5,
		MaxPositions:         3,
		MaxDailyLosses:       2,
		MinRewardRisk:        1.5,
		MaxEntryDeviationPct: 5,
	}
}

func withProposals(ps ...Proposal) Output {
	o := validOutput()
	o.Proposals = ps
	return o
}

// The desk example: $5,000 at 0.5% over a $1.50 stop is 16 shares of NVDA.
func TestSizeProposals(t *testing.T) {
	in := briefingData()
	got := SizeProposals(in, withProposals(proposal("NVDA")), 5000, 0, deskLimits())

	if len(got) != 1 {
		t.Fatalf("got %d sized proposals, want 1", len(got))
	}
	s := got[0]
	if !s.Sizeable() {
		t.Fatalf("the desk example was refused: %s", s.Reject)
	}
	if s.Symbol != "NVDA" || s.SetupID != "M2" {
		t.Errorf("the proposal did not survive sizing: %+v", s.Proposal)
	}
	if s.Plan.Qty != 16 {
		t.Errorf("qty = %d, want 16", s.Plan.Qty)
	}
	if math.Abs(s.Plan.RiskUSD-24) > 0.005 {
		t.Errorf("risk = %v, want 24", s.Plan.RiskUSD)
	}
	if math.Abs(s.Plan.Notional-2054.40) > 0.005 {
		t.Errorf("notional = %v, want 2054.40", s.Plan.Notional)
	}
	if math.Abs(s.Plan.RewardRisk-2) > 0.005 {
		t.Errorf("reward/risk = %v, want 2", s.Plan.RewardRisk)
	}
}

// Sizing knew the risk budget and nothing about the bill. A $512 share stopped
// $1.50 away sizes 16, which is $8,192 of stock against a $5,000 ledger — an
// idea printed as takeable that the overspend rule would refuse at the venue.
func TestSizeProposalsCapsOnFreeCash(t *testing.T) {
	in := briefingData()
	in.Indexes = append(in.Indexes, SymbolRead{Symbol: "SPY", Last: 512.00})
	in.Equity, in.FreeCash = 5000, 5000

	spy := proposal("SPY")
	spy.Entry, spy.Stop, spy.Target = 512.00, 510.50, 517.00

	got := SizeProposals(in, withProposals(spy), in.Equity, in.FreeCash, deskLimits())
	if len(got) != 1 || !got[0].Sizeable() {
		t.Fatalf("the idea was refused rather than trimmed: %+v", got)
	}
	// 2% of headroom off $5,000 leaves $4,900, which buys 9 shares at $512.
	if got[0].Plan.Qty != 9 {
		t.Fatalf("qty = %d, want the 9 shares the cash buys", got[0].Plan.Qty)
	}
	if !got[0].Plan.CashCapped {
		t.Fatal("a trimmed plan must say the cash ceiling set the size")
	}
	if math.Abs(got[0].Plan.Notional-4608) > 0.005 {
		t.Fatalf("notional = %v, want 4608", got[0].Plan.Notional)
	}
}

// An unknown free cash figure — a ledger the briefing could not read — leaves
// the risk size alone rather than refusing every idea.
func TestSizeProposalsWithoutAFreeCashFigure(t *testing.T) {
	in := briefingData()
	got := SizeProposals(in, withProposals(proposal("NVDA")), 5000, 0, deskLimits())
	if len(got) != 1 || got[0].Plan.Qty != 16 || got[0].Plan.CashCapped {
		t.Fatalf("sized = %+v, want the unchanged 16 shares", got)
	}
}

// A refused idea keeps its place in the list. It was still proposed, so it is
// still journaled, with the rule that refused it.
func TestSizeProposalsRefusals(t *testing.T) {
	in := briefingData()

	thin := proposal("NVDA")
	thin.Target = 129.40

	far := proposal("NVDA")
	far.Entry, far.Stop, far.Target = 200, 195, 215

	unquoted := proposal("MSFT")

	tests := []struct {
		name     string
		equity   float64
		proposal Proposal
		wantRule string
	}{
		{"a sizeable idea", 5000, proposal("NVDA"), ""},
		{"a ledger too small for one share", 100, proposal("NVDA"), "buys less than one share"},
		{"a target that does not pay for the stop", 5000, thin, "rule: reward/risk"},
		{"an entry nowhere near the last price", 5000, far, "rule: entry near last"},
		// MSFT carries no quote here, so there is nothing to be far from.
		{"a symbol with no quote skips the deviation rule", 5000, unquoted, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SizeProposals(in, withProposals(tc.proposal), tc.equity, 0, deskLimits())
			if len(got) != 1 {
				t.Fatalf("got %d sized proposals, want 1: a refusal is not a drop", len(got))
			}
			s := got[0]
			if s.Symbol != tc.proposal.Symbol {
				t.Errorf("symbol = %q, want %q", s.Symbol, tc.proposal.Symbol)
			}
			if tc.wantRule == "" {
				if !s.Sizeable() {
					t.Fatalf("refused a sizeable idea: %s", s.Reject)
				}
				return
			}
			if s.Sizeable() {
				t.Fatalf("sized an idea that breaks %q: %+v", tc.wantRule, s.Plan)
			}
			if !strings.Contains(s.Reject, tc.wantRule) {
				t.Errorf("rejection %q does not name %q", s.Reject, tc.wantRule)
			}
			if s.Plan != (risk.Plan{}) {
				t.Errorf("a refused idea carries a plan: %+v", s.Plan)
			}
		})
	}
}

// The whole slate is sized in one pass, and one refusal does not touch its
// neighbours.
func TestSizeProposalsSizesTheWholeSlate(t *testing.T) {
	in := briefingData()

	thin := proposal("AAPL")
	thin.Entry, thin.Stop, thin.Target = 225, 223, 226

	spy := proposal("SPY")
	spy.Entry, spy.Stop, spy.Target = 512, 509, 520

	got := SizeProposals(in, withProposals(proposal("NVDA"), thin, spy), 5000, 0, deskLimits())
	if len(got) != 3 {
		t.Fatalf("got %d sized proposals, want 3", len(got))
	}
	if !got[0].Sizeable() || got[0].Plan.Qty != 16 {
		t.Errorf("NVDA: %+v %s", got[0].Plan, got[0].Reject)
	}
	if got[1].Sizeable() {
		t.Errorf("AAPL is a 0.5R idea and must be refused: %+v", got[1].Plan)
	}
	if !got[2].Sizeable() || got[2].Plan.Qty != 8 {
		t.Errorf("SPY: %+v %s", got[2].Plan, got[2].Reject)
	}
}

func TestSizeProposalsOfAnEmptySlate(t *testing.T) {
	got := SizeProposals(briefingData(), withProposals(), 5000, 0, deskLimits())
	if len(got) != 0 {
		t.Errorf("got %d sized proposals from an empty slate", len(got))
	}
}
