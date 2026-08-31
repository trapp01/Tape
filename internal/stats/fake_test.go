package stats

import (
	"context"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// fakeSource honours the same bounds the store does: half-open [from, to) on
// times, inclusive [fromDay, toDay] on day strings.
type fakeSource struct {
	trades    []journal.Trade
	fills     []journal.Fill
	ledger    journal.Ledger
	proposals []journal.Proposal
	outcomes  []journal.ProposalOutcome
	calls     []journal.Call
	notes     []journal.NoteScore
	refusals  []journal.Refusal
	briefings []journal.Briefing
	orders    map[int64]journal.Order
	playbook  *journal.PlaybookVersion
}

func (f *fakeSource) ClosedTrades(_ context.Context, from, to time.Time, _ string) ([]journal.Trade, error) {
	var out []journal.Trade
	for _, t := range f.trades {
		if !from.IsZero() && t.ClosedAt.Before(from) {
			continue
		}
		if !to.IsZero() && !t.ClosedAt.Before(to) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeSource) Fills(_ context.Context, from, to time.Time, _ string) ([]journal.Fill, error) {
	var out []journal.Fill
	for _, fl := range f.fills {
		if !from.IsZero() && fl.FilledAt.Before(from) {
			continue
		}
		if !to.IsZero() && !fl.FilledAt.Before(to) {
			continue
		}
		out = append(out, fl)
	}
	return out, nil
}

func (f *fakeSource) Ledger(context.Context, string) (journal.Ledger, error) { return f.ledger, nil }

func inDays(day, from, to string) bool { return day >= from && day <= to }

func (f *fakeSource) ProposalsInRange(_ context.Context, _, from, to string) ([]journal.Proposal, error) {
	var out []journal.Proposal
	for _, p := range f.proposals {
		if inDays(p.Day, from, to) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeSource) OutcomesInRange(_ context.Context, _, from, to string) ([]journal.ProposalOutcome, error) {
	var out []journal.ProposalOutcome
	for _, o := range f.outcomes {
		if inDays(o.Day, from, to) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *fakeSource) CallsInRange(_ context.Context, _, from, to string) ([]journal.Call, error) {
	var out []journal.Call
	for _, c := range f.calls {
		if inDays(c.Day, from, to) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeSource) NoteScoresInRange(_ context.Context, _, from, to string) ([]journal.NoteScore, error) {
	var out []journal.NoteScore
	for _, n := range f.notes {
		if inDays(n.Day, from, to) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (f *fakeSource) RefusalsInRange(_ context.Context, _, from, to string) ([]journal.Refusal, error) {
	var out []journal.Refusal
	for _, r := range f.refusals {
		if inDays(r.Day, from, to) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeSource) BriefingsInRange(_ context.Context, _, from, to string) ([]journal.Briefing, error) {
	var out []journal.Briefing
	for _, b := range f.briefings {
		if inDays(b.Day, from, to) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeSource) OrdersByIDs(_ context.Context, ids []int64) (map[int64]journal.Order, error) {
	out := make(map[int64]journal.Order)
	for _, id := range ids {
		if o, ok := f.orders[id]; ok {
			out[id] = o
		}
	}
	return out, nil
}

func (f *fakeSource) ProposalsByIDs(_ context.Context, ids []int64) (map[int64]journal.Proposal, error) {
	out := make(map[int64]journal.Proposal)
	for _, id := range ids {
		for _, p := range f.proposals {
			if p.ID == id {
				out[id] = p
			}
		}
	}
	return out, nil
}

func (f *fakeSource) LatestPlaybookVersion(context.Context) (journal.PlaybookVersion, error) {
	if f.playbook == nil {
		return journal.PlaybookVersion{}, journal.ErrNotFound
	}
	return *f.playbook, nil
}

// mt is the zone the trader's days are measured in.
var mt = mustLoad("America/Denver")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func day(s string) time.Time {
	t, err := time.ParseInLocation(dayLayout, s, mt)
	if err != nil {
		panic(err)
	}
	return t
}

// at builds a timestamp inside a session day.
func at(s string, hour int) time.Time { return day(s).Add(time.Duration(hour) * time.Hour) }

func testWindow(from, to string) Window {
	return Window{From: day(from), To: day(to), Loc: mt, Mode: journal.ModePaper}
}

func testGate() Gate {
	return Gate{
		MinMonths:            3,
		MinSessions:          50,
		MinTrades:            30,
		MinProfitFactor:      1.3,
		MaxDrawdownPct:       10,
		MinExpectancyUSD:     0,
		MaxRefusalsLastMonth: 0,
		MaxNullPassRate:      0.05,
	}
}

func run(t *testing.T, src *fakeSource, w Window, g Gate) Report {
	t.Helper()
	rep, err := Compute(context.Background(), src, w, g)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return rep
}

func checkByName(t *testing.T, rep Report, name string) GateCheck {
	t.Helper()
	for _, c := range rep.GateChecks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no gate check named %q; have %v", name, checkNames(rep))
	return GateCheck{}
}

func checkNames(rep Report) []string {
	names := make([]string, 0, len(rep.GateChecks))
	for _, c := range rep.GateChecks {
		names = append(names, c.Name)
	}
	return names
}

func near(got, want float64) bool {
	d := got - want
	return d < 1e-9 && d > -1e-9
}
