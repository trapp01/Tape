package trading

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
)

func TestDailyHaltStopsNewEntries(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{MaxDailyLosses: 2})
	ctx := context.Background()

	for _, symbol := range []string{"AAPL", "MSFT"} {
		fb.SetPrice(symbol, 100)
		if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: symbol, Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
			t.Fatalf("buying %s: %v", symbol, err)
		}
		fb.SetPrice(symbol, 90)
		if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: symbol, Side: broker.Sell, Qty: 10}, journal.SourceHuman, ""); err != nil {
			t.Fatalf("stopping out of %s: %v", symbol, err)
		}
	}

	fb.SetPrice("NVDA", 50)
	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "NVDA", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleDailyHalt, "NVDA", "2 positions closed at a loss", "limit of 2")

	halted, why, err := eng.Halted(ctx)
	if err != nil || !halted || !strings.Contains(why, "limit of 2") {
		t.Fatalf("Halted = %v, %q, %v; want the day stopped with a reason", halted, why, err)
	}
}

// The halt counts losing positions, not losing exits. Scaling out of one winner
// in two clips books two round trips whose net is negative only because the $1
// commission floor lands twice, and that is not two stopped-out losses.
func TestDailyHaltIgnoresScaleOutsOfAWinner(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 20000, risk.Limits{MaxDailyLosses: 2})
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 30}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("opening buy: %v", err)
	}
	fb.SetPrice("AAPL", 100.20)
	for i := 0; i < 2; i++ {
		if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 10}, journal.SourceHuman, ""); err != nil {
			t.Fatalf("scale-out %d: %v", i+1, err)
		}
	}

	recap, err := eng.jnl.DayRecap(ctx, eng.Today(), eng.loc, journal.ModePaper)
	if err != nil {
		t.Fatalf("day recap: %v", err)
	}
	if recap.Losses != 2 {
		t.Fatalf("the day booked %d net losses; the test needs the two scratches it is about", recap.Losses)
	}

	halted, why, err := eng.Halted(ctx)
	if err != nil {
		t.Fatalf("Halted: %v", err)
	}
	if halted {
		t.Fatalf("two scale-outs of one winner halted the day: %s", why)
	}
	if _, err := eng.Submit(ctx, buyAt("MSFT", 1, 50), journal.SourceHuman, ""); err != nil {
		t.Fatalf("a day with no losing position must still take an entry: %v", err)
	}
}

// A position closed at a loss counts once, however many clips it took to exit.
func TestDailyHaltCountsOneLosingPositionOnce(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 20000, risk.Limits{MaxDailyLosses: 2})
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 20}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("opening buy: %v", err)
	}
	fb.SetPrice("AAPL", 90)
	for i := 0; i < 2; i++ {
		if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 10}, journal.SourceHuman, ""); err != nil {
			t.Fatalf("exit %d: %v", i+1, err)
		}
	}

	halted, why, err := eng.Halted(ctx)
	if err != nil {
		t.Fatalf("Halted: %v", err)
	}
	if halted {
		t.Fatalf("one losing position halted a two-loss day: %s", why)
	}
}
