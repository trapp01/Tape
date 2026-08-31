package retro

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/stats"
)

// extremes names the biggest winners and losers of the window. Only losers reach
// the worst list and only winners the best one, so a short week never reads as
// more trades than it holds.
func extremes(ctx context.Context, d Deps, from, to time.Time, fromDay, toDay string, loc *time.Location) (best, worst []TradeLine, err error) {
	trades, err := d.Journal.ClosedTrades(ctx, from, to.AddDate(0, 0, 1), d.Mode)
	if err != nil {
		return nil, nil, fmt.Errorf("retro: closed trades %s..%s: %w", fromDay, toDay, err)
	}
	if len(trades) == 0 {
		return nil, nil, nil
	}
	ideas, err := d.Journal.ProposalsInRange(ctx, d.Mode, fromDay, toDay)
	if err != nil {
		return nil, nil, fmt.Errorf("retro: proposals %s..%s: %w", fromDay, toDay, err)
	}
	byID := make(map[int64]journal.Proposal, len(ideas))
	for _, p := range ideas {
		byID[p.ID] = p
	}
	byOrder, err := entryProposals(ctx, d.Journal, trades, byID)
	if err != nil {
		return nil, nil, err
	}

	ordered := slices.Clone(trades)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].NetPL != ordered[j].NetPL {
			return ordered[i].NetPL < ordered[j].NetPL
		}
		return ordered[i].ID < ordered[j].ID
	})
	for i := 0; i < len(ordered) && len(worst) < topTrades; i++ {
		if ordered[i].NetPL >= 0 {
			break
		}
		worst = append(worst, tradeLine(ordered[i], byOrder, loc))
	}
	for i := len(ordered) - 1; i >= 0 && len(best) < topTrades; i-- {
		if ordered[i].NetPL <= 0 {
			break
		}
		best = append(best, tradeLine(ordered[i], byOrder, loc))
	}
	return best, worst, nil
}

// tradeLine names a trade by the playbook rule that argued for it and measures it
// in R against the risk that rule planned.
func tradeLine(t journal.Trade, byOrder map[int64]journal.Proposal, loc *time.Location) TradeLine {
	line := TradeLine{
		Day:     t.OpenedAt.In(loc).Format(dayLayout),
		Symbol:  t.Symbol,
		SetupID: stats.SetupHuman,
		NetUSD:  t.NetPL,
	}
	p, ok := byOrder[t.EntryOrderID]
	if !ok {
		return line
	}
	if p.SetupID != "" {
		line.SetupID = p.SetupID
	}
	if p.Day != "" {
		line.Day = p.Day
	}
	if planned := p.Entry - p.Stop; planned > 0 {
		line.RMultiple = (t.ExitAvgPrice - t.EntryAvgPrice) / planned
	}
	return line
}

// entryProposals resolves each closed trade's entry order to the idea behind it.
// A trade closed inside the window can open on a proposal from before it.
func entryProposals(ctx context.Context, jnl Store, trades []journal.Trade, known map[int64]journal.Proposal) (map[int64]journal.Proposal, error) {
	var ids []int64
	seen := make(map[int64]bool, len(trades))
	for _, t := range trades {
		if t.EntryOrderID != 0 && !seen[t.EntryOrderID] {
			seen[t.EntryOrderID] = true
			ids = append(ids, t.EntryOrderID)
		}
	}
	out := make(map[int64]journal.Proposal, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	slices.Sort(ids)
	orders, err := jnl.OrdersByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("retro: entry orders: %w", err)
	}

	link := make(map[int64]int64, len(orders))
	var missing []int64
	for id, o := range orders {
		if o.ProposalID == nil {
			continue
		}
		link[id] = *o.ProposalID
		if _, ok := known[*o.ProposalID]; !ok {
			missing = append(missing, *o.ProposalID)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		extra, err := jnl.ProposalsByIDs(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("retro: proposals behind entry orders: %w", err)
		}
		for id, p := range extra {
			known[id] = p
		}
	}
	for orderID, proposalID := range link {
		if p, ok := known[proposalID]; ok {
			out[orderID] = p
		}
	}
	return out, nil
}
