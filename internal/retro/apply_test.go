package retro

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// applyHome writes the test playbook into a temp home and returns its path.
func applyHome(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "playbook.md")
	if err := os.WriteFile(path, []byte(testPlaybook), 0o600); err != nil {
		t.Fatalf("writing the playbook: %v", err)
	}
	return path
}

// filedReview archives a review carrying the given diffs and returns its id.
func filedReview(t *testing.T, st *fakeStore, path, diffs string) int64 {
	t.Helper()
	res, err := Run(context.Background(), testDeps(t, st, path), &fakeProvider{reply: reply(diffs)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res.Retro.ID
}

const editM1 = `
  {"section": "### M1 gap-and-go continuation", "change": "edit",
   "rationale": "The one M1 trade held its gap.",
   "before": "Enter on the first pullback that holds the gap.",
   "after": "Enter on the first pullback that holds the gap and the prior day's high."}`

const addSetup = `
  {"section": "## Setups", "change": "add", "rationale": "A second continuation.",
   "before": "", "after": "### M2 afternoon continuation\n\nEnter the second push over the noon high."}`

func TestApplyRewritesThePlaybook(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1+","+addSetup)

	report, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1, 2})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Applied) != 2 {
		t.Fatalf("applied = %+v", report.Applied)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the playbook: %v", err)
	}
	text := string(written)
	if !strings.Contains(text, "holds the gap and the prior day's high.") {
		t.Fatalf("the edit did not land:\n%s", text)
	}
	if !strings.Contains(text, "### M2 afternoon continuation") {
		t.Fatalf("the addition did not land:\n%s", text)
	}
	// The new setup goes under Setups, ahead of the risk rules it may not touch.
	if strings.Index(text, "### M2") > strings.Index(text, "## Risk rules") {
		t.Fatalf("the addition landed outside its section:\n%s", text)
	}
	if !strings.Contains(text, "Per trade 0.5% of equity") {
		t.Fatalf("the risk rules were disturbed:\n%s", text)
	}
}

// The playbook that is being replaced is kept, so an old stat can still be read
// next to the rules it was traded under.
func TestApplyKeepsThePreviousPlaybook(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1)

	report, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if filepath.Base(filepath.Dir(report.HistoryPath)) != historyDir {
		t.Fatalf("history went to %s", report.HistoryPath)
	}
	kept, err := os.ReadFile(report.HistoryPath)
	if err != nil {
		t.Fatalf("reading the kept playbook: %v", err)
	}
	if string(kept) != testPlaybook {
		t.Fatalf("the kept playbook is not the one that was replaced:\n%s", kept)
	}
}

// Applying an edit is a rule change, so the gate stops reading the sessions that
// argued for it.
func TestApplyRecordsAVersion(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1)

	report, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(st.versions) != 1 {
		t.Fatalf("versions = %+v", st.versions)
	}
	v := st.versions[0]
	if v.RetroID == nil || *v.RetroID != id || v.Path != path {
		t.Fatalf("version = %+v", v)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the playbook: %v", err)
	}
	if v.SHA256 != sha256Hex(written) {
		t.Fatalf("the version fingerprints something other than the file on disk")
	}
	if report.Applied[0].AppliedAt == nil || report.Applied[0].VersionID == nil {
		t.Fatalf("the diff was not marked applied: %+v", report.Applied[0])
	}

	// EnsureVersion sees nothing new, because the snapshot already matches.
	next, err := EnsureVersion(context.Background(), st, testDeps(t, st, path).Cfg, path)
	if err != nil {
		t.Fatalf("EnsureVersion: %v", err)
	}
	if next != nil {
		t.Fatalf("a second snapshot of an unchanged file: %+v", next)
	}
}

// An edit lands once. The second attempt is refused rather than doubling the
// text it added.
func TestApplyRefusesADiffTwice(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1)

	if _, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	_, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1})
	if err == nil {
		t.Fatal("the second apply must be refused")
	}
	if !strings.Contains(err.Error(), "an edit lands once") {
		t.Fatalf("refusal = %v", err)
	}
}

// The playbook may have moved since the review was written, so every diff is
// resolved against the file as it stands now.
func TestApplyRevalidatesAgainstTheCurrentPlaybook(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1)

	edited := strings.Replace(testPlaybook, "Enter on the first pullback that holds the gap.", "Enter on any pullback.", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("editing the playbook by hand: %v", err)
	}

	_, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1})
	if err == nil {
		t.Fatal("a diff whose before is gone must be refused")
	}
	if !strings.Contains(err.Error(), "does not appear under") {
		t.Fatalf("refusal = %v", err)
	}
	if len(st.versions) != 0 {
		t.Fatalf("a refused apply recorded a version: %+v", st.versions)
	}
}

func TestApplyNamesADiffThatDoesNotExist(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, editM1)

	_, err := Apply(context.Background(), testDeps(t, st, path), id, []int{4})
	if err == nil || !strings.Contains(err.Error(), "has no diff 4") {
		t.Fatalf("refusal = %v", err)
	}
}
