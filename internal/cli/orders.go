package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/journal"
)

func newOrdersCmd() *cobra.Command {
	var (
		openOnly bool
		since    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "orders",
		Short: "List journaled orders",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "orders")
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			if _, err := a.engine.Sync(ctx); err != nil {
				return err
			}

			filter := journal.ListFilter{Mode: a.cfg.Mode, OpenOnly: openOnly}
			if since > 0 {
				filter.Since = a.engine.Today().Add(-since)
			}
			orders, err := a.jnl.ListOrders(ctx, filter)
			if err != nil {
				return err
			}
			if len(orders) == 0 {
				fmt.Fprintf(a.out, "\nno orders in the last %s.\n", since)
				return nil
			}

			fmt.Fprintln(a.out)
			tw := table(a.out)
			row(tw, "ID", "TIME", "SYMBOL", "SIDE", "QTY", "TYPE", "STATUS", "FILLED", "SOURCE")
			for _, o := range orders {
				row(tw,
					fmt.Sprintf("#%d", o.ID),
					shortStamp(o.SubmittedAt, a.loc),
					o.Symbol,
					o.Side,
					strconv.Itoa(o.Qty),
					orderTypeLabel(o),
					o.Status,
					filledCell(o),
					dash(o.Source),
				)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&openOnly, "open", false, "only orders that can still fill")
	cmd.Flags().DurationVar(&since, "since", 24*time.Hour, "how far back to look (0 for everything)")
	return cmd
}

func filledCell(o journal.Order) string {
	if o.FilledQty == 0 {
		return "-"
	}
	if o.FilledAvgPrice == nil {
		return strconv.Itoa(o.FilledQty)
	}
	return fmt.Sprintf("%d @ %s", o.FilledQty, price(*o.FilledAvgPrice))
}
