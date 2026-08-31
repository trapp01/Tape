package stats

import (
	"fmt"
	"math"
	"time"
)

// insufficient is the Actual a significance check reports when the record is too
// small to build a null from.
const insufficient = "insufficient trades"

// checks judges the gate window against every threshold. Nothing here unlocks
// anything: a report says where the record stands and the human decides.
func (v gateView) checks(w Window, g Gate) []GateCheck {
	s := v.agg.stats
	out := []GateCheck{
		v.monthsCheck(w, g),
		{
			Name:   "sessions",
			Passed: v.sessions >= g.MinSessions,
			Actual: fmt.Sprintf("%d", v.sessions),
			Needed: fmt.Sprintf("%d+", g.MinSessions),
		},
		{
			Name:   "trades",
			Passed: s.Count >= g.MinTrades,
			Actual: fmt.Sprintf("%d", s.Count),
			Needed: fmt.Sprintf("%d+", g.MinTrades),
		},
		{
			Name:   "expectancy",
			Passed: s.Count > 0 && s.ExpectancyUSD > g.MinExpectancyUSD,
			Actual: usd(s.ExpectancyUSD) + "/trade",
			Needed: "> " + usd(g.MinExpectancyUSD),
		},
		v.expectancyBoundCheck(),
		v.profitFactorCheck(g),
		{
			Name:   "max drawdown",
			Passed: v.equity.MaxDrawdownPct <= g.MaxDrawdownPct,
			Actual: fmt.Sprintf("%.1f%%", v.equity.MaxDrawdownPct),
			Needed: fmt.Sprintf("<= %.1f%%", g.MaxDrawdownPct),
		},
		v.nullPassRateCheck(g),
		{
			Name:   "refusals last month",
			Passed: v.refusals <= g.MaxRefusalsLastMonth,
			Actual: fmt.Sprintf("%d", v.refusals),
			Needed: fmt.Sprintf("<= %d", g.MaxRefusalsLastMonth),
		},
		{
			Name:   "setups identified",
			Passed: v.setups >= 1,
			Actual: fmt.Sprintf("%d", v.setups),
			Needed: fmt.Sprintf("1+ (%d trades, +E)", minSetupTrades),
		},
	}
	if v.resetAt != nil {
		// The playbook moved inside the window, so say which record was judged.
		since := " since " + v.from.Format(dayLayout)
		for i := range out {
			out[i].Actual += since
		}
	}
	return out
}

// monthsCheck wants both a long enough window and a record that actually starts
// that far back. Actual reports the binding one — the shorter of the two.
func (v gateView) monthsCheck(w Window, g Gate) GateCheck {
	span := months(v.from, w.To)
	record := 0.0
	if v.firstDay != "" {
		if first, err := time.ParseInLocation(dayLayout, v.firstDay, w.Loc); err == nil {
			record = months(first, w.To)
		}
	}
	binding := math.Min(span, record)
	return GateCheck{
		Name:   "months covered",
		Passed: v.sessions > 0 && binding >= float64(g.MinMonths),
		Actual: fmt.Sprintf("%.1f mo", binding),
		Needed: fmt.Sprintf("%d mo", g.MinMonths),
	}
}

// profitFactorCheck passes outright on a record with no losing trade: the ratio
// is unbounded, not small. ProfitFactor holds gross profit in that case.
func (v gateView) profitFactorCheck(g Gate) GateCheck {
	c := GateCheck{Name: "profit factor", Needed: fmt.Sprintf(">= %.2f", g.MinProfitFactor)}
	switch {
	case v.agg.sumLosses == 0 && v.agg.sumWins > 0:
		c.Passed, c.Actual = true, "no losses"
	case v.agg.stats.Count == 0:
		c.Actual = "0.00"
	default:
		c.Passed = v.agg.stats.ProfitFactor >= g.MinProfitFactor
		c.Actual = fmt.Sprintf("%.2f", v.agg.stats.ProfitFactor)
	}
	return c
}

func (v gateView) expectancyBoundCheck() GateCheck {
	c := GateCheck{Name: "expectancy lower bound", Needed: "> " + usd(0)}
	if v.significance.Paths == 0 {
		c.Actual = insufficient
		return c
	}
	c.Passed = v.significance.ExpectancyCI95Low > 0
	c.Actual = usd(v.significance.ExpectancyCI95Low) + " (95% low)"
	return c
}

func (v gateView) nullPassRateCheck(g Gate) GateCheck {
	c := GateCheck{Name: "null pass rate", Needed: fmt.Sprintf("<= %.1f%%", g.MaxNullPassRate*100)}
	if v.significance.Paths == 0 {
		c.Actual = insufficient
		return c
	}
	c.Passed = v.significance.NullPassRate <= g.MaxNullPassRate
	c.Actual = fmt.Sprintf("%.1f%%", v.significance.NullPassRate*100)
	return c
}

// usd formats a gate string's money. Report fields stay unrounded; only these
// short strings carry cents.
func usd(v float64) string {
	if v < 0 {
		return fmt.Sprintf("-$%.2f", -v)
	}
	return fmt.Sprintf("$%.2f", v)
}
