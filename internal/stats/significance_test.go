package stats

import (
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// heavyTailRecord is twenty trades whose profit factor rests on one outlier:
// seven $100 wins, one $1,500 win, and twelve $150 losses. Averaged, the null
// trader wins $275 every time it wins, which no trade in the record did. The
// ledger is large so the drawdown ceiling never decides a path; the profit
// factor does, which is where the dispersion shows.
func heavyTailRecord() *fakeSource {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 50000}}
	nets := []float64{100, 100, 100, 100, 100, 100, 100, 1500}
	for range 12 {
		nets = append(nets, -150)
	}
	closed := at("2026-08-03", 10)
	for i, net := range nets {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(i + 1), ClosedAt: closed, NetPL: net,
		})
		closed = closed.Add(time.Hour)
	}
	return src
}

func TestTheNullTraderDrawsTheRecordsOwnWinsAndLosses(t *testing.T) {
	rep := run(t, heavyTailRecord(), testWindow("2026-08-01", "2026-08-30"), testGate())

	if rep.Significance.Paths != sigPaths {
		t.Fatalf("paths = %d, want a null built from twenty trades", rep.Significance.Paths)
	}
	// Break-even stays the averaged rate: 150 / (275 + 150).
	if !near(rep.Significance.NullWinRate, 150.0/425.0) {
		t.Fatalf("null win rate = %v, want 150/425", rep.Significance.NullWinRate)
	}
	// A null that can draw the $1,500 outlier clears the thresholds far more often
	// than one that wins $275 like clockwork.
	if got := rep.Significance.NullPassRate; got < 0.28 || got > 0.36 {
		t.Fatalf("null pass rate = %.4f, want roughly 0.32 from bootstrapped magnitudes", got)
	}
}

func TestTheBootstrapDoesNotShareTheMonteCarloStream(t *testing.T) {
	src := qualifyingRecord()
	w := testWindow("2026-05-22", "2026-08-30")

	var bound float64
	var rates []float64
	for i, ceiling := range []float64{4, 6, 8, 10, 12, 20} {
		g := testGate()
		g.MaxDrawdownPct = ceiling
		sig := run(t, src, w, g).Significance
		rates = append(rates, sig.NullPassRate)
		if i == 0 {
			bound = sig.ExpectancyCI95Low
			continue
		}
		if sig.ExpectancyCI95Low != bound {
			t.Fatalf("max drawdown %v%% moved the expectancy bound to %v; at 4%% it was %v",
				ceiling, sig.ExpectancyCI95Low, bound)
		}
	}
	if rates[0] == rates[len(rates)-1] {
		t.Fatalf("the drawdown ceiling changed nothing in the null itself: %v", rates)
	}
}

func TestFewerThanTwentyTradesIsNoNullAtAll(t *testing.T) {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}}
	block := []float64{150, 150, -100, -100, 150, -100}
	closed := at("2026-08-03", 10)
	for i := range 18 {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(i + 1), ClosedAt: closed, NetPL: block[i%len(block)],
		})
		closed = closed.Add(time.Hour)
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if rep.Significance != (Significance{}) {
		t.Fatalf("eighteen trades cannot carry a null, got %+v", rep.Significance)
	}
	for _, name := range []string{"null pass rate", "expectancy lower bound"} {
		if c := checkByName(t, rep, name); c.Passed || c.Actual != insufficient {
			t.Fatalf("check %q = %+v, want a failed %q", name, c, insufficient)
		}
	}
}
