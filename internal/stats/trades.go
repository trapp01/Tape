package stats

import (
	"sort"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// tradeAgg carries the win and loss sums beside the public stats, because the
// gate has to tell "no losses" apart from a small profit factor.
type tradeAgg struct {
	stats TradeStats
	// sumWins is positive, sumLosses negative.
	sumWins   float64
	sumLosses float64
}

// aggregate summarises closed trades. A trade that netted exactly zero is
// neither a win nor a loss, so it moves the count and nothing else.
//
// WinRate    = wins / count
// AvgWin     = Σ(net > 0) / wins, AvgLoss = Σ(net < 0) / losses (negative)
// Expectancy = Σnet / count
// ProfitFactor = Σwins / |Σlosses|, or Σwins when nothing lost.
func aggregate(trades []journal.Trade) tradeAgg {
	var agg tradeAgg
	s := &agg.stats
	s.Count = len(trades)
	for _, t := range trades {
		s.GrossPL += t.GrossPL
		s.Costs += t.Costs
		s.NetPL += t.NetPL
		switch {
		case t.NetPL > 0:
			s.Wins++
			agg.sumWins += t.NetPL
		case t.NetPL < 0:
			s.Losses++
			agg.sumLosses += t.NetPL
		}
	}
	if s.Count == 0 {
		return agg
	}
	s.WinRate = float64(s.Wins) / float64(s.Count)
	s.ExpectancyUSD = s.NetPL / float64(s.Count)
	if s.Wins > 0 {
		s.AvgWinUSD = agg.sumWins / float64(s.Wins)
	}
	if s.Losses > 0 {
		s.AvgLossUSD = agg.sumLosses / float64(s.Losses)
	}
	// With no losing trade the ratio has no divisor, so the field holds gross
	// profit instead. The gate reads sumLosses to tell the two cases apart.
	if agg.sumLosses == 0 {
		s.ProfitFactor = agg.sumWins
	} else {
		s.ProfitFactor = agg.sumWins / -agg.sumLosses
	}
	return agg
}

// equityStats walks the closed trades in close order from start. The peak begins
// at start, so a record that never recovers its first loss still shows a drawdown.
// The dollar and percent maxima are tracked separately: the deepest dollar fall
// and the deepest fall relative to its own peak need not be the same episode.
func equityStats(start float64, trades []journal.Trade) EquityStats {
	e := EquityStats{StartingEquity: start, EndingEquity: start}
	equity, peak := start, start
	for _, t := range byCloseTime(trades) {
		equity += t.NetPL
		if equity > peak {
			peak = equity
		}
		dd := peak - equity
		if dd > e.MaxDrawdownUSD {
			e.MaxDrawdownUSD = dd
		}
		if peak > 0 {
			if pct := dd / peak * 100; pct > e.MaxDrawdownPct {
				e.MaxDrawdownPct = pct
			}
		}
	}
	e.EndingEquity = equity
	if start != 0 {
		e.ReturnPct = (equity - start) / start * 100
	}
	return e
}

// windowNet is what the report window's own trades netted, and that net against
// the equity the window opened at. The account itself is the whole record's.
func windowNet(start float64, trades []journal.Trade, from time.Time) (float64, float64) {
	opening, net := start, 0.0
	for _, t := range byCloseTime(trades) {
		if t.ClosedAt.Before(from) {
			opening += t.NetPL
			continue
		}
		net += t.NetPL
	}
	if opening == 0 {
		return net, 0
	}
	return net, net / opening * 100
}

// byCloseTime copies and orders trades so the curve does not depend on the
// source's ordering.
func byCloseTime(trades []journal.Trade) []journal.Trade {
	out := make([]journal.Trade, len(trades))
	copy(out, trades)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ClosedAt.Equal(out[j].ClosedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].ClosedAt.Before(out[j].ClosedAt)
	})
	return out
}
