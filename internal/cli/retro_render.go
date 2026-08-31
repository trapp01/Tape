package cli

import (
	"fmt"
	"strconv"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/retro"
)

// diffTextWidth is how wide the quoted playbook text wraps under a diff.
const diffTextWidth = 88

// renderRetro prints a review: what it concluded, what the numbers behind that
// were, and the exact edits it proposes. Nothing here changes a file.
func renderRetro(a *app, r journal.Retro, out retro.Output, diffs []journal.RetroDiff) {
	fmt.Fprintf(a.out, "\nreview #%d · %s → %s · %s (%s) · %s in / %s out\n",
		r.ID, r.FromDay, r.ToDay, r.Model, r.Provider, tokens(r.InputTokens), tokens(r.OutputTokens))

	fmt.Fprintln(a.out, "\nSUMMARY")
	for _, line := range wrap(oneLine(out.Summary), diffTextWidth) {
		fmt.Fprintf(a.out, "  %s\n", line)
	}

	renderFindings(a, out.Findings)
	renderDiffs(a, diffs)
}

func renderFindings(a *app, findings []retro.Finding) {
	fmt.Fprintln(a.out, "\nFINDINGS")
	if len(findings) == 0 {
		fmt.Fprintln(a.out, "  none; the record did not support one.")
		return
	}
	for i, f := range findings {
		fmt.Fprintf(a.out, "  %d. %s  %s\n", i+1, oneLine(f.Title), a.style.dim("("+f.Confidence+" confidence)"))
		for _, line := range wrap(oneLine(f.Evidence), diffTextWidth-5) {
			fmt.Fprintf(a.out, "     %s\n", line)
		}
	}
}

// renderDiffs shows each proposed edit with the text it would replace, so the
// trader reads the change before accepting it.
func renderDiffs(a *app, diffs []journal.RetroDiff) {
	fmt.Fprintln(a.out, "\nPLAYBOOK DIFFS")
	if len(diffs) == 0 {
		fmt.Fprintln(a.out, "  none. an empty list is a real answer: the record could not carry a change.")
		return
	}
	for _, d := range diffs {
		fmt.Fprintf(a.out, "  %d. %s under %s%s\n", d.Index, d.Change, oneLine(d.Section), appliedSuffix(a, d))
		fmt.Fprintf(a.out, "     why: %s\n", oneLine(d.Rationale))
		quote(a, "     - ", d.Before)
		quote(a, "     + ", d.After)
	}
	fmt.Fprintf(a.out, "act: tape retro apply %d --diff 1 · --all\n", diffs[0].RetroID)
}

// quote prints one side of a diff, wrapped, or nothing when that side is empty.
func quote(a *app, prefix, text string) {
	if oneLine(text) == "" {
		return
	}
	for _, line := range wrap(oneLine(text), diffTextWidth-len(prefix)) {
		fmt.Fprintf(a.out, "%s%s\n", prefix, line)
	}
}

func appliedSuffix(a *app, d journal.RetroDiff) string {
	if d.AppliedAt == nil {
		return ""
	}
	return "   " + a.style.dim("applied "+shortStamp(*d.AppliedAt, a.loc))
}

// renderApplied says exactly what changed on disk and what the gate now reads.
func renderApplied(a *app, report retro.AppliedReport) {
	fmt.Fprintf(a.out, "\napplied %d edit(s) to %s\n", len(report.Applied), report.Path)
	for _, d := range report.Applied {
		fmt.Fprintf(a.out, "  %d. %s under %s\n", d.Index, d.Change, oneLine(d.Section))
	}
	tw := table(a.out)
	pair(tw, "previous playbook", report.HistoryPath)
	pair(tw, "version", "#"+strconv.FormatInt(report.Version.ID, 10)+" "+shortSHA(report.Version.SHA256))
	tw.Flush()
	fmt.Fprintln(a.out, a.style.dim(
		"the gate now reads only the sessions from here on; what came before was traded under the old rules."))
}

// shortSHA is the leading twelve characters, which is enough to tell two
// snapshots apart by eye.
func shortSHA(sum string) string {
	if len(sum) <= 12 {
		return sum
	}
	return sum[:12]
}

// renderVersions lists the playbook snapshots newest first.
func renderVersions(a *app, versions []journal.PlaybookVersion) {
	if len(versions) == 0 {
		fmt.Fprintln(a.out, "\nno playbook versions yet; one is recorded the first time a command reads the record.")
		return
	}
	fmt.Fprintln(a.out)
	tw := table(a.out)
	row(tw, "ID", "TAKEN", "SHA", "REVIEW", "NOTE")
	for _, v := range versions {
		review := "-"
		if v.RetroID != nil {
			review = "#" + strconv.FormatInt(*v.RetroID, 10)
		}
		row(tw, "#"+strconv.FormatInt(v.ID, 10), shortStamp(v.CreatedAt, a.loc), shortSHA(v.SHA256), review, v.Note)
	}
	tw.Flush()
}
