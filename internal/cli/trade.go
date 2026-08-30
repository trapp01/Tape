package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/trading"
)

func newBuyCmd() *cobra.Command {
	var (
		limit, stop, target float64
		note                string
	)
	cmd := &cobra.Command{
		Use:   "buy SYMBOL QTY",
		Short: "Buy shares, with an optional bracket",
		Long:  "buy submits an entry. --stop and --target together attach a bracket; one alone attaches a single exit.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := orderRequest(broker.Buy, args, cmd, limit)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("stop") {
				req.StopLoss = &stop
			}
			if cmd.Flags().Changed("target") {
				req.TakeProfit = &target
			}
			return runTrade(cmd, req, note)
		},
	}
	cmd.Flags().Float64Var(&limit, "limit", 0, "limit price (default: market order)")
	cmd.Flags().Float64Var(&stop, "stop", 0, "stop-loss price for the bracket")
	cmd.Flags().Float64Var(&target, "target", 0, "take-profit price for the bracket")
	cmd.Flags().StringVar(&note, "note", "", "why you are taking this trade")
	return cmd
}

func newSellCmd() *cobra.Command {
	var (
		limit float64
		note  string
	)
	cmd := &cobra.Command{
		Use:   "sell SYMBOL QTY",
		Short: "Sell shares the ledger holds",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := orderRequest(broker.Sell, args, cmd, limit)
			if err != nil {
				return err
			}
			return runTrade(cmd, req, note)
		},
	}
	cmd.Flags().Float64Var(&limit, "limit", 0, "limit price (default: market order)")
	cmd.Flags().StringVar(&note, "note", "", "why you are closing this")
	return cmd
}

func orderRequest(side broker.Side, args []string, cmd *cobra.Command, limit float64) (broker.OrderRequest, error) {
	symbol := strings.ToUpper(strings.TrimSpace(args[0]))
	qty, err := strconv.Atoi(args[1])
	if err != nil {
		return broker.OrderRequest{}, fmt.Errorf("%s %s %q: quantity must be a whole number of shares", side, symbol, args[1])
	}
	req := broker.OrderRequest{Symbol: symbol, Side: side, Qty: qty, Type: broker.Market}
	if cmd.Flags().Changed("limit") {
		req.Type = broker.Limit
		req.LimitPrice = &limit
	}
	return req, nil
}

// runTrade submits through the engine and prints the journaled order with any
// fill the poll window caught.
func runTrade(cmd *cobra.Command, req broker.OrderRequest, note string) error {
	headline := fmt.Sprintf("%s %d %s", req.Side, req.Qty, req.Symbol)
	a, err := newApp(cmd, headline)
	if err != nil {
		return err
	}
	defer a.Close()

	res, err := a.engine.Submit(cmd.Context(), req, journal.SourceHuman, note)
	if err != nil {
		return fmt.Errorf("%s: %w", headline, err)
	}
	return printSubmission(a, res)
}

func printSubmission(a *app, res trading.Result) error {
	o := res.Order
	fmt.Fprintln(a.out)
	tw := table(a.out)
	pair(tw, "journal id", fmt.Sprintf("#%d", o.ID))
	pair(tw, "order", fmt.Sprintf("%s %d %s %s", o.Side, o.Qty, o.Symbol, orderTypeLabel(o)))
	pair(tw, "status", o.Status)
	pair(tw, "broker id", dash(o.BrokerOrderID))
	if o.StopLoss != nil {
		pair(tw, "stop", price(*o.StopLoss))
	}
	if o.TakeProfit != nil {
		pair(tw, "target", price(*o.TakeProfit))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(res.Fills) == 0 {
		fmt.Fprintf(a.out, "\nno fill yet; run `tape orders --open` or `tape pos` to pick it up.\n")
		return nil
	}

	fmt.Fprintln(a.out, "\nFills")
	tw = table(a.out)
	row(tw, "QTY", "RAW", "MODELED", "COMMISSION", "FEES", cashLabel(o.Side))
	for _, f := range res.Fills {
		row(tw, strconv.Itoa(f.Qty), price(f.RawPrice), price(f.ModeledPrice), money(f.Commission), money(f.Fees), money(fillCash(f)))
	}
	return tw.Flush()
}

func cashLabel(side string) string {
	if side == string(broker.Sell) {
		return "NET"
	}
	return "COST"
}

// fillCash is what the fill moves through the ledger: a buy pays the costs on top
// of the shares, a sell has them taken out of the proceeds.
func fillCash(f journal.Fill) float64 {
	gross := float64(f.Qty) * f.ModeledPrice
	if f.Side == string(broker.Sell) {
		return gross - f.Commission - f.Fees
	}
	return gross + f.Commission + f.Fees
}

func orderTypeLabel(o journal.Order) string {
	if o.LimitPrice != nil {
		return fmt.Sprintf("%s %s", o.Type, price(*o.LimitPrice))
	}
	return o.Type
}
