package stats

import (
	"context"
	"fmt"
	"sort"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// SetupHuman is the bucket for trades no proposal opened: a `tape buy` the
// trader placed themselves. It is a label, never a playbook rule.
const SetupHuman = "human"

// trace resolves every closed trade's entry order to the proposal behind it, so
// a trade can be grouped by the playbook rule that argued for it.
func trace(ctx context.Context, src Source, rec *records) error {
	rec.proposalByOrder = make(map[int64]int64)
	rec.proposalByID = make(map[int64]journal.Proposal, len(rec.proposals))
	for _, p := range rec.proposals {
		rec.proposalByID[p.ID] = p
	}

	seen := make(map[int64]bool, len(rec.trades))
	var orderIDs []int64
	for _, t := range rec.trades {
		if t.EntryOrderID != 0 && !seen[t.EntryOrderID] {
			seen[t.EntryOrderID] = true
			orderIDs = append(orderIDs, t.EntryOrderID)
		}
	}
	if len(orderIDs) == 0 {
		return nil
	}
	sort.Slice(orderIDs, func(i, j int) bool { return orderIDs[i] < orderIDs[j] })

	orders, err := src.OrdersByIDs(ctx, orderIDs)
	if err != nil {
		return fmt.Errorf("stats: entry orders: %w", err)
	}
	var missing []int64
	for id, o := range orders {
		if o.ProposalID == nil {
			continue
		}
		rec.proposalByOrder[id] = *o.ProposalID
		if _, ok := rec.proposalByID[*o.ProposalID]; !ok {
			missing = append(missing, *o.ProposalID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	// A trade closed inside the window can open on a proposal from before it.
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	extra, err := src.ProposalsByIDs(ctx, missing)
	if err != nil {
		return fmt.Errorf("stats: proposals behind entry orders: %w", err)
	}
	for id, p := range extra {
		rec.proposalByID[id] = p
	}
	return nil
}

// setupOf names the playbook rule a trade came from. An entry with no proposal
// behind it, or one whose proposal cites nothing, counts as human.
func (r records) setupOf(t journal.Trade) string {
	pid, ok := r.proposalByOrder[t.EntryOrderID]
	if !ok {
		return SetupHuman
	}
	p, ok := r.proposalByID[pid]
	if !ok || p.SetupID == "" {
		return SetupHuman
	}
	return p.SetupID
}

// tradeDay is the session a trade belongs to: the day of the proposal that
// opened it, or its entry's Eastern session date when a human placed it. Both
// are venue days, so a regime cut never splits on the trader's zone.
func (r records) tradeDay(t journal.Trade) string {
	if pid, ok := r.proposalByOrder[t.EntryOrderID]; ok {
		if p, ok := r.proposalByID[pid]; ok && p.Day != "" {
			return p.Day
		}
	}
	return market.SessionDate(t.OpenedAt)
}

// bySetup cuts the record by playbook rule: what each setup actually did, and
// what its replayed proposals say it would have done whether taken or not.
// A setup shows up when it traded or when a proposal citing it was scored.
func bySetup(rec records) []SetupStats {
	return setupRows(rec, rec.trades)
}

func setupRows(rec records, trades []journal.Trade) []SetupStats {
	byID := make(map[string][]journal.Trade)
	for _, t := range trades {
		id := rec.setupOf(t)
		byID[id] = append(byID[id], t)
	}
	outs := make(map[string][]journal.ProposalOutcome)
	for _, o := range rec.outcomes {
		p, ok := rec.proposalByID[o.ProposalID]
		if !ok || p.SetupID == "" {
			continue
		}
		outs[p.SetupID] = append(outs[p.SetupID], o)
	}

	ids := make([]string, 0, len(byID)+len(outs))
	for id := range byID {
		ids = append(ids, id)
	}
	for id := range outs {
		if _, ok := byID[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	rows := make([]SetupStats, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SetupStats{
			SetupID:        id,
			Trades:         aggregate(byID[id]).stats,
			Counterfactual: counterfactual(outs[id]),
		})
	}
	return rows
}

// counterfactual summarises replayed proposals. Wins and losses count only the
// replays that filled; AvgRMultiple averages over those same fills.
func counterfactual(outs []journal.ProposalOutcome) CounterfactualStats {
	var c CounterfactualStats
	var sumR float64
	for _, o := range outs {
		c.Replayed++
		if o.Ambiguous {
			c.Ambiguous++
		}
		if !o.Filled {
			continue
		}
		c.Filled++
		c.NetPL += o.NetPL
		sumR += o.RMultiple
		switch {
		case o.NetPL > 0:
			c.Wins++
		case o.NetPL < 0:
			c.Losses++
		}
	}
	if c.Filled > 0 {
		c.AvgRMultiple = sumR / float64(c.Filled)
	}
	return c
}
