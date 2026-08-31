package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/broker"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the ledger, open positions, and the market clock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "status")
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			led, err := a.jnl.Ledger(ctx, a.cfg.Mode)
			if err != nil {
				return err
			}
			account, err := a.broker.Account(ctx)
			if err != nil {
				return err
			}
			clock, err := a.broker.Clock(ctx)
			if err != nil {
				return err
			}

			fmt.Fprintln(a.out, "\nLedger (tape)")
			tw := table(a.out)
			pair(tw, "starting equity", money(led.StartingEquity))
			pair(tw, "cash", money(led.Cash))
			pair(tw, "realized P&L (net)", a.style.pl(led.RealizedPL, signedMoney(led.RealizedPL)))
			pair(tw, "costs paid", money(led.Commissions+led.Fees))
			pair(tw, "open positions", strconv.Itoa(len(led.OpenPositions)))
			pair(tw, "journal", a.jnl.Path())
			if err := tw.Flush(); err != nil {
				return err
			}

			fmt.Fprintf(a.out, "\nBroker: %s (ignored by stats)\n", a.broker.Name())
			tw = table(a.out)
			pair(tw, "paper equity", money(account.Equity))
			pair(tw, "paper cash", money(account.Cash))
			if err := tw.Flush(); err != nil {
				return err
			}

			fmt.Fprintln(a.out, "\nMarket")
			tw = table(a.out)
			pair(tw, "status", marketState(clock))
			pair(tw, "next open", stamp(clock.NextOpen, a.loc))
			pair(tw, "next close", stamp(clock.NextClose, a.loc))
			if err := tw.Flush(); err != nil {
				return err
			}
			return printRisk(ctx, a)
		},
	}
}

// printRisk shows the walls every entry clears, the count of times they fired
// today, and whether the day is already over.
func printRisk(ctx context.Context, a *app) error {
	l := a.engine.Limits()
	equity, err := a.engine.Equity(ctx)
	if err != nil {
		return err
	}
	// Refusals are filed under the session they belong to, which after 22:00
	// Mountain is already tomorrow's.
	day := a.slateDay(ctx)
	refusals, err := a.jnl.RefusalCount(ctx, a.cfg.Mode, day, day)
	if err != nil {
		return err
	}
	halted, why, err := a.engine.Halted(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintln(a.out, "\nRisk (enforced in Go)")
	tw := table(a.out)
	pair(tw, "per trade", fmt.Sprintf("%s%% of equity = %s", trimPct(l.PerTradePct), money(equity*l.PerTradePct/100)))
	pair(tw, "max positions", strconv.Itoa(l.MaxPositions))
	pair(tw, "daily loss limit", fmt.Sprintf("%d closed losses", l.MaxDailyLosses))
	pair(tw, "entries stop", fmt.Sprintf("%d min before the close", l.NoEntriesBeforeCloseMinutes))
	pair(tw, "refusals today", strconv.Itoa(refusals))
	if halted {
		pair(tw, "HALT", why)
	}
	return tw.Flush()
}

func marketState(c broker.Clock) string {
	if c.IsOpen {
		return "open"
	}
	return "closed"
}
