package counterfactual

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// oneFilledBar reaches the entry and closes above it, so a case that expects an
// error fails loudly rather than quietly replaying nothing.
func oneFilledBar() []market.Bar {
	return []market.Bar{bar(0, 100.50, 100.60, 100.00, 100.20)}
}

func TestSimulateRejectsMalformedLevels(t *testing.T) {
	tests := []struct {
		name  string
		tweak func(*journal.Proposal)
		says  string
	}{
		{"stop level with the entry", func(p *journal.Proposal) { p.Stop = 100 }, "not below entry"},
		{"stop above the entry", func(p *journal.Proposal) { p.Stop = 101 }, "not below entry"},
		{"target level with the entry", func(p *journal.Proposal) { p.Target = 100 }, "not above entry"},
		{"target below the entry", func(p *journal.Proposal) { p.Target = 99.5 }, "not above entry"},
		{"no entry", func(p *journal.Proposal) { p.Entry = 0 }, "entry 0 is not a tradable price"},
		{"no stop", func(p *journal.Proposal) { p.Stop = 0 }, "stop 0 is not a tradable price"},
		{"no target", func(p *journal.Proposal) { p.Target = 0 }, "target 0 is not a tradable price"},
		{"negative entry", func(p *journal.Proposal) { p.Entry = -100 }, "not a tradable price"},
		{"entry is not a number", func(p *journal.Proposal) { p.Entry = math.NaN() }, "not a tradable price"},
		{"entry is infinite", func(p *journal.Proposal) { p.Entry = math.Inf(1) }, "not a tradable price"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := longProposal()
			tc.tweak(&p)

			got, err := Simulate(p, oneFilledBar(), costs.Default(), DefaultRules())
			if err == nil {
				t.Fatalf("expected an error, got outcome %+v", got)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("error %q does not say %q", err, tc.says)
			}
			for _, want := range []string{"7", "SPY"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
			if got != (journal.ProposalOutcome{}) {
				t.Errorf("a rejected proposal must not carry a partial outcome: %+v", got)
			}
		})
	}
}

func TestSimulateWithoutBars(t *testing.T) {
	_, err := Simulate(longProposal(), nil, costs.Default(), DefaultRules())
	if !errors.Is(err, ErrNoBars) {
		t.Fatalf("error = %v, want ErrNoBars", err)
	}
	for _, want := range []string{"SPY", "2026-08-28"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// The outcome is written straight to the journal, so it has to carry the row it
// belongs to. ScoredAt is the caller's stamp, not the replay's.
func TestSimulateCarriesTheProposalIdentity(t *testing.T) {
	got, err := Simulate(longProposal(), oneFilledBar(), costs.Default(), DefaultRules())
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if got.ProposalID != 7 || got.Mode != journal.ModePaper || got.Day != "2026-08-28" {
		t.Errorf("outcome identity = %d/%s/%s, want 7/paper/2026-08-28", got.ProposalID, got.Mode, got.Day)
	}
	if !got.ScoredAt.IsZero() {
		t.Errorf("ScoredAt = %s, want the zero time", got.ScoredAt)
	}
	if got.ID != 0 {
		t.Errorf("ID = %d, want 0; the store assigns it", got.ID)
	}
}

// A negative size is a bug upstream, not a short. It replays like no size at all.
func TestSimulateTreatsANegativeSizeAsNoShares(t *testing.T) {
	p := longProposal()
	p.TakenQty = qtyPtr(-5)

	got, err := Simulate(p, oneFilledBar(), costs.Default(), DefaultRules())
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if got.Qty != 0 || got.GrossPL != 0 || got.Costs != 0 || got.NetPL != 0 {
		t.Errorf("outcome = %+v, want no shares and no dollars", got)
	}
	if !got.Filled || got.ExitKind != journal.ExitClose {
		t.Errorf("the levels still replay: filled %v, exit %q", got.Filled, got.ExitKind)
	}
}
