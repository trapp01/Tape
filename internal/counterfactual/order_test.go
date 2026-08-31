package counterfactual

import (
	"slices"
	"testing"

	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// walk is a session that fills on the first bar and reaches the target on the
// third, so any reordering of it changes the answer.
func walk() []market.Bar {
	return []market.Bar{
		bar(0, 100.50, 100.60, 99.90, 100.20),
		bar(1, 100.10, 100.30, 100.05, 100.20),
		bar(2, 100.20, 102.40, 100.10, 102.20),
	}
}

// The replay reads the path price took, so it puts the bars in time order
// itself rather than trusting whatever the feed handed back.
func TestReplayOrdersTheBarsItself(t *testing.T) {
	ordered, err := Simulate(longProposal(), walk(), costs.Default(), DefaultRules())
	if err != nil {
		t.Fatalf("Simulate ordered: %v", err)
	}

	shuffled := walk()
	slices.Reverse(shuffled)
	got, err := Simulate(longProposal(), shuffled, costs.Default(), DefaultRules())
	if err != nil {
		t.Fatalf("Simulate reversed: %v", err)
	}

	if got.ExitKind != ordered.ExitKind {
		t.Fatalf("reversed bars exited by %q, ordered by %q", got.ExitKind, ordered.ExitKind)
	}
	if got.NetPL != ordered.NetPL || got.RMultiple != ordered.RMultiple {
		t.Fatalf("reversed = %+v, ordered = %+v", got, ordered)
	}
	if got.ExitAt == nil || ordered.ExitAt == nil || !got.ExitAt.Equal(*ordered.ExitAt) {
		t.Fatalf("reversed exited at %v, ordered at %v", got.ExitAt, ordered.ExitAt)
	}
}

// One minute that opens above the entry, trades through the stop and prints the
// target cannot say which came first. The exit is the stop either way; what the
// record has to carry is that a convention chose it.
func TestAFillBarSpanningBothLevelsIsAmbiguous(t *testing.T) {
	bars := []market.Bar{
		bar(0, 100.50, 102.50, 98.50, 101.00),
		bar(1, 101.00, 101.20, 100.80, 101.10),
	}
	for _, rules := range []Rules{DefaultRules(), {FillOnTouch: true}} {
		got, err := Simulate(longProposal(), bars, costs.Default(), rules)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if !got.Ambiguous {
			t.Fatalf("rules %+v: the fill bar spanned entry, stop and target: %+v", rules, got)
		}
		if got.ExitKind != journal.ExitStop {
			t.Fatalf("rules %+v: exit = %q, want the stop the fill bar could reach", rules, got.ExitKind)
		}
	}
}
