package journal

import (
	"context"
	"strings"
	"testing"
)

func TestApplyRetroDiffsFilesTheVersionAndMarksEveryDiff(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	r, diffs := seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)

	v := &PlaybookVersion{SHA256: "abc123", Path: "playbook.md", RetroID: &r.ID, Note: "applied 2 diff(s)"}
	ids := []int64{diffs[0].ID, diffs[1].ID}
	if err := s.ApplyRetroDiffs(ctx, v, ids, retroTime); err != nil {
		t.Fatalf("ApplyRetroDiffs: %v", err)
	}
	if v.ID == 0 {
		t.Fatal("the version id was not filled in")
	}

	stored, err := s.DiffsByRetro(ctx, r.ID)
	if err != nil {
		t.Fatalf("DiffsByRetro: %v", err)
	}
	for i, d := range stored {
		if d.AppliedAt == nil || !d.AppliedAt.Equal(retroTime) {
			t.Errorf("diff %d AppliedAt = %v, want %v", i+1, d.AppliedAt, retroTime)
		}
		if d.VersionID == nil || *d.VersionID != v.ID {
			t.Errorf("diff %d VersionID = %v, want %d", i+1, d.VersionID, v.ID)
		}
	}
}

// One diff the playbook already carries fails the whole apply: the version row
// and the other marks roll back with it, so the file and the record cannot drift.
func TestApplyRetroDiffsIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	r, diffs := seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)

	first := &PlaybookVersion{SHA256: "abc123", Path: "playbook.md", RetroID: &r.ID}
	if err := s.ApplyRetroDiffs(ctx, first, []int64{diffs[0].ID}, retroTime); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	second := &PlaybookVersion{SHA256: "def456", Path: "playbook.md", RetroID: &r.ID}
	err := s.ApplyRetroDiffs(ctx, second, []int64{diffs[1].ID, diffs[0].ID}, retroTime)
	if err == nil {
		t.Fatal("re-applying a landed diff must fail the whole apply")
	}
	if !strings.Contains(err.Error(), "an edit lands once") {
		t.Fatalf("refusal = %v", err)
	}

	stored, err := s.DiffsByRetro(ctx, r.ID)
	if err != nil {
		t.Fatalf("DiffsByRetro: %v", err)
	}
	if stored[1].AppliedAt != nil {
		t.Fatalf("the second diff was marked by a transaction that failed: %+v", stored[1])
	}
	versions, err := s.ListPlaybookVersions(ctx, 0)
	if err != nil {
		t.Fatalf("ListPlaybookVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want only the one the first apply filed", len(versions))
	}
}

func TestApplyRetroDiffsRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	v := &PlaybookVersion{SHA256: "abc123", Path: "playbook.md"}

	if err := s.ApplyRetroDiffs(ctx, nil, []int64{1}, retroTime); err == nil {
		t.Error("a nil version was accepted")
	}
	if err := s.ApplyRetroDiffs(ctx, v, nil, retroTime); err == nil {
		t.Error("an empty diff list was accepted")
	}
	if err := s.ApplyRetroDiffs(ctx, &PlaybookVersion{}, []int64{1}, retroTime); err == nil {
		t.Error("a version with no sha256 was accepted")
	}
	if err := s.ApplyRetroDiffs(ctx, v, []int64{404}, retroTime); err == nil {
		t.Error("a diff that does not exist was accepted")
	}
}
