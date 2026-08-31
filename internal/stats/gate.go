package stats

import (
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// minSetupTrades is the sample a single playbook rule needs before its
// expectancy counts as identified rather than noticed.
const minSetupTrades = 10

// gateView is the slice of the record the gate judges: everything from the last
// playbook change onward, whatever window the report covers. Sessions traded
// under rules that have since moved are fitted, not evidence, so they describe
// the record without qualifying it.
type gateView struct {
	from    time.Time
	fromDay string
	resetAt *time.Time

	trades      []journal.Trade
	startEquity float64
	agg         tradeAgg
	equity      EquityStats

	sessions     int
	firstDay     string
	refusals     int
	setups       int
	significance Significance
}

func newGateView(rec records, w Window, g Gate) gateView {
	v := gateView{fromDay: firstDay, startEquity: rec.ledger.StartingEquity}
	if rec.reset != nil {
		at := rec.reset.In(w.Loc)
		v.from, v.resetAt = at, &at
		v.fromDay = startOfDay(v.from, w.Loc).Format(dayLayout)
	}
	days := sessionDays(rec)
	v.sessions, v.firstDay = gateSessions(days, v.fromDay)
	if v.sessions == len(days) {
		// Nothing was traded under the older rules, so the gate reads the whole
		// record and has no reset to name.
		v.resetAt, v.fromDay = nil, firstDay
		v.from = recordStart(v.firstDay, w)
	}

	for _, t := range rec.trades {
		if v.resetAt != nil && t.ClosedAt.Before(v.from) {
			// Equity carried into the gate window, so its drawdown is measured
			// against the account as it stood when the current rules began.
			v.startEquity += t.NetPL
			continue
		}
		v.trades = append(v.trades, t)
	}
	v.agg = aggregate(v.trades)
	v.equity = equityStats(v.startEquity, v.trades)
	v.refusals = gateRefusals(rec.refusals, w, v.fromDay)
	v.setups = carryingSetups(rec, v.trades)
	v.significance = significance(v.trades, v.startEquity, g)
	return v
}

// recordStart is where an unreset gate window opens: the record's first session,
// or today when the record has none.
func recordStart(firstSession string, w Window) time.Time {
	if firstSession != "" {
		if d, err := time.ParseInLocation(dayLayout, firstSession, w.Loc); err == nil {
			return d
		}
	}
	return startOfDay(w.To, w.Loc)
}

// gateSessions counts the days inside the gate window and names the earliest.
func gateSessions(days map[string]bool, fromDay string) (int, string) {
	n, first := 0, ""
	for day := range days {
		if day < fromDay {
			continue
		}
		n++
		if first == "" || day < first {
			first = day
		}
	}
	return n, first
}

// gateRefusals counts breaches in the final 30 days, never reaching back past
// the gate window's start.
func gateRefusals(refusals []journal.Refusal, w Window, fromDay string) int {
	since := lastMonthDay(w)
	if fromDay > since {
		since = fromDay
	}
	n := 0
	for _, r := range refusals {
		if r.Day >= since {
			n++
		}
	}
	return n
}

// carryingSetups counts the playbook rules with enough trades to have shown an
// edge and a positive one. Human trades are not a rule and never count.
func carryingSetups(rec records, trades []journal.Trade) int {
	bySetup := make(map[string][]journal.Trade)
	for _, t := range trades {
		id := rec.setupOf(t)
		if id == SetupHuman {
			continue
		}
		bySetup[id] = append(bySetup[id], t)
	}
	n := 0
	for _, ts := range bySetup {
		if len(ts) < minSetupTrades {
			continue
		}
		if aggregate(ts).stats.ExpectancyUSD > 0 {
			n++
		}
	}
	return n
}

// maxMonths bounds the calendar walk below, so a zero or absurd `from` cannot
// spin.
const maxMonths = 1200

// months is the span from `from` to `to` in calendar months: whole months by
// AddDate, plus the fraction of the month the remainder covers.
func months(from, to time.Time) float64 {
	if from.IsZero() || to.Before(from) {
		return 0
	}
	n := 0
	for n < maxMonths && !from.AddDate(0, n+1, 0).After(to) {
		n++
	}
	whole := from.AddDate(0, n, 0)
	next := from.AddDate(0, n+1, 0)
	span := next.Sub(whole).Seconds()
	if span <= 0 {
		return float64(n)
	}
	return float64(n) + to.Sub(whole).Seconds()/span
}
