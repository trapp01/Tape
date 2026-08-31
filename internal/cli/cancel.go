package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/trading"
)

func newCancelCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "cancel [ORDER-ID...]",
		Short: "Cancel resting orders at the venue",
		Long: "cancel takes journal order numbers, the ones `tape orders` prints. --all cancels\n" +
			"every order this account still has working.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := orderIDs(args, all)
			if err != nil {
				return err
			}
			a, err := newApp(cmd, cancelHeadline(args, all))
			if err != nil {
				return err
			}
			defer a.Close()

			cancelled, err := a.engine.CancelOpen(cmd.Context(), ids)
			if err != nil {
				return err
			}
			return printCancellations(a, cancelled)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "cancel every order that can still fill")
	return cmd
}

// orderIDs parses the numbers `tape orders` prints. Cancelling nothing in
// particular is a mistake, so an empty list without --all is refused.
func orderIDs(args []string, all bool) ([]int64, error) {
	if all {
		if len(args) > 0 {
			return nil, fmt.Errorf("cancel: --all cancels everything open, so it takes no order numbers")
		}
		return nil, nil
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("cancel: name the order numbers from `tape orders`, or pass --all")
	}
	ids := make([]int64, 0, len(args))
	for _, arg := range args {
		id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(arg), "#"), 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("cancel: %q is not an order number; they are the ids `tape orders` prints", arg)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func cancelHeadline(args []string, all bool) string {
	if all {
		return "cancel --all"
	}
	return "cancel " + strings.Join(args, " ")
}

func printCancellations(a *app, cancelled []journal.Order) error {
	if len(cancelled) == 0 {
		fmt.Fprintln(a.out, "\nnothing open to cancel.")
		return nil
	}
	fmt.Fprintf(a.out, "\ncancelled %d order(s)\n", len(cancelled))
	tw := table(a.out)
	row(tw, "ID", "SYMBOL", "SIDE", "QTY", "TYPE", "STATUS", "FILLED")
	for _, o := range cancelled {
		row(tw, "#"+strconv.FormatInt(o.ID, 10), o.Symbol, o.Side,
			strconv.Itoa(o.Qty), orderTypeLabel(o), o.Status, filledCell(o))
	}
	return tw.Flush()
}

// printCancelled names the resting orders an exit took off the books to reach
// its own shares, so the trader is never surprised by a missing stop.
func printCancelled(a *app, res trading.Result) {
	if len(res.Cancelled) == 0 {
		return
	}
	fmt.Fprintf(a.out, "\ncancelled %d resting %s for %s\n",
		len(res.Cancelled), cancelledKind(res.Cancelled), res.Cancelled[0].Symbol)
}

// cancelledKind names what came off: a bracket's legs, or plain resting sells.
func cancelledKind(orders []journal.Order) string {
	legs := true
	for _, o := range orders {
		if o.ParentOrderID == "" {
			legs = false
		}
	}
	noun := "sell order"
	if legs {
		noun = "bracket leg"
	}
	if len(orders) != 1 {
		noun += "s"
	}
	return noun
}
