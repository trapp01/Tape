package stats

import (
	"fmt"
	"sort"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
)

// noBriefingLabel holds the days that ran without an archived briefing, so a
// regime row never absorbs sessions whose conditions nobody recorded.
const noBriefingLabel = "no briefing"

type regimeBucket struct {
	sessions int
	calls    []journal.Call
	notes    []journal.NoteScore
	trades   []journal.Trade
	outcomes []journal.ProposalOutcome
}

// byRegime cuts the record by the market the briefing recorded that morning,
// which is the only regime label the archive can still prove.
func byRegime(rec records) ([]RegimeStats, error) {
	labels, err := regimeLabels(rec.briefings, rec.calls)
	if err != nil {
		return nil, err
	}
	label := func(day string) string {
		if l, ok := labels[day]; ok {
			return l
		}
		return noBriefingLabel
	}

	buckets := make(map[string]*regimeBucket)
	at := func(day string) *regimeBucket {
		l := label(day)
		b, ok := buckets[l]
		if !ok {
			b = &regimeBucket{}
			buckets[l] = b
		}
		return b
	}

	for day := range sessionDays(rec) {
		at(day).sessions++
	}
	for _, c := range rec.calls {
		b := at(c.Day)
		b.calls = append(b.calls, c)
	}
	for _, n := range rec.notes {
		b := at(n.Day)
		b.notes = append(b.notes, n)
	}
	for _, t := range rec.trades {
		b := at(rec.tradeDay(t))
		b.trades = append(b.trades, t)
	}
	for _, o := range rec.outcomes {
		b := at(o.Day)
		b.outcomes = append(b.outcomes, o)
	}

	rows := make([]RegimeStats, 0, len(buckets))
	for l, b := range buckets {
		rows = append(rows, RegimeStats{
			Label:          l,
			Sessions:       b.sessions,
			Calls:          callStats(b.calls),
			Notes:          noteStats(b.notes),
			Trades:         aggregate(b.trades).stats,
			Counterfactual: counterfactual(b.outcomes),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })
	return rows, nil
}

// regimeLabels reads the archived input of the briefing that stood for each day:
// the one the day's call is filed against, since the call locks at the open. A
// re-run day with no call falls back to the newest briefing.
func regimeLabels(briefings []journal.Briefing, calls []journal.Call) (map[string]string, error) {
	standing := make(map[string]int64, len(calls))
	for _, c := range calls {
		if _, ok := standing[c.Day]; !ok {
			standing[c.Day] = c.BriefingID
		}
	}
	latest := make(map[string]journal.Briefing, len(briefings))
	for _, b := range briefings {
		cur, ok := latest[b.Day]
		if ok && cur.ID == standing[b.Day] {
			continue
		}
		if b.ID == standing[b.Day] || !ok || b.GeneratedAt.After(cur.GeneratedAt) {
			latest[b.Day] = b
		}
	}
	out := make(map[string]string, len(latest))
	for day, b := range latest {
		in, err := brief.ArchivedInput(b)
		if err != nil {
			return nil, fmt.Errorf("stats: regime for %s: %w", day, err)
		}
		if in.Regime.Trend == "" && in.Regime.Vol == "" {
			continue
		}
		out[day] = fmt.Sprintf("%s, %s vol", in.Regime.Trend, in.Regime.Vol)
	}
	return out, nil
}
