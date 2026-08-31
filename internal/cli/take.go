package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/trading"
)

func newTakeCmd() *cobra.Command {
	var qty int
	cmd := &cobra.Command{
		Use:   "take N",
		Short: "Trade today's proposal N",
		Long: "take submits the proposal as a bracket: a limit at its entry with its stop and target\n" +
			"attached. The size is the one Go computed from the risk limits; --qty may only lower it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd, "take "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			index, err := proposalIndex(args[0])
			if err != nil {
				return err
			}
			return runTake(cmd.Context(), a, index, qty, cmd.Flags().Changed("qty"))
		},
	}
	cmd.Flags().IntVar(&qty, "qty", 0, "trade fewer shares than the sized quantity")
	return cmd
}

// runTake claims the idea, submits it, and records what happened. The claim is
// what makes the window between the venue accepting an order and the decision
// landing survivable: a crash there leaves a claim, not a takeable proposal.
func runTake(ctx context.Context, a *app, index, qty int, lowered bool) error {
	day := a.slateDay(ctx)
	p, err := a.openProposal(ctx, index)
	if err != nil {
		return err
	}
	if p.Day != day {
		return fmt.Errorf("proposal #%d (%s) is filed for %s but the takeable slate is %s; run `tape proposals`",
			p.Index, p.Symbol, p.Day, day)
	}
	if lowered {
		if qty < 1 || qty > p.Qty {
			return fmt.Errorf("take %d: --qty %d is outside 1 to %d; take may lower the sized quantity, never raise it",
				index, qty, p.Qty)
		}
		p.Qty = qty
	}
	riskUSD := float64(p.Qty) * (p.Entry - p.Stop)

	if err := a.jnl.ClaimProposal(ctx, p.ID, timeNow().UTC()); err != nil {
		return err
	}

	entry, stop, target := p.Entry, p.Stop, p.Target
	req := broker.OrderRequest{
		Symbol:     p.Symbol,
		Side:       broker.Buy,
		Qty:        p.Qty,
		Type:       broker.Limit,
		LimitPrice: &entry,
		StopLoss:   &stop,
		TakeProfit: &target,
	}
	note := fmt.Sprintf("proposal #%d %s", p.Index, p.SetupID)

	res, err := a.engine.SubmitFor(ctx, req, journal.SourceProposal, note, &p.ID)
	if err != nil {
		return takeFailed(ctx, a, p, index, err)
	}
	if err := a.jnl.DecideTaken(ctx, p.ID, res.Order.ID, p.Qty, riskUSD, timeNow().UTC()); err != nil {
		return fmt.Errorf("order #%d is live for proposal #%d but the decision did not record: %w", res.Order.ID, p.Index, err)
	}
	printCancelled(a, res)
	fmt.Fprintf(a.out, "\ntaken: proposal #%d %s %d sh → order #%d\n", p.Index, p.Symbol, p.Qty, res.Order.ID)
	return printSubmission(a, res)
}

// takeFailed answers the claim. A rule that refused the idea itself decides it;
// one that refused today's circumstances reopens it, because a cap that will be
// gone in an hour is not a verdict on the trade. Anything else leaves the claim
// standing: the venue may hold an order nothing has recorded yet.
func takeFailed(ctx context.Context, a *app, p journal.Proposal, index int, err error) error {
	wrapped := fmt.Errorf("take %d (%s): %w", index, p.Symbol, err)

	var refusal *trading.RefusalError
	if !errors.As(err, &refusal) {
		fmt.Fprintf(a.out, "\norder may be live for proposal #%d; run `tape orders` or `tape proposals --reconcile`\n", p.Index)
		return wrapped
	}
	if refusal.Situational {
		if released := a.jnl.ReleaseProposal(ctx, p.ID); released != nil {
			return errors.Join(wrapped, released)
		}
		fmt.Fprintf(a.out, "\nproposal #%d %s is still open; the rule refused today, not the idea.\n", p.Index, p.Symbol)
		return wrapped
	}
	if decided := a.jnl.DecideProposal(ctx, p.ID, journal.ProposalRejected, refusal.Error(), nil, timeNow().UTC()); decided != nil {
		return errors.Join(wrapped, decided)
	}
	fmt.Fprintf(a.out, "\nproposal #%d %s is rejected on the record.\n", p.Index, p.Symbol)
	return wrapped
}

func newPassCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "pass N",
		Short: "Decline today's proposal N, with a reason",
		Long: "pass records the veto and why. Passes are scored against what the trade would have\n" +
			"done, so the reason is required: an unexplained pass teaches the record nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newRecordApp(cmd, "pass "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			index, err := proposalIndex(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("pass %d: --reason is required; a pass is scored later and needs to say why", index)
			}
			p, err := a.openProposal(cmd.Context(), index)
			if err != nil {
				return err
			}
			if err := a.jnl.DecideProposal(cmd.Context(), p.ID, journal.ProposalPassed, oneLine(reason), nil, timeNow().UTC()); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "\npassed: proposal #%d %s — %s\n", p.Index, p.Symbol, oneLine(reason))
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why you are declining this idea (required)")
	return cmd
}
