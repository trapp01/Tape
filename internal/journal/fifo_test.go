package journal

import (
	"math"
	"testing"
	"time"
)

var base = time.Date(2026, 8, 24, 15, 30, 0, 0, time.UTC)

// fill builds a Fill at minute `min` past base with no costs unless given.
func fill(id int64, symbol, side string, qty int, price float64, min int, costs ...float64) Fill {
	f := Fill{
		ID:           id,
		OrderID:      id,
		Symbol:       symbol,
		Side:         side,
		Qty:          qty,
		RawPrice:     price,
		ModeledPrice: price,
		FilledAt:     base.Add(time.Duration(min) * time.Minute),
	}
	if len(costs) > 0 {
		f.Commission = costs[0]
	}
	if len(costs) > 1 {
		f.Fees = costs[1]
	}
	return f
}

func closeTo(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestMatchFIFOFullRoundTrip(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "buy", 10, 100, 0, 1.00),
		fill(2, "AAPL", "sell", 10, 110, 5, 1.00, 0.16),
	})

	if len(trades) != 1 {
		t.Fatalf("got %d trades, want 1", len(trades))
	}
	if len(open) != 0 {
		t.Fatalf("got %d open positions, want 0", len(open))
	}
	tr := trades[0]
	if tr.Qty != 10 {
		t.Errorf("Qty = %d, want 10", tr.Qty)
	}
	closeTo(t, "EntryAvgPrice", tr.EntryAvgPrice, 100)
	closeTo(t, "ExitAvgPrice", tr.ExitAvgPrice, 110)
	closeTo(t, "GrossPL", tr.GrossPL, 100)
	closeTo(t, "Costs", tr.Costs, 2.16)
	closeTo(t, "NetPL", tr.NetPL, 100-2.16)
	if !tr.OpenedAt.Equal(base) {
		t.Errorf("OpenedAt = %v, want %v", tr.OpenedAt, base)
	}
	if !tr.ClosedAt.Equal(base.Add(5 * time.Minute)) {
		t.Errorf("ClosedAt = %v, want %v", tr.ClosedAt, base.Add(5*time.Minute))
	}
}

func TestMatchFIFOPartialExitsProrateCosts(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "buy", 10, 100, 0, 1.00),
		fill(2, "AAPL", "sell", 4, 110, 5, 1.00),
		fill(3, "AAPL", "sell", 6, 120, 9, 1.00),
	})

	if len(trades) != 2 {
		t.Fatalf("got %d trades, want 2", len(trades))
	}
	if len(open) != 0 {
		t.Fatalf("got %d open positions, want 0", len(open))
	}

	first := trades[0]
	if first.Qty != 4 {
		t.Errorf("first Qty = %d, want 4", first.Qty)
	}
	closeTo(t, "first GrossPL", first.GrossPL, 40)
	// 4/10 of the $1.00 entry plus all of the $1.00 exit.
	closeTo(t, "first Costs", first.Costs, 0.4+1.00)
	closeTo(t, "first NetPL", first.NetPL, 40-1.40)

	second := trades[1]
	if second.Qty != 6 {
		t.Errorf("second Qty = %d, want 6", second.Qty)
	}
	closeTo(t, "second GrossPL", second.GrossPL, 120)
	closeTo(t, "second Costs", second.Costs, 0.6+1.00)

	// Every cent of both fills' costs ends up allocated exactly once.
	closeTo(t, "allocated costs", first.Costs+second.Costs, 3.00)
}

func TestMatchFIFOLeavesRemainderOpen(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "buy", 10, 100, 0, 1.00),
		fill(2, "AAPL", "sell", 4, 110, 5, 1.00),
	})

	if len(trades) != 1 {
		t.Fatalf("got %d trades, want 1", len(trades))
	}
	if len(open) != 1 {
		t.Fatalf("got %d open positions, want 1", len(open))
	}
	pos := open[0]
	if pos.Symbol != "AAPL" || pos.Qty != 6 {
		t.Fatalf("open position = %+v, want AAPL qty 6", pos)
	}
	closeTo(t, "AvgEntryPrice", pos.AvgEntryPrice, 100)
	closeTo(t, "CostBasis", pos.CostBasis, 600)
	if !pos.OpenedAt.Equal(base) {
		t.Errorf("OpenedAt = %v, want %v", pos.OpenedAt, base)
	}
}

func TestMatchFIFOConsumesOldestLotFirst(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "buy", 10, 100, 0),
		fill(2, "AAPL", "buy", 10, 120, 1),
		fill(3, "AAPL", "sell", 15, 130, 2),
	})

	if len(trades) != 1 {
		t.Fatalf("got %d trades, want 1", len(trades))
	}
	tr := trades[0]
	if tr.Qty != 15 {
		t.Errorf("Qty = %d, want 15", tr.Qty)
	}
	// 10 shares from the $100 lot and 5 from the $120 lot.
	closeTo(t, "EntryAvgPrice", tr.EntryAvgPrice, (10*100+5*120)/15.0)
	closeTo(t, "GrossPL", tr.GrossPL, 15*130-(10*100+5*120))

	if len(open) != 1 || open[0].Qty != 5 {
		t.Fatalf("open = %+v, want one position of 5", open)
	}
	closeTo(t, "remaining AvgEntryPrice", open[0].AvgEntryPrice, 120)
}

func TestMatchFIFOKeepsSymbolsApart(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "buy", 10, 100, 0),
		fill(2, "MSFT", "buy", 5, 400, 1),
		fill(3, "AAPL", "sell", 10, 105, 2),
	})

	if len(trades) != 1 || trades[0].Symbol != "AAPL" {
		t.Fatalf("trades = %+v, want one AAPL trade", trades)
	}
	if len(open) != 1 || open[0].Symbol != "MSFT" || open[0].Qty != 5 {
		t.Fatalf("open = %+v, want MSFT qty 5", open)
	}
}

func TestMatchFIFOShortRoundTrip(t *testing.T) {
	trades, open := matchFIFO([]Fill{
		fill(1, "AAPL", "sell", 10, 110, 0),
		fill(2, "AAPL", "buy", 10, 100, 5),
	})

	if len(trades) != 1 {
		t.Fatalf("got %d trades, want 1", len(trades))
	}
	if len(open) != 0 {
		t.Fatalf("got %d open positions, want 0", len(open))
	}
	// Sold high, bought back low: the short earns the difference.
	closeTo(t, "GrossPL", trades[0].GrossPL, 100)
	closeTo(t, "EntryAvgPrice", trades[0].EntryAvgPrice, 110)
	closeTo(t, "ExitAvgPrice", trades[0].ExitAvgPrice, 100)
}

func TestMatchFIFOSortsUnorderedFillsWithoutMutating(t *testing.T) {
	in := []Fill{
		fill(2, "AAPL", "sell", 10, 110, 5),
		fill(1, "AAPL", "buy", 10, 100, 0),
	}
	trades, open := matchFIFO(in)

	if len(trades) != 1 || len(open) != 0 {
		t.Fatalf("trades = %+v, open = %+v", trades, open)
	}
	closeTo(t, "GrossPL", trades[0].GrossPL, 100)
	if in[0].ID != 2 {
		t.Errorf("matchFIFO reordered the caller's slice")
	}
}

func TestMatchFIFOEmpty(t *testing.T) {
	trades, open := matchFIFO(nil)
	if len(trades) != 0 || len(open) != 0 {
		t.Fatalf("trades = %+v, open = %+v, want both empty", trades, open)
	}
}
