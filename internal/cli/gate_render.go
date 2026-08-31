package cli

import (
	"fmt"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/stats"
)

// renderSignificance answers the only question a threshold cannot: could noise
// have produced this record at this sample size.
func renderSignificance(a *app, rep stats.Report) {
	s := rep.Significance
	fmt.Fprintln(a.out, "\nSIGNIFICANCE")
	if s.Paths == 0 {
		fmt.Fprintln(a.out, "  too few trades to build a zero-edge trader from.")
		return
	}
	tw := table(a.out)
	pair(tw, "break-even win rate", fmt.Sprintf("%.1f%%", s.NullWinRate*100))
	pair(tw, "a zero-edge trader clears the thresholds", fmt.Sprintf("%.1f%% of %d paths", s.NullPassRate*100, s.Paths))
	pair(tw, "expectancy, 95% lower bound", a.style.pl(s.ExpectancyCI95Low, signedMoney(s.ExpectancyCI95Low)+" per trade"))
	tw.Flush()
}

// renderGate prints where the record stands against every threshold, and names
// the playbook snapshot the window is allowed to start at.
func renderGate(a *app, rep stats.Report, v *journal.PlaybookVersion) {
	fmt.Fprintln(a.out, "\nGATE")
	fmt.Fprintf(a.out, "  reading from %s\n", rep.GateWindowFrom.Format(dayLayout))
	if rep.GateResetAt != nil && v != nil {
		fmt.Fprintln(a.out, a.style.dim(fmt.Sprintf(
			"  playbook version #%d (%s) was recorded that day; the gate reads only what came after it, so a rule fitted to the record is never graded on the record that produced it.",
			v.ID, v.Note)))
	}
	if len(rep.GateChecks) == 0 {
		fmt.Fprintln(a.out, "  nothing to check yet.")
		return
	}

	tw := table(a.out)
	row(tw, "  CHECK", "ACTUAL", "NEEDED", "")
	for _, c := range rep.GateChecks {
		row(tw, "  "+c.Name, c.Actual, c.Needed, a.style.pl(boolSign(c.Passed), mark(c.Passed)))
	}
	tw.Flush()

	if rep.GateOpen {
		fmt.Fprintln(a.out, "\nevery check passes. tape still trades paper; going live is a decision, not a threshold.")
		return
	}
	fmt.Fprintln(a.out, "\nthe gate is shut. tape trades paper until every line above reads ✓.")
}
