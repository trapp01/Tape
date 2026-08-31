package stats

import (
	"math/rand/v2"
	"slices"

	"github.com/trapp01/tape/internal/journal"
)

const (
	// sigPaths is both the Monte Carlo path count and the bootstrap resample count.
	sigPaths = 10_000
	// ci95LowIndex is the 2.5th percentile of sigPaths sorted means, by nearest
	// rank: the 250th value, counting from zero.
	ci95LowIndex = sigPaths/40 - 1
	// The seeds are fixed so two runs over one record report the same numbers. The
	// bootstrap has its own pair: a gate threshold must not move a number that
	// reads only the trades.
	nullSeedA uint64 = 0x5461706553746174
	nullSeedB uint64 = 0x476174653a6d6972
	bootSeedA uint64 = 0x426f6f7473747261
	bootSeedB uint64 = 0x704578706563743a
	// sigMinSide is the wins and the losses a record needs before either side has
	// a shape to draw from.
	sigMinSide = 5
	// sigMinTrades is the sample below which a null says nothing at all.
	sigMinTrades = 20
)

// significance asks whether a zero-edge trader with this record's shape and
// sample size would have cleared the gate anyway.
//
//	NullWinRate       = |avgLoss| / (avgWin + |avgLoss|) — the break-even rate
//	NullPassRate      = share of sigPaths null records clearing MinProfitFactor
//	                    and MaxDrawdownPct
//	ExpectancyCI95Low = sorted bootstrap means at ci95LowIndex
//
// Each null trade wins at NullWinRate and then draws its size from the record's
// own wins or losses, so a profit factor resting on one outlier is tested against
// a null that can draw that outlier too. Under sigMinTrades trades, or with fewer
// than sigMinSide on either side, every field stays zero.
func significance(trades []journal.Trade, startEquity float64, g Gate) Significance {
	agg := aggregate(trades)
	wins, losses := magnitudes(trades)
	if agg.stats.Count < sigMinTrades || len(wins) < sigMinSide || len(losses) < sigMinSide {
		return Significance{}
	}
	avgWin := agg.stats.AvgWinUSD
	avgLoss := -agg.stats.AvgLossUSD
	if avgWin+avgLoss <= 0 {
		return Significance{}
	}

	sig := Significance{
		NullWinRate: avgLoss / (avgWin + avgLoss),
		Paths:       sigPaths,
	}
	rng := rand.New(rand.NewPCG(nullSeedA, nullSeedB))
	n := len(wins) + len(losses)
	passed := 0
	for range sigPaths {
		if nullPathClears(rng, n, sig.NullWinRate, wins, losses, startEquity, g) {
			passed++
		}
	}
	sig.NullPassRate = float64(passed) / sigPaths
	sig.ExpectancyCI95Low = bootstrapLow(rand.New(rand.NewPCG(bootSeedA, bootSeedB)), trades)
	return sig
}

// magnitudes splits the record into the sizes of its wins and of its losses,
// both positive. A trade that netted exactly zero belongs to neither.
func magnitudes(trades []journal.Trade) (wins, losses []float64) {
	for _, t := range trades {
		switch {
		case t.NetPL > 0:
			wins = append(wins, t.NetPL)
		case t.NetPL < 0:
			losses = append(losses, -t.NetPL)
		}
	}
	return wins, losses
}

// nullPathClears runs one zero-edge record of n trades and reports whether it
// would have satisfied the profit-factor and drawdown thresholds. Every trade's
// size is drawn from the record's own, so the null carries its dispersion.
func nullPathClears(rng *rand.Rand, n int, winRate float64, wins, losses []float64, start float64, g Gate) bool {
	equity, peak := start, start
	var won, lost float64
	for range n {
		if rng.Float64() < winRate {
			size := wins[rng.IntN(len(wins))]
			won += size
			equity += size
		} else {
			size := losses[rng.IntN(len(losses))]
			lost += size
			equity -= size
		}
		if equity > peak {
			peak = equity
		}
		if peak > 0 && (peak-equity)/peak*100 > g.MaxDrawdownPct {
			return false
		}
	}
	if lost == 0 {
		return won > 0
	}
	return won/lost >= g.MinProfitFactor
}

// bootstrapLow resamples the record's net P&L with replacement and returns the
// 2.5th percentile of the resampled means: the low end of a 95% interval on
// expectancy.
func bootstrapLow(rng *rand.Rand, trades []journal.Trade) float64 {
	n := len(trades)
	if n == 0 {
		return 0
	}
	means := make([]float64, sigPaths)
	for i := range means {
		var sum float64
		for range n {
			sum += trades[rng.IntN(n)].NetPL
		}
		means[i] = sum / float64(n)
	}
	slices.Sort(means)
	return means[ci95LowIndex]
}
