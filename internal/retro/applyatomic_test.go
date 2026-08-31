package retro

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// brokenJournal is a store whose record refuses to commit, which is what a
// locked or crashed journal looks like to Apply.
type brokenJournal struct {
	Store
	err error
}

func (b brokenJournal) ApplyRetroDiffs(context.Context, *journal.PlaybookVersion, []int64, time.Time) error {
	return b.err
}

func readPlaybook(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the playbook: %v", err)
	}
	return string(raw)
}

// The record commits before the file moves, so an apply that cannot be recorded
// leaves nothing behind for the next one to add a second time.
func TestAFailedRecordLeavesThePlaybookUntouched(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, addSetup)

	broken := testDeps(t, st, path)
	broken.Journal = brokenJournal{Store: st, err: errors.New("journal locked")}
	if _, err := Apply(context.Background(), broken, id, []int{1}); err == nil {
		t.Fatal("a journal that cannot record the version must fail the apply")
	}
	if n := strings.Count(readPlaybook(t, path), "### M2"); n != 0 {
		t.Fatalf("the addition landed on disk with nothing marking it applied, %d time(s)", n)
	}

	if _, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1}); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if n := strings.Count(readPlaybook(t, path), "### M2"); n != 1 {
		t.Fatalf("the addition appears %d times, want once", n)
	}

	_, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1})
	if err == nil || !strings.Contains(err.Error(), "an edit lands once") {
		t.Fatalf("refusal = %v", err)
	}
	if n := strings.Count(readPlaybook(t, path), "### M2"); n != 1 {
		t.Fatalf("after the refusal the addition appears %d times, want once", n)
	}
}

// The new playbook arrives by rename, so a reader never sees half a strategy and
// the staged copy does not outlive the apply.
func TestApplyLeavesNoStagedFileAndKeepsThePlaybookPrivate(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	path := applyHome(t)
	id := filedReview(t, st, path, addSetup)

	if _, err := Apply(context.Background(), testDeps(t, st, path), id, []int{1}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("playbook mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading the home: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a staged copy was left behind: %s", e.Name())
		}
	}
}
