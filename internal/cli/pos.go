package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newPosCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pos",
		Short: "Show open positions from the journal, priced live",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "positions")
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			if _, err := a.engine.Sync(ctx); err != nil {
				return err
			}
			views, err := a.engine.Positions(ctx)
			if err != nil {
				return err
			}
			if len(views) == 0 {
				fmt.Fprintln(a.out, "\nflat: the ledger holds nothing.")
				return nil
			}

			fmt.Fprintln(a.out)
			tw := table(a.out)
			row(tw, "SYMBOL", "QTY", "AVG ENTRY", "CURRENT", "COST BASIS", "MARKET VALUE", "UNREALIZED")
			total := 0.0
			for _, v := range views {
				unrealized := "no price"
				if v.Priced {
					unrealized = a.style.pl(v.UnrealizedPL, fmt.Sprintf("%s (%s)", signedMoney(v.UnrealizedPL), percent(v.UnrealizedPct)))
					total += v.UnrealizedPL
				}
				row(tw,
					v.Symbol,
					strconv.Itoa(v.Qty),
					price(v.AvgEntryPrice),
					priceOrDash(v.CurrentPrice, v.Priced),
					money(v.CostBasis),
					moneyOrDash(v.MarketValue, v.Priced),
					unrealized,
				)
			}
			if err := tw.Flush(); err != nil {
				return err
			}

			tw = table(a.out)
			fmt.Fprintln(a.out)
			pair(tw, "unrealized total", a.style.pl(total, signedMoney(total)))
			return tw.Flush()
		},
	}
}

func priceOrDash(v float64, ok bool) string {
	if !ok {
		return "-"
	}
	return price(v)
}

func moneyOrDash(v float64, ok bool) string {
	if !ok {
		return "-"
	}
	return money(v)
}
