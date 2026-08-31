package retro

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The helpers below format one value for the prompt. Anything somebody else
// wrote goes through dataText on its way into a fenced block.
func usd(v float64) string {
	if v < 0 {
		return fmt.Sprintf("-$%.2f", -v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func pct(v float64) string { return fmt.Sprintf("%+.2f%%", v) }

// num renders a limit without trailing zeroes, so 0.5 reads as "0.5".
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// oneLine flattens free text so one thought stays one row.
func oneLine(s string) string { return strings.TrimSpace(strings.Join(strings.Fields(s), " ")) }

// fenceRun matches the "=" runs the block markers are built from.
var fenceRun = regexp.MustCompile(`={2,}`)

// dataText prepares somebody else's words for a fenced block: one row, with
// every "=" run collapsed so the text cannot close the block it sits in.
func dataText(s string) string { return fenceRun.ReplaceAllString(oneLine(s), "=") }

// clipRunes cuts s to at most n runes, marking where it stopped.
func clipRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
