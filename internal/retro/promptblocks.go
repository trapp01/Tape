package retro

import (
	"fmt"
	"strings"

	"github.com/trapp01/tape/internal/risk"
	"github.com/trapp01/tape/internal/stats"
)

// The blocks below render one section of the prompt each. Anything the trader or
// an earlier review wrote goes inside a fenced block; the rest is tape's own text.

func writeTrades(b *strings.Builder, rep stats.Report) {
	t := rep.Trades
	b.WriteString("\nTRADES\n")
	if t.Count == 0 {
		b.WriteString("  none closed in this window\n")
		return
	}
	fmt.Fprintf(b, "  closed        %d (%d win / %d loss, %.0f%% win rate)\n", t.Count, t.Wins, t.Losses, t.WinRate*100)
	fmt.Fprintf(b, "  expectancy    %s per trade\n", usd(t.ExpectancyUSD))
	fmt.Fprintf(b, "  avg win/loss  %s / %s\n", usd(t.AvgWinUSD), usd(t.AvgLossUSD))
	fmt.Fprintf(b, "  profit factor %s\n", profitFactor(t))
	fmt.Fprintf(b, "  gross / costs / net  %s / %s / %s\n", usd(t.GrossPL), usd(t.Costs), usd(t.NetPL))
}

func writeEquity(b *strings.Builder, rep stats.Report) {
	e := rep.Equity
	b.WriteString("\nEQUITY (the whole record; the window below is only this review's slice)\n")
	fmt.Fprintf(b, "  start / end   %s / %s (%s)\n", usd(e.StartingEquity), usd(e.EndingEquity), pct(e.ReturnPct))
	fmt.Fprintf(b, "  max drawdown  %s (%.1f%%)\n", usd(e.MaxDrawdownUSD), e.MaxDrawdownPct)
	fmt.Fprintf(b, "  this window   %s (%s)\n", usd(e.WindowNetPL), pct(e.WindowReturnPct))
}

func writeSetups(b *strings.Builder, rep stats.Report) {
	b.WriteString("\nBY SETUP\n")
	if len(rep.BySetup) == 0 {
		b.WriteString("  nothing traded or replayed\n")
		return
	}
	for _, s := range rep.BySetup {
		fmt.Fprintf(b, "  %-6s traded %d, expectancy %s, net %s | %s\n",
			s.SetupID, s.Trades.Count, usd(s.Trades.ExpectancyUSD), usd(s.Trades.NetPL), replayLine(s.Counterfactual))
	}
}

func writeRegimes(b *strings.Builder, rep stats.Report) {
	b.WriteString("\nBY REGIME\n")
	if len(rep.ByRegime) == 0 {
		b.WriteString("  no briefing archived a regime in this window\n")
		return
	}
	for _, r := range rep.ByRegime {
		fmt.Fprintf(b, "  %-22s sessions %d, calls %d/%d, notes %d/%d, trades %d net %s\n",
			r.Label, r.Sessions, r.Calls.Correct, r.Calls.Total, r.Notes.Correct, r.Notes.Total,
			r.Trades.Count, usd(r.Trades.NetPL))
	}
}

// writeReads shows both graded predictions: the one call of the day and the
// watchlist notes, which carry the sample size the call cannot.
func writeReads(b *strings.Builder, rep stats.Report) {
	b.WriteString("\nCALLS\n  ")
	b.WriteString(readLine(rep.Calls))
	b.WriteString("\nNOTES\n  ")
	b.WriteString(readLine(rep.Notes))
	b.WriteString("\n")
}

func readLine(c stats.CallStats) string {
	if c.Total == 0 {
		if c.Pending > 0 {
			return fmt.Sprintf("none graded yet, %d pending", c.Pending)
		}
		return "none graded"
	}
	return fmt.Sprintf("%d/%d correct (%.0f%%), %d pending, %d decided inside the noise band",
		c.Correct, c.Total, c.Accuracy*100, c.Pending, c.WithinNoiseBand)
}

func writeProposals(b *strings.Builder, rep stats.Report) {
	p := rep.Proposals
	b.WriteString("\nPROPOSALS\n")
	fmt.Fprintf(b, "  proposed %d, taken %d, passed %d, rejected %d, expired %d, unfilled %d\n",
		p.Proposed, p.Taken, p.Passed, p.Rejected, p.Expired, p.Unfilled)
	fmt.Fprintf(b, "  passes that would have profited  %d (%s left on the table)\n",
		p.PassesThatWouldHaveProfited, usd(p.MissedNetUSD))
	fmt.Fprintf(b, "  losses the vetoes avoided        %s\n", usd(p.VetoedLossesAvoidedUSD))
	fmt.Fprintf(b, "  execution drag on takes          %s (replay minus what was booked)\n", usd(p.ExecutionDragUSD))
	fmt.Fprintf(b, "  %s\n", replayLine(p.Counterfactual))
}

// replayLine summarises replayed proposals. Ambiguous replays are named because
// the stop-first convention decided them, not the bar.
func replayLine(c stats.CounterfactualStats) string {
	if c.Replayed == 0 {
		return "no replays"
	}
	return fmt.Sprintf("replayed %d, filled %d (%d win / %d loss), net %s, avg %.2fR, %d ambiguous",
		c.Replayed, c.Filled, c.Wins, c.Losses, usd(c.NetPL), c.AvgRMultiple, c.Ambiguous)
}

func writeExtremes(b *strings.Builder, in Input) {
	writeTradeLines(b, "BEST TRADES", in.Best)
	writeTradeLines(b, "WORST TRADES", in.Worst)
}

func writeTradeLines(b *strings.Builder, header string, lines []TradeLine) {
	fmt.Fprintf(b, "\n%s\n", header)
	if len(lines) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, l := range lines {
		fmt.Fprintf(b, "  %s  %-6s %-6s %s  %.2fR\n", l.Day, l.Symbol, l.SetupID, usd(l.NetUSD), l.RMultiple)
	}
}

// writePasses puts every veto next to its replay, and fences the reasons: they
// are the trader's own words, not instructions to you.
func writePasses(b *strings.Builder, lines []PassLine) {
	b.WriteString("\nPASSES AND THEIR REPLAYS\n")
	if len(lines) == 0 {
		b.WriteString("  nothing was passed on in this window\n")
		return
	}
	for _, l := range lines {
		fmt.Fprintf(b, "  %s  %-6s %-6s %s\n", l.Day, l.Symbol, l.SetupID, replayResult(l))
	}
	fmt.Fprintf(b, "%s\n", reasonsOpen)
	for _, l := range lines {
		fmt.Fprintf(b, "  %s %s: %s\n", l.Day, l.Symbol, clipRunes(dataText(l.Reason), summaryRunes))
	}
	fmt.Fprintf(b, "%s\n", reasonsClose)
}

func replayResult(l PassLine) string {
	switch {
	case !l.Replayed:
		return "not replayed yet"
	case !l.Filled:
		return "the entry never filled"
	case l.Ambiguous:
		return fmt.Sprintf("%s at %s, %.2fR (one bar spanned both levels; the stop-first rule decided it)",
			l.ExitKind, usd(l.NetUSD), l.RMultiple)
	default:
		return fmt.Sprintf("%s at %s, %.2fR", l.ExitKind, usd(l.NetUSD), l.RMultiple)
	}
}

func writeRefusals(b *strings.Builder, lines []RefusalLine) {
	b.WriteString("\nREFUSALS\n")
	if len(lines) == 0 {
		b.WriteString("  no guardrail had to say no\n")
		return
	}
	for _, l := range lines {
		fmt.Fprintf(b, "  %-28s %d\n", l.Rule, l.Count)
	}
}

// writeGate shows where the record stands against the real-money threshold, and
// which slice of it the gate is allowed to read.
func writeGate(b *strings.Builder, rep stats.Report) {
	b.WriteString("\nGATE (the whole record, from the last rule change onward)\n")
	if rep.GateResetAt == nil {
		b.WriteString("  reading       the whole record; the rules have not moved\n")
	} else {
		fmt.Fprintf(b, "  reading from  %s, when the playbook or the config last changed\n", rep.GateResetAt.Format(dayLayout))
	}
	for _, c := range rep.GateChecks {
		mark := "no"
		if c.Passed {
			mark = "yes"
		}
		fmt.Fprintf(b, "  %-24s %-28s needs %-22s %s\n", c.Name, c.Actual, c.Needed, mark)
	}
	s := rep.Significance
	if s.Paths == 0 {
		b.WriteString("  significance: too few trades to build a null from\n")
		return
	}
	fmt.Fprintf(b, "  significance: a zero-edge trader with this record's shape clears the thresholds %.1f%% of the time over %d paths; break-even win rate %.1f%%; expectancy 95%% lower bound %s\n",
		s.NullPassRate*100, s.Paths, s.NullWinRate*100, usd(s.ExpectancyCI95Low))
}

// writeLimits restates the walls so the model's edits plan inside them. It cannot
// move them; they are enforced in code.
func writeLimits(b *strings.Builder, l risk.Limits) {
	b.WriteString("\nRISK LIMITS (enforced in code; you may not edit the \"## Risk rules\" section)\n")
	fmt.Fprintf(b, "  per trade      %s%% of equity, lost at the stop\n", num(l.PerTradePct))
	if l.RequireStop {
		b.WriteString("  stop           required on every entry\n")
	}
	if l.MaxPositions > 0 {
		fmt.Fprintf(b, "  max positions  %d\n", l.MaxPositions)
	}
	if l.MaxDailyLosses > 0 {
		fmt.Fprintf(b, "  daily halt     after %d losing trades\n", l.MaxDailyLosses)
	}
	if l.MinRewardRisk > 0 {
		fmt.Fprintf(b, "  reward/risk    %sR or better\n", num(l.MinRewardRisk))
	}
	if l.MaxEntryDeviationPct > 0 {
		fmt.Fprintf(b, "  entry          within %s%% of the last price\n", num(l.MaxEntryDeviationPct))
	}
}

func writePrevious(b *strings.Builder, summary string) {
	if strings.TrimSpace(summary) == "" {
		return
	}
	fmt.Fprintf(b, "\n%s\n%s\n%s\n", previousOpen, clipRunes(dataText(summary), summaryRunes), previousClose)
}

func writeSetupIDs(b *strings.Builder, ids []string) {
	b.WriteString("\nSETUP IDS THE PLAYBOOK DEFINES\n  ")
	if len(ids) == 0 {
		b.WriteString("none\n")
		return
	}
	b.WriteString(strings.Join(ids, ", ") + "\n")
}

// profitFactor reads the field the way the gate does: with no losing trade the
// ratio has no divisor and the number carries gross profit instead.
func profitFactor(t stats.TradeStats) string {
	if t.Losses == 0 {
		return "no losses"
	}
	return fmt.Sprintf("%.2f", t.ProfitFactor)
}
