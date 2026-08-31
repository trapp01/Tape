package stats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// dayLayout matches the journal's day columns, so day strings compare and sort
// as text.
const dayLayout = "2006-01-02"

// firstDay reaches back past any record. The equity curve and the gate read the
// whole journal through the window's end; only the descriptive sections are cut
// down to the window.
const firstDay = "0001-01-01"

// records is everything one report reads, fetched once. proposalByOrder and
// proposalByID trace a closed trade back to the idea that opened it.
type records struct {
	trades    []journal.Trade
	fills     []journal.Fill
	ledger    journal.Ledger
	proposals []journal.Proposal
	outcomes  []journal.ProposalOutcome
	calls     []journal.Call
	notes     []journal.NoteScore
	refusals  []journal.Refusal
	briefings []journal.Briefing

	proposalByOrder map[int64]int64
	proposalByID    map[int64]journal.Proposal
	// reset is when the playbook last changed; nil when it never has.
	reset *time.Time
}

func compute(ctx context.Context, src Source, w Window, g Gate) (Report, error) {
	if w.Loc == nil {
		w.Loc = time.UTC
	}
	loc := w.Loc
	from := startOfDay(w.From, loc)
	to := startOfDay(w.To, loc).AddDate(0, 0, 1)
	fromDay := from.Format(dayLayout)
	toDay := startOfDay(w.To, loc).Format(dayLayout)

	all, err := fetch(ctx, src, w.Mode, to, toDay)
	if err != nil {
		return Report{}, err
	}
	win := all.since(from, fromDay)

	rep := Report{Window: w}
	rep.Sessions = len(sessionDays(win))
	rep.Trades = aggregate(win.trades).stats
	rep.BySetup = bySetup(win)
	rep.Calls = callStats(win.calls)
	rep.Notes = noteStats(win.notes)
	rep.Proposals = proposalStats(win)
	rep.Refusals = refusalStats(win.refusals, all.refusals, w)
	rep.Equity = equityStats(all.ledger.StartingEquity, all.trades)
	rep.Equity.WindowNetPL, rep.Equity.WindowReturnPct = windowNet(all.ledger.StartingEquity, all.trades, from)
	if rep.ByRegime, err = byRegime(win); err != nil {
		return Report{}, err
	}

	gv := newGateView(all, w, g)
	rep.GateWindowFrom = gv.from
	rep.GateResetAt = gv.resetAt
	rep.Significance = gv.significance
	rep.GateChecks = gv.checks(w, g)
	rep.GateOpen = allPassed(rep.GateChecks)
	return rep, nil
}

// fetch reads the whole record through the window's end. Time bounds are
// half-open [zero, to); day bounds are inclusive, matching the journal's queries.
func fetch(ctx context.Context, src Source, mode string, to time.Time, toDay string) (records, error) {
	var rec records
	var err error
	if rec.trades, err = src.ClosedTrades(ctx, time.Time{}, to, mode); err != nil {
		return rec, fmt.Errorf("stats: closed trades: %w", err)
	}
	if rec.fills, err = src.Fills(ctx, time.Time{}, to, mode); err != nil {
		return rec, fmt.Errorf("stats: fills: %w", err)
	}
	if rec.ledger, err = src.Ledger(ctx, mode); err != nil {
		return rec, fmt.Errorf("stats: ledger: %w", err)
	}
	if rec.proposals, err = src.ProposalsInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: proposals: %w", err)
	}
	if rec.outcomes, err = src.OutcomesInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: proposal outcomes: %w", err)
	}
	if rec.calls, err = src.CallsInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: calls: %w", err)
	}
	if rec.notes, err = src.NoteScoresInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: note scores: %w", err)
	}
	if rec.refusals, err = src.RefusalsInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: refusals: %w", err)
	}
	if rec.briefings, err = src.BriefingsInRange(ctx, mode, firstDay, toDay); err != nil {
		return rec, fmt.Errorf("stats: briefings: %w", err)
	}
	if err = trace(ctx, src, &rec); err != nil {
		return rec, err
	}
	if rec.reset, err = lastPlaybookChange(ctx, src); err != nil {
		return rec, err
	}
	return rec, nil
}

// lastPlaybookChange is when the rules the record was traded under last moved.
func lastPlaybookChange(ctx context.Context, src Source) (*time.Time, error) {
	v, err := src.LatestPlaybookVersion(ctx)
	if errors.Is(err, journal.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stats: latest playbook version: %w", err)
	}
	at := v.CreatedAt
	return &at, nil
}

// since narrows the whole record to the report window. The descriptive sections
// read this; the equity curve and the gate read the record it came from.
func (r records) since(from time.Time, fromDay string) records {
	out := r
	out.trades = keepAfter(r.trades, from, func(t journal.Trade) time.Time { return t.ClosedAt })
	out.fills = keepAfter(r.fills, from, func(f journal.Fill) time.Time { return f.FilledAt })
	out.proposals = keepFrom(r.proposals, fromDay, func(p journal.Proposal) string { return p.Day })
	out.outcomes = keepFrom(r.outcomes, fromDay, func(o journal.ProposalOutcome) string { return o.Day })
	out.calls = keepFrom(r.calls, fromDay, func(c journal.Call) string { return c.Day })
	out.notes = keepFrom(r.notes, fromDay, func(n journal.NoteScore) string { return n.Day })
	out.refusals = keepFrom(r.refusals, fromDay, func(rf journal.Refusal) string { return rf.Day })
	out.briefings = keepFrom(r.briefings, fromDay, func(b journal.Briefing) string { return b.Day })
	return out
}

func keepFrom[T any](rows []T, fromDay string, day func(T) string) []T {
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		if day(r) >= fromDay {
			out = append(out, r)
		}
	}
	return out
}

func keepAfter[T any](rows []T, from time.Time, at func(T) time.Time) []T {
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		if !at(r).Before(from) {
			out = append(out, r)
		}
	}
	return out
}

// sessionDays are the days the record shows work on: a slate, a call, or a fill.
// A fill's day is its Eastern session date, which is what the day columns hold.
func sessionDays(rec records) map[string]bool {
	days := make(map[string]bool)
	for _, p := range rec.proposals {
		days[p.Day] = true
	}
	for _, c := range rec.calls {
		days[c.Day] = true
	}
	for _, f := range rec.fills {
		days[market.SessionDate(f.FilledAt)] = true
	}
	return days
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

func allPassed(checks []GateCheck) bool {
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return len(checks) > 0
}
