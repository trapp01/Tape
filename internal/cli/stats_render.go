package cli

import (
	"fmt"
	"sort"
	"strconv"
	"text/tabwriter"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/stats"
)

// renderReport prints every section of a report in the order a reader wants
// them: what happened, then what it was worth, then whether it means anything.
func renderReport(a *app, rep stats.Report, version *journal.PlaybookVersion, span string) {
	fmt.Fprintf(a.out, "\n%s · %d session(s) · %s\n", span, rep.Sessions, rep.Window.Mode)

	renderTrades(a, rep)
	renderEquity(a, rep)
	renderSetups(a, rep)
	renderRegimes(a, rep)
	renderReads(a, rep)
	renderProposalStats(a, rep)
	renderRefusals(a, rep)
	renderSignificance(a, rep)
	renderGate(a, rep, version)
}

func renderTrades(a *app, rep stats.Report) {
	t := rep.Trades
	fmt.Fprintln(a.out, "\nTRADES")
	if t.Count == 0 {
		fmt.Fprintln(a.out, "  nothing closed in this window.")
		return
	}
	tw := table(a.out)
	pair(tw, "closed", fmt.Sprintf("%d  (%d win / %d loss, %.0f%%)", t.Count, t.Wins, t.Losses, t.WinRate*100))
	pair(tw, "expectancy", a.style.pl(t.ExpectancyUSD, signedMoney(t.ExpectancyUSD)+" per trade"))
	pair(tw, "avg win / avg loss", signedMoney(t.AvgWinUSD)+" / "+signedMoney(t.AvgLossUSD))
	pair(tw, "profit factor", profitFactor(t))
	pair(tw, "gross", signedMoney(t.GrossPL))
	pair(tw, "costs", money(t.Costs))
	pair(tw, "net", a.style.pl(t.NetPL, signedMoney(t.NetPL)))
	tw.Flush()
}

func renderEquity(a *app, rep stats.Report) {
	e := rep.Equity
	fmt.Fprintln(a.out, "\nEQUITY (the whole record; a window never moves the account)")
	tw := table(a.out)
	pair(tw, "start", money(e.StartingEquity))
	pair(tw, "end", a.style.pl(e.EndingEquity-e.StartingEquity, money(e.EndingEquity)))
	pair(tw, "return", a.style.pl(e.ReturnPct, percent(e.ReturnPct)))
	pair(tw, "max drawdown", fmt.Sprintf("%s (%.1f%%)", money(e.MaxDrawdownUSD), e.MaxDrawdownPct))
	pair(tw, "this window", a.style.pl(e.WindowNetPL,
		fmt.Sprintf("%s (%s)", signedMoney(e.WindowNetPL), percent(e.WindowReturnPct))))
	tw.Flush()
}

func renderSetups(a *app, rep stats.Report) {
	fmt.Fprintln(a.out, "\nBY SETUP")
	if len(rep.BySetup) == 0 {
		fmt.Fprintln(a.out, "  nothing traded or replayed.")
		return
	}
	tw := table(a.out)
	row(tw, "  SETUP", "TRADES", "WIN", "EXPECTANCY", "NET", "REPLAYS", "REPLAY NET")
	for _, s := range rep.BySetup {
		row(tw, "  "+s.SetupID, strconv.Itoa(s.Trades.Count), winRate(s.Trades),
			signedMoney(s.Trades.ExpectancyUSD), signedMoney(s.Trades.NetPL),
			replayCount(s.Counterfactual), a.style.pl(s.Counterfactual.NetPL, signedMoney(s.Counterfactual.NetPL)))
	}
	tw.Flush()
}

func renderRegimes(a *app, rep stats.Report) {
	fmt.Fprintln(a.out, "\nBY REGIME")
	if len(rep.ByRegime) == 0 {
		fmt.Fprintln(a.out, "  no briefing archived a regime in this window.")
		return
	}
	tw := table(a.out)
	row(tw, "  REGIME", "SESSIONS", "CALLS", "NOTES", "TRADES", "NET")
	for _, r := range rep.ByRegime {
		row(tw, "  "+r.Label, strconv.Itoa(r.Sessions), hits(r.Calls), hits(r.Notes),
			strconv.Itoa(r.Trades.Count), a.style.pl(r.Trades.NetPL, signedMoney(r.Trades.NetPL)))
	}
	tw.Flush()
}

// renderReads prints both graded predictions. The warning under them is measured
// against the two together: neither sample stands alone yet.
func renderReads(a *app, rep stats.Report) {
	fmt.Fprintln(a.out, "\nCALLS / NOTES")
	tw := table(a.out)
	row(tw, "  KIND", "GRADED", "CORRECT", "PENDING", "INSIDE NOISE BAND")
	readRow(tw, "  calls", rep.Calls)
	readRow(tw, "  notes", rep.Notes)
	tw.Flush()

	if graded := rep.Calls.Total + rep.Notes.Total; graded < meaningfulCalls {
		fmt.Fprintln(a.out, a.style.dim(fmt.Sprintf(
			"  %d reads graded here; this needs 3+ months to mean anything.", graded)))
	}
}

func readRow(tw *tabwriter.Writer, label string, c stats.CallStats) {
	accuracy := "-"
	if c.Total > 0 {
		accuracy = fmt.Sprintf("%d (%.0f%%)", c.Correct, c.Accuracy*100)
	}
	row(tw, label, strconv.Itoa(c.Total), accuracy, strconv.Itoa(c.Pending), strconv.Itoa(c.WithinNoiseBand))
}

// renderProposalStats shows what became of every idea and what the replays say
// the decisions cost or saved. This is the only place a veto is measurable.
func renderProposalStats(a *app, rep stats.Report) {
	p := rep.Proposals
	fmt.Fprintln(a.out, "\nPROPOSALS")
	tw := table(a.out)
	pair(tw, "status", fmt.Sprintf("proposed %d · taken %d · passed %d · rejected %d · expired %d · unfilled %d",
		p.Proposed, p.Taken, p.Passed, p.Rejected, p.Expired, p.Unfilled))
	pair(tw, "passes that would have profited", fmt.Sprintf("%d (%s left on the table)",
		p.PassesThatWouldHaveProfited, money(p.MissedNetUSD)))
	pair(tw, "losses the vetoes avoided", money(p.VetoedLossesAvoidedUSD))
	pair(tw, "execution drag on takes", a.style.pl(-p.ExecutionDragUSD, signedMoney(p.ExecutionDragUSD)))
	c := p.Counterfactual
	pair(tw, "replays", fmt.Sprintf("%d replayed · %d filled (%d win / %d loss) · net %s · avg %.2fR",
		c.Replayed, c.Filled, c.Wins, c.Losses, signedMoney(c.NetPL), c.AvgRMultiple))
	pair(tw, "decided by the stop-first rule", strconv.Itoa(c.Ambiguous))
	tw.Flush()
}

func renderRefusals(a *app, rep stats.Report) {
	r := rep.Refusals
	fmt.Fprintln(a.out, "\nREFUSALS")
	if r.Total == 0 {
		fmt.Fprintln(a.out, "  no guardrail had to say no.")
		return
	}
	rules := make([]string, 0, len(r.ByRule))
	for rule := range r.ByRule {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if r.ByRule[rules[i]] != r.ByRule[rules[j]] {
			return r.ByRule[rules[i]] > r.ByRule[rules[j]]
		}
		return rules[i] < rules[j]
	})
	tw := table(a.out)
	for _, rule := range rules {
		pair(tw, rule, strconv.Itoa(r.ByRule[rule]))
	}
	pair(tw, "last 30 days", strconv.Itoa(r.LastMonth))
	tw.Flush()
}

// profitFactor reads the field the way the gate does: with no losing trade the
// ratio has no divisor and the number carries gross profit instead.
func profitFactor(t stats.TradeStats) string {
	if t.Count == 0 {
		return "-"
	}
	if t.Losses == 0 {
		return "no losses"
	}
	return fmt.Sprintf("%.2f", t.ProfitFactor)
}

func winRate(t stats.TradeStats) string {
	if t.Count == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", t.WinRate*100)
}

func hits(c stats.CallStats) string {
	if c.Total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", c.Correct, c.Total)
}

func replayCount(c stats.CounterfactualStats) string {
	if c.Replayed == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d filled", c.Filled, c.Replayed)
}

// exitPhrase says how a replay ended, naming the convention when a single bar
// could not decide it.
func exitPhrase(o journal.ProposalOutcome) string {
	if !o.Filled {
		return "never filled"
	}
	if o.Ambiguous {
		return o.ExitKind + " (stop-first)"
	}
	return o.ExitKind
}
