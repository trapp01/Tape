package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/trapp01/tape/internal/config"
)

const (
	ansiReset = "\x1b[0m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiDim   = "\x1b[2m"
)

// styler decides whether output carries colour. Escape bytes count toward a
// tabwriter cell's width, so colour only ever goes in a row's last cell.
type styler struct{ color bool }

func newStyler(w io.Writer) styler {
	return styler{color: isTerminal(w) && os.Getenv("NO_COLOR") == ""}
}

// isTerminal reports whether w is a character device, which is the only place
// escape codes belong.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// banner is the first line of every command: the mode tag, then a headline. Live
// stays locked until the real-money gate opens, so the tag never claims an
// account tape is not allowed to trade.
func (s styler) banner(w io.Writer, mode, headline string) {
	tag := "[paper]"
	if mode == config.ModeLive {
		tag = "[LIVE — locked]"
	}
	if headline == "" {
		fmt.Fprintln(w, tag)
		return
	}
	fmt.Fprintln(w, tag, headline)
}

// pl colours text green or red by the sign of v. It must be the last cell in a
// tabwriter row.
func (s styler) pl(v float64, text string) string {
	switch {
	case !s.color || v == 0:
		return text
	case v > 0:
		return ansiGreen + text + ansiReset
	default:
		return ansiRed + text + ansiReset
	}
}

func (s styler) dim(text string) string {
	if !s.color {
		return text
	}
	return ansiDim + text + ansiReset
}

// money renders a dollar amount as $1,234.56, negatives with a leading minus. An
// amount that rounds to nothing loses its sign rather than printing -$0.00.
func money(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := strconv.FormatFloat(v, 'f', 2, 64)
	if digits == "0.00" {
		sign = ""
	}
	whole, frac, _ := strings.Cut(digits, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + "$" + b.String() + "." + frac
}

// signedMoney keeps the plus so a gain reads as a gain in a column of numbers.
func signedMoney(v float64) string {
	m := money(v)
	if v > 0 && m != "$0.00" {
		return "+" + m
	}
	return m
}

func percent(v float64) string { return fmt.Sprintf("%+.2f%%", v) }

// percent1 is the coarser form for a scanning column, where a second decimal is
// noise.
func percent1(v float64) string { return fmt.Sprintf("%+.1f%%", v) }

// tokens abbreviates a model's token count: 41200 reads as 41.2k.
func tokens(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
}

// humanDuration is a rough gap between two clock times: minutes until the bell,
// seconds a model took. Precision past the leading unit is not information here.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "under a second"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// oneLine flattens model or feed text so one thought stays one row.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// wrap breaks text on spaces at width runes. Empty text produces no lines, so a
// section with nothing to say prints nothing.
func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var (
		lines []string
		line  []string
		n     int
	)
	for _, w := range words {
		runes := len([]rune(w))
		if len(line) > 0 && n+1+runes > width {
			lines = append(lines, strings.Join(line, " "))
			line, n = nil, 0
		}
		if len(line) > 0 {
			n++
		}
		line = append(line, w)
		n += runes
	}
	return append(lines, strings.Join(line, " "))
}

// price is a raw quote, which is not money the account holds.
func price(v float64) string { return "$" + strconv.FormatFloat(v, 'f', 2, 64) }

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

// row writes one tab-separated line into a tabwriter.
func row(tw *tabwriter.Writer, cells ...string) {
	fmt.Fprintln(tw, strings.Join(cells, "\t"))
}

// pair prints a label and value in a two-column block; the value is last so a
// coloured one stays aligned.
func pair(tw *tabwriter.Writer, label, value string) {
	fmt.Fprintf(tw, "  %s\t%s\n", label, value)
}

func stamp(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}

func shortStamp(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("01-02 15:04")
}
