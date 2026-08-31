package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/journal"
)

func newProposalsCmd() *cobra.Command {
	var (
		day       string
		reconcile bool
	)
	cmd := &cobra.Command{
		Use:   "proposals",
		Short: "List a session's trade ideas and what became of them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := openProposalsApp(cmd, reconcile)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			if reconcile {
				rep, err := a.engine.Sync(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(a.out, "\nreconciled %d order(s); %d proposal(s) closed out\n",
					rep.Checked, len(rep.ReconciledProposals))
			}
			if day == "" {
				day = a.slateDay(ctx)
			}
			rows, err := a.jnl.ProposalsForDay(ctx, a.cfg.Mode, day)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintf(a.out, "\nno proposals for %s; run `tape brief`.\n", day)
				return nil
			}

			fmt.Fprintf(a.out, "\n%s\n", day)
			tw := table(a.out)
			row(tw, "#", "SYMBOL", "SETUP", "ENTRY", "STOP", "TARGET", "QTY", "RISK", "STATUS", "NOTE")
			for _, p := range rows {
				row(tw, "#"+strconv.Itoa(p.Index), p.Symbol, p.SetupID,
					price(p.Entry), price(p.Stop), price(p.Target),
					tradedQty(p), tradedRisk(p), p.Status, statusNote(p))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&day, "day", "", "session to list (YYYY-MM-DD, default today)")
	cmd.Flags().BoolVar(&reconcile, "reconcile", false, "sync the venue first, closing out any proposal whose order is already live")
	return cmd
}

// openProposalsApp builds the venue-backed app only when --reconcile needs one,
// so listing the archive still works without keys.
func openProposalsApp(cmd *cobra.Command, reconcile bool) (*app, error) {
	if reconcile {
		return newApp(cmd, "proposals --reconcile")
	}
	return newRecordApp(cmd, "proposals")
}

// proposal resolves the number the trader typed against the session's slate.
func (a *app) proposal(ctx context.Context, index int) (journal.Proposal, error) {
	day := a.slateDay(ctx)
	p, err := a.jnl.ProposalByDayIndex(ctx, a.cfg.Mode, day, index)
	if errors.Is(err, journal.ErrNotFound) {
		return journal.Proposal{}, a.noSuchProposal(ctx, day, index)
	}
	return p, err
}

// noSuchProposal says how many ideas the slate actually has. A re-run that
// produced two ideas leaves "take 3" pointing at nothing, and "expired" is the
// wrong answer for a number that was never on the slate.
func (a *app) noSuchProposal(ctx context.Context, day string, index int) error {
	n, err := a.slateSize(ctx, day)
	if err != nil || n == 0 {
		return fmt.Errorf("no proposal #%d for %s; run `tape brief`, or `tape proposals` to see the slate", index, day)
	}
	return fmt.Errorf("no proposal #%d for %s; the current slate has %d idea(s)", index, day, n)
}

// slateSize counts the ideas in the day's newest slate, which is the one the
// trader was shown last.
func (a *app) slateSize(ctx context.Context, day string) (int, error) {
	rows, err := a.jnl.ProposalsForDay(ctx, a.cfg.Mode, day)
	if err != nil {
		return 0, err
	}
	var newest int64
	for _, p := range rows {
		newest = max(newest, p.BriefingID)
	}
	n := 0
	for _, p := range rows {
		if p.BriefingID == newest {
			n++
		}
	}
	return n, nil
}

// openProposal is proposal plus the rule that a decision is made once.
func (a *app) openProposal(ctx context.Context, index int) (journal.Proposal, error) {
	p, err := a.proposal(ctx, index)
	if err != nil {
		return p, err
	}
	if p.Status == journal.ProposalSubmitting {
		return p, fmt.Errorf("proposal #%d (%s) is being submitted and an order for it may already be live; "+
			"run `tape orders` or `tape proposals --reconcile` before taking it again", p.Index, p.Symbol)
	}
	if p.Status != journal.ProposalProposed {
		return p, fmt.Errorf("proposal #%d (%s) is already %s%s; a proposal is decided once",
			p.Index, p.Symbol, p.Status, reasonTail(p))
	}
	return p, nil
}

func reasonTail(p journal.Proposal) string {
	if p.Reason == "" {
		return ""
	}
	return ": " + oneLine(p.Reason)
}

func proposalIndex(arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a proposal number; they start at 1", arg)
	}
	return n, nil
}

// trimPct renders a percentage without trailing zeroes, so 0.5 reads as "0.5".
func trimPct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
