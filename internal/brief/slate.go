package brief

import (
	"context"
	"fmt"

	"github.com/trapp01/tape/internal/journal"
)

// fileSlate sizes the morning's ideas and puts every one of them on the record:
// the ones that clear the limits as proposed, the ones that do not as rejected
// with the rule text. The slate locks with the call — after the open the standing
// slate is the takeable one and this run is only a second read.
func fileSlate(ctx context.Context, d Deps, res *Result, day string) error {
	if res.CallKept {
		res.SlateKept = true
		return loadStandingSlate(ctx, d, res, day)
	}

	res.Sized = SizeProposals(res.Input, res.Output, res.Input.Equity, res.Input.FreeCash, d.Limits)
	at := d.now().UTC()
	if _, err := d.Journal.ExpireOpenProposals(ctx, d.Mode, day, at); err != nil {
		return err
	}

	rows := make([]*journal.Proposal, 0, len(res.Sized))
	for i, s := range res.Sized {
		rows = append(rows, &journal.Proposal{
			BriefingID:   res.Briefing.ID,
			Mode:         d.Mode,
			Day:          day,
			Index:        i + 1,
			Symbol:       s.Symbol,
			Side:         s.Side,
			SetupID:      s.SetupID,
			Entry:        s.Entry,
			Stop:         s.Stop,
			Target:       s.Target,
			Qty:          s.Plan.Qty,
			RiskUSD:      s.Plan.RiskUSD,
			Thesis:       s.Thesis,
			Invalidation: s.Invalidation,
			Confidence:   string(s.Confidence),
			Status:       journal.ProposalProposed,
			CreatedAt:    at,
		})
	}
	if err := d.Journal.InsertProposals(ctx, rows); err != nil {
		return err
	}
	for i, s := range res.Sized {
		if s.Sizeable() {
			continue
		}
		if err := d.Journal.DecideProposal(ctx, rows[i].ID, journal.ProposalRejected, s.Reject, nil, at); err != nil {
			return err
		}
	}

	stored, err := d.Journal.ProposalsByBriefing(ctx, res.Briefing.ID)
	if err != nil {
		return err
	}
	res.Proposals = stored
	return nil
}

// loadStandingSlate returns the session's takeable ideas: the newest slate filed
// for the day, which is the one the trader was shown before the bell.
func loadStandingSlate(ctx context.Context, d Deps, res *Result, day string) error {
	all, err := d.Journal.ProposalsForDay(ctx, d.Mode, day)
	if err != nil {
		return fmt.Errorf("brief: reading the session's standing slate: %w", err)
	}
	res.Proposals = newestSlate(all)
	return nil
}

// newestSlate keeps the rows of the highest briefing id present, so an earlier
// re-run's expired ideas do not read as today's plan.
func newestSlate(ps []journal.Proposal) []journal.Proposal {
	var newest int64
	for _, p := range ps {
		if p.BriefingID > newest {
			newest = p.BriefingID
		}
	}
	out := make([]journal.Proposal, 0, len(ps))
	for _, p := range ps {
		if p.BriefingID == newest {
			out = append(out, p)
		}
	}
	return out
}
