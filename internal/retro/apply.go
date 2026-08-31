package retro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// historyDir holds every playbook the desk has replaced, so an old stat can be
// read next to the rules that produced it.
const historyDir = "playbook.history"

// AppliedReport is what one apply did to the strategy file.
type AppliedReport struct {
	Path        string
	HistoryPath string
	Version     journal.PlaybookVersion
	Applied     []journal.RetroDiff
}

// Apply rewrites the playbook with the chosen diffs. Every diff is resolved
// against the file as it stands now, not as it stood when the review was written,
// and a diff the playbook already carries is refused.
func Apply(ctx context.Context, d Deps, retroID int64, indexes []int) (AppliedReport, error) {
	if d.Journal == nil {
		return AppliedReport{}, errors.New("retro: apply: no journal configured")
	}
	if d.PlaybookPath == "" {
		return AppliedReport{}, errors.New("retro: apply: no playbook path configured")
	}
	if len(indexes) == 0 {
		return AppliedReport{}, fmt.Errorf("retro: apply review #%d: no diffs chosen", retroID)
	}

	rows, err := d.Journal.DiffsByRetro(ctx, retroID)
	if err != nil {
		return AppliedReport{}, err
	}
	if len(rows) == 0 {
		return AppliedReport{}, fmt.Errorf("retro: review #%d proposed no changes", retroID)
	}
	chosen, err := chooseDiffs(retroID, rows, indexes)
	if err != nil {
		return AppliedReport{}, err
	}

	previous, err := os.ReadFile(d.PlaybookPath)
	if err != nil {
		return AppliedReport{}, fmt.Errorf("retro: reading %s: %w", d.PlaybookPath, err)
	}
	text, err := applyAll(string(previous), diffsOf(chosen))
	if err != nil {
		return AppliedReport{}, err
	}

	// The record commits before the file moves. A crash the other way round would
	// leave edits nothing marks as landed, and the next apply would add them twice.
	at := d.now().UTC()
	report := AppliedReport{Path: d.PlaybookPath, Applied: chosen}
	staged, err := stage(d.PlaybookPath, text)
	if err != nil {
		return AppliedReport{}, err
	}
	defer os.Remove(staged)

	if err := recordVersion(ctx, d, &report, retroID, text, at); err != nil {
		return AppliedReport{}, err
	}
	if report.HistoryPath, err = writeHistory(d.PlaybookPath, previous, at); err != nil {
		return report, err
	}
	if err := os.Rename(staged, d.PlaybookPath); err != nil {
		return report, fmt.Errorf("retro: moving %s onto %s: %w", staged, d.PlaybookPath, err)
	}
	return report, nil
}

// recordVersion files the snapshot the edits produced and marks every diff as
// landed, in one transaction. A diff the playbook already carries fails all of
// them, so the file the caller is about to move is the one this row describes.
func recordVersion(ctx context.Context, d Deps, report *AppliedReport, retroID int64, text string, at time.Time) error {
	configHash, err := ConfigHash(d.Cfg)
	if err != nil {
		return err
	}
	// The journal stamps CreatedAt, the way EnsureVersion's snapshots are stamped.
	// Two clocks here would let an older row sort as the newest version.
	v := journal.PlaybookVersion{
		SHA256:     sha256Hex([]byte(text)),
		Path:       d.PlaybookPath,
		RetroID:    &retroID,
		Note:       fmt.Sprintf("applied %d diff(s) from review #%d", len(report.Applied), retroID),
		ConfigHash: configHash,
	}
	ids := make([]int64, len(report.Applied))
	for i := range report.Applied {
		ids[i] = report.Applied[i].ID
	}
	if err := d.Journal.ApplyRetroDiffs(ctx, &v, ids, at); err != nil {
		return err
	}
	report.Version = v
	for i := range report.Applied {
		report.Applied[i].AppliedAt = &at
		report.Applied[i].VersionID = &v.ID
	}
	return nil
}

// stage writes the new playbook beside the old one so the swap is a rename: a
// reader never sees a half-written strategy file.
func stage(path, text string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("retro: staging a new %s: %w", path, err)
	}
	name := f.Name()
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(name)
		return "", fmt.Errorf("retro: writing %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("retro: writing %s: %w", name, err)
	}
	// CreateTemp opens at 0600; the rename carries that onto the playbook.
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("retro: setting permissions on %s: %w", name, err)
	}
	return name, nil
}

// chooseDiffs resolves the numbers the trader typed against the review's own
// list, in the order the model made them. A diff the playbook already carries is
// refused: an edit lands once.
func chooseDiffs(retroID int64, rows []journal.RetroDiff, indexes []int) ([]journal.RetroDiff, error) {
	byIndex := make(map[int]journal.RetroDiff, len(rows))
	for _, r := range rows {
		byIndex[r.Index] = r
	}
	seen := make(map[int]bool, len(indexes))
	chosen := make([]journal.RetroDiff, 0, len(indexes))
	for _, n := range indexes {
		if seen[n] {
			continue
		}
		seen[n] = true
		r, ok := byIndex[n]
		if !ok {
			return nil, fmt.Errorf("retro: review #%d has no diff %d; it proposed %d", retroID, n, len(rows))
		}
		if r.AppliedAt != nil {
			return nil, fmt.Errorf("retro: diff %d of review #%d was applied at %s; an edit lands once",
				n, retroID, r.AppliedAt.Format(time.RFC3339))
		}
		chosen = append(chosen, r)
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].Index < chosen[j].Index })
	return chosen, nil
}

func diffsOf(rows []journal.RetroDiff) []Diff {
	out := make([]Diff, 0, len(rows))
	for _, r := range rows {
		out = append(out, Diff{
			Section: r.Section, Change: r.Change, Rationale: r.Rationale,
			Before: r.Before, After: r.After,
		})
	}
	return out
}

// writeHistory keeps the playbook that is being replaced, named for the moment it
// stopped being in force.
func writeHistory(path string, previous []byte, at time.Time) (string, error) {
	dir := filepath.Join(filepath.Dir(path), historyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("retro: creating %s: %w", dir, err)
	}
	out := freeName(dir, at.UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(out, previous, 0o600); err != nil {
		return "", fmt.Errorf("retro: writing %s: %w", out, err)
	}
	return out, nil
}

// freeName numbers a second snapshot taken in the same second rather than
// overwriting the first.
func freeName(dir, stem string) string {
	name := filepath.Join(dir, stem+".md")
	for n := 2; n < 100; n++ {
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			return name
		}
		name = filepath.Join(dir, stem+"-"+strconv.Itoa(n)+".md")
	}
	return name
}
