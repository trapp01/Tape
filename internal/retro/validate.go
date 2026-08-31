package retro

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/trapp01/tape/internal/journal"
)

// confidences are the three readings a finding may carry.
var confidences = []string{"low", "medium", "high"}

// Validate range-checks a reply in Go and resolves every diff against the
// playbook it was written from. The returned text is what the playbook would
// become; Run throws it away and Apply keeps it.
func Validate(playbookText string, out Output) (string, error) {
	if strings.TrimSpace(out.Summary) == "" {
		return "", errors.New("retro: the summary is empty")
	}
	if len(out.Findings) > MaxFindings {
		return "", fmt.Errorf("retro: %d findings, at most %d allowed", len(out.Findings), MaxFindings)
	}
	if len(out.Diffs) > MaxDiffs {
		return "", fmt.Errorf("retro: %d diffs, at most %d allowed", len(out.Diffs), MaxDiffs)
	}
	for i, f := range out.Findings {
		if err := validateFinding(i, f); err != nil {
			return "", err
		}
	}
	for i, d := range out.Diffs {
		if strings.TrimSpace(d.Rationale) == "" {
			return "", fmt.Errorf("retro: diff %d (%s) has no rationale", i+1, oneLine(d.Section))
		}
	}
	return applyAll(playbookText, out.Diffs)
}

func validateFinding(i int, f Finding) error {
	if strings.TrimSpace(f.Title) == "" {
		return fmt.Errorf("retro: finding %d has no title", i+1)
	}
	if strings.TrimSpace(f.Evidence) == "" {
		return fmt.Errorf("retro: finding %d (%s) cites no numbers", i+1, oneLine(f.Title))
	}
	if !slices.Contains(confidences, f.Confidence) {
		return fmt.Errorf("retro: finding %d (%s): confidence %q is not one of %s",
			i+1, oneLine(f.Title), f.Confidence, strings.Join(confidences, ", "))
	}
	return nil
}

// diffRows turns the validated reply into the rows the journal stores, numbered
// in the order the model made them.
func diffRows(out Output) []*journal.RetroDiff {
	rows := make([]*journal.RetroDiff, 0, len(out.Diffs))
	for _, d := range out.Diffs {
		rows = append(rows, &journal.RetroDiff{
			Section: d.Section, Change: d.Change, Rationale: d.Rationale,
			Before: d.Before, After: d.After,
		})
	}
	return rows
}
