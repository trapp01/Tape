package trading

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
)

// F3: eod journalled the venue's quantity. With the broker holding 12 and the
// ledger holding 5, tape wrote `sell 12` and left the journal at AAPL -7.
func TestFlattenClosesTheLedgerQuantityNotTheVenues(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 5}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("buy: %v", err)
	}
	// Seven more shares reached the venue with no journal order behind them.
	fb.SetPosition("AAPL", 12, 100)

	rep, err := eng.Flatten(ctx)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if len(rep.Orders) != 1 || rep.Orders[0].Qty != 5 || rep.Orders[0].Side != string(broker.Sell) {
		t.Fatalf("flatten orders = %+v, want one sell of the 5 the ledger holds", rep.Orders)
	}
	if len(rep.Fills) != 1 || rep.Fills[0].Qty != 5 {
		t.Fatalf("flatten fills = %+v, want one of 5", rep.Fills)
	}

	positions, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("journal ended at %+v, want flat", positions)
	}

	var named bool
	for _, p := range rep.Problems {
		if strings.Contains(p, "venue holds 12") && strings.Contains(p, "ledger holds 5") && strings.Contains(p, "7 share divergence") {
			named = true
		}
	}
	if !named {
		t.Fatalf("problems must name the 7-share divergence, got %v", rep.Problems)
	}

	// The seven tape never bought stay at the venue for the human to reconcile.
	if len(rep.StillOpen) != 1 || rep.StillOpen[0].Qty != 7 {
		t.Fatalf("still open = %+v, want the 7 the ledger never held", rep.StillOpen)
	}
}

// A symbol the ledger holds and the venue does not is the same divergence read
// from the other side, and eod still has to say so.
func TestFlattenReportsAVenueThatHoldsNothing(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, _ := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 5}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("buy: %v", err)
	}
	fb.SetPosition("AAPL", 0, 0)

	rep, err := eng.Flatten(ctx)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}
	var named bool
	for _, p := range rep.Problems {
		if strings.Contains(p, "the ledger holds 5 but the venue holds none") {
			named = true
		}
	}
	if !named {
		t.Fatalf("problems must name the missing venue position, got %v", rep.Problems)
	}
}
