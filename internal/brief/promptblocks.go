package brief

import (
	"fmt"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/market"
)

// The blocks below render one Input section each. Anything a venue or a news
// wire wrote goes inside a fenced block; everything else is tape's own text.

func sessionLine(in Input, loc *time.Location) string {
	state := "closed"
	if in.MarketOpen {
		state = "open"
	}
	parts := []string{state}
	if !in.NextOpen.IsZero() {
		parts = append(parts, "next open "+stamp(in.NextOpen, loc))
	}
	if !in.NextClose.IsZero() {
		parts = append(parts, "next close "+stamp(in.NextClose, loc))
	}
	return strings.Join(parts, ", ")
}

func writeSymbols(b *strings.Builder, header string, reads []SymbolRead) {
	fmt.Fprintf(b, "\n%s\n", header)
	if len(reads) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, r := range reads {
		fmt.Fprintf(b, "  %-6s last %.2f  prev close %.2f  %+.2f%%\n", r.Symbol, r.Last, r.PrevClose, r.ChangePct)
	}
}

// writeHeadlines fences every story the news wire wrote into one block, so the
// untrusted text has a visible start and end. Market news scales with the
// per-symbol allowance: the full 15 at 5 each.
func writeHeadlines(b *strings.Builder, in Input, limit int, loc *time.Location) {
	if limit == 0 {
		return
	}
	var body strings.Builder
	for _, r := range in.Watchlist {
		if len(r.Headlines) == 0 {
			continue
		}
		fmt.Fprintf(&body, "%s\n", r.Symbol)
		writeHeadlineList(&body, r.Headlines, limit, loc)
	}
	if len(in.MarketHeadlines) > 0 {
		body.WriteString("MARKET\n")
		writeHeadlineList(&body, in.MarketHeadlines, limit*3, loc)
	}
	if body.Len() == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n%s%s\n", headlinesOpen, body.String(), headlinesClose)
}

func writeHeadlineList(b *strings.Builder, stories []market.Headline, limit int, loc *time.Location) {
	for i, h := range stories {
		if i >= limit {
			return
		}
		writeHeadline(b, h, "  ", loc)
	}
}

func writeHeadline(b *strings.Builder, h market.Headline, indent string, loc *time.Location) {
	source := h.Source
	if source == "" {
		source = "unattributed"
	}
	fmt.Fprintf(b, "%s- %s  %s (%s)\n", indent, stamp(h.CreatedAt, loc), oneLine(h.Headline), oneLine(source))
	if s := oneLine(h.Summary); s != "" {
		fmt.Fprintf(b, "%s  %s\n", indent, s)
	}
}

func writeCalendar(b *strings.Builder, in Input, loc *time.Location) {
	b.WriteString("\nCALENDAR\n")
	if len(in.Calendar) == 0 {
		b.WriteString("  nothing scheduled\n")
		return
	}
	for _, e := range in.Calendar {
		fmt.Fprintf(b, "  %-20s  %s (%s, %s impact)\n", eventStamp(e, loc), e.Title, e.Kind, e.Impact)
		if d := oneLine(e.Detail); d != "" {
			fmt.Fprintf(b, "      %s\n", d)
		}
	}
}

func eventStamp(e calendar.Event, loc *time.Location) string {
	if e.AllDay {
		return e.At.In(loc).Format(dayLayout) + " all day"
	}
	return stamp(e.At, loc)
}

func writeMovers(b *strings.Builder, in Input) {
	if len(in.Gainers) == 0 && len(in.Losers) == 0 && len(in.Actives) == 0 {
		return
	}
	b.WriteString("\nMOVERS\n")
	writeMoverLine(b, "gainers", in.Gainers)
	writeMoverLine(b, "losers", in.Losers)
	if len(in.Actives) > 0 {
		parts := make([]string, 0, len(in.Actives))
		for _, a := range in.Actives {
			parts = append(parts, fmt.Sprintf("%s %.0f", a.Symbol, a.Volume))
		}
		fmt.Fprintf(b, "  actives  %s\n", strings.Join(parts, ", "))
	}
}

func writeMoverLine(b *strings.Builder, label string, movers []market.Mover) {
	if len(movers) == 0 {
		return
	}
	parts := make([]string, 0, len(movers))
	for _, m := range movers {
		parts = append(parts, fmt.Sprintf("%s %+.2f%% at %.2f", m.Symbol, m.PercentChg, m.Price))
	}
	fmt.Fprintf(b, "  %-8s %s\n", label, strings.Join(parts, ", "))
}

// writeWarnings fences the warnings too: most carry a provider's own words. The
// prompt gets the gist; Input.Warnings keeps the whole thing for the archive.
func writeWarnings(b *strings.Builder, in Input) {
	if len(in.Warnings) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\nthis briefing was written without these sources\n", warningsOpen)
	for _, w := range in.Warnings {
		fmt.Fprintf(b, "  - %s\n", clipRunes(oneLine(w), warningRunes))
	}
	fmt.Fprintf(b, "%s\n", warningsClose)
}

func stamp(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04 MST")
}

// oneLine flattens a feed string so one story stays one row.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// clipRunes cuts s to at most n runes, marking where it stopped.
func clipRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
