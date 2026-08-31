package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

const (
	dayLayout = "2006-01-02"
	// labelWidth is the gutter every section header sits in; continuation lines
	// line up under the content, not the label.
	labelWidth = 8
	// moversShown caps each side of the movers line so it stays one line.
	moversShown = 5
)

// renderBriefing prints the morning read: computed facts first, the model's call
// second, and what the briefing was written without last. withModel is false for
// a dry run, which has facts but no reply.
func renderBriefing(a *app, res brief.Result, withModel bool) {
	in, out := res.Input, res.Output
	if !withModel {
		out = brief.Output{}
	}

	fmt.Fprintln(a.out, briefHeader(in, a.loc))
	renderMarket(a, in, out.MarketRead)
	renderRegime(a, in, out.RegimeNote)
	if withModel {
		renderCall(a, res)
		renderProposals(a, res)
	}
	renderCalendar(a, in, a.loc, out.CalendarNote)
	renderWatchlist(a, in, out.Watchlist)
	renderMovers(a, in)
	bulleted(a, "RISKS", "• ", out.Risks)
	bulleted(a, "SOURCES", "", in.Warnings)
	if withModel {
		renderFooter(a, res)
	}
}

func briefHeader(in brief.Input, loc *time.Location) string {
	at := in.GeneratedAt.In(loc)
	return strings.Join([]string{
		"TAPE",
		at.Format("Mon Jan 2"),
		at.Format("15:04 MST"),
		sessionPhrase(in, loc),
		"cash " + money(in.LedgerCash),
	}, " · ")
}

// sessionPhrase says how long until the bell. A session on another day gets its
// weekday, because "opens in 49h" is not something anyone reads as Monday.
func sessionPhrase(in brief.Input, loc *time.Location) string {
	now := in.GeneratedAt
	switch {
	case in.MarketOpen && in.NextClose.After(now):
		return "market open, closes in " + humanDuration(in.NextClose.Sub(now))
	case in.MarketOpen:
		return "market open"
	case in.NextOpen.IsZero() || !in.NextOpen.After(now):
		return "market closed"
	case in.NextOpen.In(loc).Format(dayLayout) != now.In(loc).Format(dayLayout):
		return "market opens " + in.NextOpen.In(loc).Format("Mon 15:04 MST")
	default:
		return "market opens in " + humanDuration(in.NextOpen.Sub(now))
	}
}

func renderMarket(a *app, in brief.Input, read string) {
	parts := make([]string, 0, len(in.Indexes))
	for _, idx := range in.Indexes {
		parts = append(parts, idx.Symbol+" "+a.style.pl(idx.ChangePct, percent(idx.ChangePct)))
	}
	if len(parts) == 0 {
		parts = append(parts, "no quotes")
	}
	label(a, "MARKET", strings.Join(parts, "  "))
	continuation(a, read)
}

func renderRegime(a *app, in brief.Input, note string) {
	summary := in.Regime.Summary
	if summary == "" {
		summary = "not classified"
	}
	label(a, "REGIME", summary)
	continuation(a, note)
}

func renderCall(a *app, res brief.Result) {
	c := res.Output.Call
	threshold := a.cfg.Brief.CallThresholdPct
	if c.ThresholdPct != nil {
		threshold = *c.ThresholdPct
	}
	label(a, "CALL", fmt.Sprintf("%s %s   %s", c.Instrument, callPhrase(c.Direction, threshold), callVerdict(a, res.Call)))
	continuation(a, c.Rationale)
	if c.Invalidation != "" {
		continuation(a, "invalid if: "+c.Invalidation)
	}
}

// callPhrase spells out what would make the call right, so the reader grades it
// the same way the scorer will.
func callPhrase(dir brief.Direction, threshold float64) string {
	switch dir {
	case brief.DirFlat:
		return fmt.Sprintf("flat within ±%.2g%% open→close", threshold)
	case brief.DirDown:
		return fmt.Sprintf("down ≥%.2g%% open→close", threshold)
	default:
		return fmt.Sprintf("up ≥%.2g%% open→close", threshold)
	}
}

func callVerdict(a *app, c *journal.Call) string {
	if c == nil {
		return a.style.dim("[no call filed]")
	}
	if c.Correct == nil || c.ActualPct == nil {
		return a.style.dim("[scored after close]")
	}
	mark := "✗"
	if *c.Correct {
		mark = "✓"
	}
	return a.style.pl(*c.ActualPct, fmt.Sprintf("[%s %s]", mark, percent(*c.ActualPct)))
}

func renderCalendar(a *app, in brief.Input, loc *time.Location, note string) {
	fmt.Fprintln(a.out, "CALENDAR")
	if len(in.Calendar) == 0 {
		fmt.Fprintln(a.out, "  nothing scheduled")
	}
	today := in.GeneratedAt.In(loc).Format(dayLayout)
	for _, e := range in.Calendar {
		fmt.Fprintf(a.out, "  %-9s %s (%s)\n", eventClock(e.At, e.AllDay, today, loc), e.Title, e.Impact)
	}
	continuation(a, note)
}

func eventClock(at time.Time, allDay bool, today string, loc *time.Location) string {
	local := at.In(loc)
	switch {
	case allDay && local.Format(dayLayout) == today:
		return "all day"
	case allDay:
		return local.Format("Mon") + " all day"
	case local.Format(dayLayout) == today:
		return local.Format("15:04")
	default:
		return local.Format("Mon 15:04")
	}
}

func renderWatchlist(a *app, in brief.Input, notes []brief.WatchNote) {
	fmt.Fprintln(a.out, "WATCHLIST")
	if len(in.Watchlist) == 0 {
		fmt.Fprintln(a.out, "  nothing on the watchlist")
		return
	}
	bySymbol := make(map[string]brief.WatchNote, len(notes))
	for _, n := range notes {
		bySymbol[strings.ToUpper(n.Symbol)] = n
	}

	tw := table(a.out)
	for _, w := range in.Watchlist {
		n := bySymbol[w.Symbol]
		row(tw, "  "+w.Symbol, percent1(w.ChangePct), n.Bias, oneLine(n.Note))
	}
	tw.Flush()
}

func renderMovers(a *app, in brief.Input) {
	gainers, losers := moverList(in.Gainers), moverList(in.Losers)
	if gainers == "" && losers == "" {
		label(a, "MOVERS", "unavailable")
		return
	}
	label(a, "MOVERS", "gainers: "+dash(gainers)+"   losers: "+dash(losers))
}

func moverList(movers []market.Mover) string {
	parts := make([]string, 0, moversShown)
	for i, m := range movers {
		if i >= moversShown {
			break
		}
		parts = append(parts, m.Symbol+" "+percent1(m.PercentChg))
	}
	return strings.Join(parts, "  ")
}

// bulleted prints a list under one label: the first item shares the header line,
// the rest sit under it. An empty list prints nothing.
func bulleted(a *app, name, bullet string, items []string) {
	for i, item := range items {
		text := bullet + oneLine(item)
		if i == 0 {
			label(a, name, text)
			continue
		}
		continuation(a, text)
	}
}

func renderFooter(a *app, res brief.Result) {
	b := res.Briefing
	if res.Reused {
		fmt.Fprintf(a.out, "%s\n", a.style.dim(fmt.Sprintf(
			"archived %s (#%d); use --force to re-run", archivedWhen(b.GeneratedAt, a.loc), b.ID)))
		return
	}
	parts := []string{
		fmt.Sprintf("briefing #%d", b.ID),
		b.Model,
		fmt.Sprintf("%s in / %s out", tokens(b.InputTokens), tokens(b.OutputTokens)),
		humanDuration(time.Duration(b.LatencyMs) * time.Millisecond),
	}
	if b.CostUSD != nil {
		parts = append(parts, "est. "+money(*b.CostUSD))
	}
	fmt.Fprintln(a.out, a.style.dim(strings.Join(parts, " · ")))

	switch {
	case res.CallReplaced:
		fmt.Fprintln(a.out, a.style.dim("replaced the earlier call and expired the earlier slate; both lock at 09:30 ET."))
	case res.CallKept:
		fmt.Fprintln(a.out, a.style.dim("the session's first call and slate stand; both lock at 09:30 ET, so this briefing is a second read, not a second prediction."))
	}
}

// archivedWhen says when the reused briefing was written. An evening briefing is
// picked up the next morning, where "earlier today" would be a lie.
func archivedWhen(at time.Time, loc *time.Location) string {
	local := at.In(loc)
	if local.Format(dayLayout) == timeNow().In(loc).Format(dayLayout) {
		return "earlier today"
	}
	return local.Format("Mon 15:04")
}

// label writes a section line; continuation puts a wrapped follow-on under the
// content column.
func label(a *app, name, content string) {
	fmt.Fprintf(a.out, "%-*s %s\n", labelWidth, name, content)
}

func continuation(a *app, text string) {
	for _, line := range wrap(oneLine(text), 80) {
		fmt.Fprintf(a.out, "%*s %s\n", labelWidth, "", line)
	}
}
