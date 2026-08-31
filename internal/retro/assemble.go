package retro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/playbook"
	"github.com/trapp01/tape/internal/stats"
)

const (
	// topTrades is how many winners and losers the review looks at by name.
	topTrades = 3
	// allTimeYears is how far the gate's own report reaches back. The gate then
	// trims itself to the sessions since the rules last moved.
	allTimeYears = 20
)

// Assemble reads the record for the last Weeks of sessions ending today, plus the
// whole record for the gate. Two runs over one journal produce the same Input.
func Assemble(ctx context.Context, d Deps) (Input, error) {
	if d.Journal == nil {
		return Input{}, errors.New("retro: no journal configured")
	}
	loc := d.loc()
	weeks := max(d.Weeks, 1)
	to := startOfDay(d.now(), loc)
	from := to.AddDate(0, 0, -(weeks*7 - 1))
	fromDay, toDay := from.Format(dayLayout), to.Format(dayLayout)

	in := Input{
		GeneratedAt: d.now(),
		Timezone:    loc.String(),
		Mode:        d.Mode,
		FromDay:     fromDay,
		ToDay:       toDay,
		Weeks:       weeks,
		Playbook:    d.Playbook,
		SetupIDs:    playbook.SetupIDs(d.Playbook),
		Limits:      d.Limits,
	}

	var err error
	window := stats.Window{From: from, To: to, Loc: loc, Mode: d.Mode}
	if in.Report, err = stats.Compute(ctx, d.Journal, window, d.Gate); err != nil {
		return Input{}, err
	}
	// The gate reads its own window, which starts at the last playbook or config
	// change and can reach back further than the review does.
	whole := stats.Window{From: to.AddDate(-allTimeYears, 0, 0), To: to, Loc: loc, Mode: d.Mode}
	if in.Gate, err = stats.Compute(ctx, d.Journal, whole, d.Gate); err != nil {
		return Input{}, err
	}

	if in.Best, in.Worst, err = extremes(ctx, d, from, to, fromDay, toDay, loc); err != nil {
		return Input{}, err
	}
	if in.Passes, err = passes(ctx, d, fromDay, toDay); err != nil {
		return Input{}, err
	}
	if in.Refusals, err = refusals(ctx, d, fromDay, toDay); err != nil {
		return Input{}, err
	}
	if in.PreviousSummary, err = previousSummary(ctx, d); err != nil {
		return Input{}, err
	}
	return in, nil
}

// passes lists every vetoed idea with the replay that says what the veto cost or
// saved. An idea nothing has replayed yet is listed as unreplayed, not as zero.
func passes(ctx context.Context, d Deps, fromDay, toDay string) ([]PassLine, error) {
	ideas, err := d.Journal.ProposalsInRange(ctx, d.Mode, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("retro: proposals %s..%s: %w", fromDay, toDay, err)
	}
	outs, err := d.Journal.OutcomesInRange(ctx, d.Mode, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("retro: proposal outcomes %s..%s: %w", fromDay, toDay, err)
	}
	byProposal := make(map[int64]journal.ProposalOutcome, len(outs))
	for _, o := range outs {
		byProposal[o.ProposalID] = o
	}

	var lines []PassLine
	for _, p := range ideas {
		if p.Status != journal.ProposalPassed {
			continue
		}
		line := PassLine{Day: p.Day, Symbol: p.Symbol, SetupID: p.SetupID, Reason: p.Reason}
		if o, ok := byProposal[p.ID]; ok {
			line.Replayed = true
			line.Filled = o.Filled
			line.ExitKind = o.ExitKind
			line.Ambiguous = o.Ambiguous
			line.NetUSD = o.NetPL
			line.RMultiple = o.RMultiple
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// refusals counts the window's guardrail refusals by rule, most frequent first.
func refusals(ctx context.Context, d Deps, fromDay, toDay string) ([]RefusalLine, error) {
	rows, err := d.Journal.RefusalsInRange(ctx, d.Mode, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("retro: refusals %s..%s: %w", fromDay, toDay, err)
	}
	byRule := make(map[string]int, len(rows))
	for _, r := range rows {
		byRule[r.Rule]++
	}
	lines := make([]RefusalLine, 0, len(byRule))
	for rule, n := range byRule {
		lines = append(lines, RefusalLine{Rule: rule, Count: n})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Count != lines[j].Count {
			return lines[i].Count > lines[j].Count
		}
		return lines[i].Rule < lines[j].Rule
	})
	return lines, nil
}

// previousSummary is the last review's own summary. An archive that failed
// validation carries no summary and contributes nothing.
func previousSummary(ctx context.Context, d Deps) (string, error) {
	rows, err := d.Journal.ListRetros(ctx, d.Mode, 1)
	if err != nil {
		return "", fmt.Errorf("retro: previous review: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	var out Output
	if err := json.Unmarshal(rows[0].OutputJSON, &out); err != nil {
		return "", nil
	}
	return out.Summary, nil
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}
