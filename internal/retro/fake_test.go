package retro

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
	"github.com/trapp01/tape/internal/stats"
)

// fakeStore is the journal in memory. It honours the same bounds the store does:
// half-open [from, to) on times, inclusive [fromDay, toDay] on day strings.
type fakeStore struct {
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

	versions []journal.PlaybookVersion
	retros   []journal.Retro
	diffs    map[int64][]journal.RetroDiff
	nextID   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		ledger: journal.Ledger{StartingEquity: 5000, Cash: 5000},
		orders: map[int64]journal.Order{},
		diffs:  map[int64][]journal.RetroDiff{},
	}
}

func (f *fakeStore) id() int64 { f.nextID++; return f.nextID }

func (f *fakeStore) ClosedTrades(_ context.Context, from, to time.Time, _ string) ([]journal.Trade, error) {
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

func (f *fakeStore) Fills(_ context.Context, from, to time.Time, _ string) ([]journal.Fill, error) {
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

func (f *fakeStore) Ledger(context.Context, string) (journal.Ledger, error) { return f.ledger, nil }

func inDays(day, from, to string) bool { return day >= from && day <= to }

func (f *fakeStore) ProposalsInRange(_ context.Context, _, from, to string) ([]journal.Proposal, error) {
	var out []journal.Proposal
	for _, p := range f.proposals {
		if inDays(p.Day, from, to) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) OutcomesInRange(_ context.Context, _, from, to string) ([]journal.ProposalOutcome, error) {
	var out []journal.ProposalOutcome
	for _, o := range f.outcomes {
		if inDays(o.Day, from, to) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *fakeStore) CallsInRange(_ context.Context, _, from, to string) ([]journal.Call, error) {
	var out []journal.Call
	for _, c := range f.calls {
		if inDays(c.Day, from, to) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) NoteScoresInRange(_ context.Context, _, from, to string) ([]journal.NoteScore, error) {
	var out []journal.NoteScore
	for _, n := range f.notes {
		if inDays(n.Day, from, to) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (f *fakeStore) RefusalsInRange(_ context.Context, _, from, to string) ([]journal.Refusal, error) {
	var out []journal.Refusal
	for _, r := range f.refusals {
		if inDays(r.Day, from, to) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) BriefingsInRange(_ context.Context, _, from, to string) ([]journal.Briefing, error) {
	var out []journal.Briefing
	for _, b := range f.briefings {
		if inDays(b.Day, from, to) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeStore) OrdersByIDs(_ context.Context, ids []int64) (map[int64]journal.Order, error) {
	out := map[int64]journal.Order{}
	for _, id := range ids {
		if o, ok := f.orders[id]; ok {
			out[id] = o
		}
	}
	return out, nil
}

func (f *fakeStore) ProposalsByIDs(_ context.Context, ids []int64) (map[int64]journal.Proposal, error) {
	out := map[int64]journal.Proposal{}
	for _, id := range ids {
		for _, p := range f.proposals {
			if p.ID == id {
				out[id] = p
			}
		}
	}
	return out, nil
}

func (f *fakeStore) LatestPlaybookVersion(context.Context) (journal.PlaybookVersion, error) {
	if len(f.versions) == 0 {
		return journal.PlaybookVersion{}, journal.ErrNotFound
	}
	return f.versions[len(f.versions)-1], nil
}

func (f *fakeStore) InsertPlaybookVersion(_ context.Context, v *journal.PlaybookVersion) error {
	if v.SHA256 == "" {
		return errors.New("sha256 is empty")
	}
	v.ID = f.id()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	f.versions = append(f.versions, *v)
	return nil
}

func (f *fakeStore) ListRetros(_ context.Context, _ string, limit int) ([]journal.Retro, error) {
	out := make([]journal.Retro, 0, len(f.retros))
	for i := len(f.retros) - 1; i >= 0; i-- {
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, f.retros[i])
	}
	return out, nil
}

func (f *fakeStore) InsertRetro(_ context.Context, r *journal.Retro, diffs []*journal.RetroDiff) error {
	r.ID = f.id()
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now().UTC()
	}
	f.retros = append(f.retros, *r)
	for i, d := range diffs {
		d.ID, d.RetroID, d.Index = f.id(), r.ID, i+1
		f.diffs[r.ID] = append(f.diffs[r.ID], *d)
	}
	return nil
}

func (f *fakeStore) RetroByID(_ context.Context, id int64) (journal.Retro, error) {
	for _, r := range f.retros {
		if r.ID == id {
			return r, nil
		}
	}
	return journal.Retro{}, journal.ErrNotFound
}

func (f *fakeStore) DiffsByRetro(_ context.Context, retroID int64) ([]journal.RetroDiff, error) {
	return f.diffs[retroID], nil
}

// ApplyRetroDiffs marks every diff or none, the way the store's transaction does.
func (f *fakeStore) ApplyRetroDiffs(ctx context.Context, v *journal.PlaybookVersion, diffIDs []int64, at time.Time) error {
	for _, id := range diffIDs {
		d, err := f.findDiff(id)
		if err != nil {
			return err
		}
		if d.AppliedAt != nil {
			return fmt.Errorf("diff %d is already applied", id)
		}
	}
	if err := f.InsertPlaybookVersion(ctx, v); err != nil {
		return err
	}
	for _, id := range diffIDs {
		d, _ := f.findDiff(id)
		d.AppliedAt, d.VersionID = &at, &v.ID
	}
	return nil
}

func (f *fakeStore) findDiff(diffID int64) (*journal.RetroDiff, error) {
	for id, rows := range f.diffs {
		for i := range rows {
			if rows[i].ID == diffID {
				return &f.diffs[id][i], nil
			}
		}
	}
	return nil, journal.ErrNotFound
}

// testPlaybook is small on purpose: the point is the shape of a section, not the
// volume of rules.
const testPlaybook = `# Playbook

## Posture by regime

Uptrend, low vol: continuations at normal size.

## Setups

### M1 gap-and-go continuation

Enter on the first pullback that holds the gap.

### N1 no-trade conditions

Stand down on an FOMC afternoon.

## Risk rules

Per trade 0.5% of equity, lost at the stop.
`

func testLimits() risk.Limits {
	return risk.Limits{
		RequireStop:                 true,
		PerTradePct:                 0.5,
		MaxPositions:                3,
		MaxDailyLosses:              2,
		NoEntriesBeforeCloseMinutes: 30,
		MinRewardRisk:               1.5,
		MaxEntryDeviationPct:        5,
	}
}

func testGate() stats.Gate {
	return stats.Gate{
		MinMonths: 3, MinSessions: 50, MinTrades: 100,
		MinProfitFactor: 1.3, MaxDrawdownPct: 10, MinExpectancyUSD: 0,
		MaxRefusalsLastMonth: 0, MaxNullPassRate: 0.10,
	}
}

func mountain(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	return loc
}

// testNow is the Friday every retro test reviews from.
func testNow(t *testing.T) time.Time {
	return time.Date(2026, 8, 28, 17, 0, 0, 0, mountain(t))
}

func testDeps(t *testing.T, st *fakeStore, path string) Deps {
	t.Helper()
	now := testNow(t)
	return Deps{
		Journal: st, Mode: journal.ModePaper, Loc: mountain(t),
		Now: func() time.Time { return now }, Weeks: 1,
		Gate: testGate(), Limits: testLimits(),
		Playbook: testPlaybook, PlaybookPath: path,
		Cfg: config.Default(),
	}
}
