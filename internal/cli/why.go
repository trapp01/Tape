package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/playbook"
)

func newWhyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "why N",
		Short: "Show everything behind today's proposal N",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newRecordApp(cmd, "why "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			index, err := proposalIndex(args[0])
			if err != nil {
				return err
			}
			p, err := a.proposal(cmd.Context(), index)
			if err != nil {
				return err
			}
			return printWhy(cmd.Context(), a, p)
		},
	}
}

func printWhy(ctx context.Context, a *app, p journal.Proposal) error {
	in, inputErr := a.proposalInput(ctx, p)
	title := setupTitle(playbook.SetupTitles(in.Playbook), p.SetupID)

	fmt.Fprintf(a.out, "\n#%d  %s %s — %s\n", p.Index, strings.ToUpper(p.Side), p.Symbol, title)
	tw := table(a.out)
	pair(tw, "entry", price(p.Entry))
	pair(tw, "stop", price(p.Stop))
	pair(tw, "target", price(p.Target))
	if rr := rewardRisk(p); rr > 0 {
		pair(tw, "reward/risk", fmt.Sprintf("%.2fR", rr))
	}
	pair(tw, "thesis", oneLine(p.Thesis))
	pair(tw, "invalid if", oneLine(p.Invalidation))
	pair(tw, "confidence", dash(p.Confidence))
	pair(tw, "status", p.Status)
	if p.TakenQty != nil && *p.TakenQty != p.Qty {
		pair(tw, "traded", fmt.Sprintf("%d sh of the %d sized%s", *p.TakenQty, p.Qty, tradedRiskTail(p)))
	}
	if p.Reason != "" {
		pair(tw, "reason", oneLine(p.Reason))
	}
	if p.OrderID != nil {
		pair(tw, "order", fmt.Sprintf("#%d", *p.OrderID))
	}
	pair(tw, "briefing", fmt.Sprintf("#%d (%s)", p.BriefingID, p.Day))
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(a.out, "\nSizing (computed in Go, never by the model)")
	tw = table(a.out)
	switch {
	case inputErr != nil:
		pair(tw, "inputs", "unavailable: "+inputErr.Error())
	case in.Limits.PerTradePct <= 0:
		pair(tw, "inputs", "sizing basis missing from the archive")
	default:
		budget := in.Equity * in.Limits.PerTradePct / 100
		risked := riskSizedQty(p, in)
		pair(tw, "budget", fmt.Sprintf("%s equity × %s%% = %s", money(in.Equity), trimPct(in.Limits.PerTradePct), money(budget)))
		pair(tw, "shares", fmt.Sprintf("%s / (%s − %s) = %d, rounded down", money(budget), price(p.Entry), price(p.Stop), risked))
		if cashCapped(p, in) {
			pair(tw, "capped", fmt.Sprintf("%s of free cash at %s buys %d",
				money(brief.CashCeiling(in.FreeCash)), price(p.Entry), p.Qty))
		}
	}
	pair(tw, "risked at the stop", money(p.RiskUSD))
	pair(tw, "notional", money(float64(p.Qty)*p.Entry))
	return tw.Flush()
}

// tradedQty and tradedRisk show what a take actually submitted next to what the
// slate sized, because `--qty` may lower both.
func tradedQty(p journal.Proposal) string {
	if p.TakenQty == nil || *p.TakenQty == p.Qty {
		return strconv.Itoa(p.Qty)
	}
	return fmt.Sprintf("%d→%d", p.Qty, *p.TakenQty)
}

func tradedRisk(p journal.Proposal) string {
	if p.TakenRiskUSD == nil || *p.TakenRiskUSD == p.RiskUSD {
		return money(p.RiskUSD)
	}
	return money(p.RiskUSD) + "→" + money(*p.TakenRiskUSD)
}

func tradedRiskTail(p journal.Proposal) string {
	if p.TakenRiskUSD == nil {
		return ""
	}
	return fmt.Sprintf(", risking %s of the %s", money(*p.TakenRiskUSD), money(p.RiskUSD))
}

// proposalInput recovers the equity and limits the idea was sized under, which
// live on the briefing rather than the proposal row.
func (a *app) proposalInput(ctx context.Context, p journal.Proposal) (brief.Input, error) {
	b, err := a.jnl.BriefingByID(ctx, p.BriefingID)
	if err != nil {
		return brief.Input{}, err
	}
	return brief.ArchivedInput(b)
}
