package journal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var retroTime = time.Date(2026, 8, 29, 23, 10, 0, 0, time.UTC)

func newRetro(mode, fromDay, toDay string, at time.Time) *Retro {
	cost := 0.0412
	return &Retro{
		Mode:         mode,
		GeneratedAt:  at,
		FromDay:      fromDay,
		ToDay:        toDay,
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-5",
		InputJSON:    []byte(`{"trades":12}`),
		OutputJSON:   []byte(`{"diffs":2}`),
		InputTokens:  9800,
		OutputTokens: 1200,
		CostUSD:      &cost,
		LatencyMs:    18400,
	}
}

func newDiffs() []*RetroDiff {
	return []*RetroDiff{
		{
			Section:   "M2 continuation",
			Change:    "tighten",
			Rationale: "Four of five M2 entries stopped out inside ten minutes.",
			Before:    "Enter on the first retest of the opening range high.",
			After:     "Enter on the first retest that holds for two minutes.",
		},
		{
			Section:   "Risk",
			Change:    "remove",
			Rationale: "The 1% tier never produced a winner.",
			Before:    "Risk up to 1% on a high-confidence setup.",
			After:     "",
		},
	}
}

func seedRetro(t *testing.T, s *Store, mode, fromDay, toDay string, at time.Time) (Retro, []*RetroDiff) {
	t.Helper()
	r := newRetro(mode, fromDay, toDay, at)
	diffs := newDiffs()
	if err := s.InsertRetro(context.Background(), r, diffs); err != nil {
		t.Fatalf("InsertRetro: %v", err)
	}
	return *r, diffs
}

func TestInsertRetroRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	r, diffs := seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)

	if r.ID == 0 {
		t.Fatal("retro was not given an id")
	}
	for i, d := range diffs {
		if d.ID == 0 {
			t.Fatalf("diff %d was not given an id", i)
		}
		if d.RetroID != r.ID {
			t.Errorf("diff %d RetroID = %d, want %d", i, d.RetroID, r.ID)
		}
		if d.Index != i+1 {
			t.Errorf("diff %d Index = %d, want %d", i, d.Index, i+1)
		}
	}

	got, err := s.RetroByID(ctx, r.ID)
	if err != nil {
		t.Fatalf("RetroByID: %v", err)
	}
	if got.Mode != ModePaper || got.FromDay != "2026-08-24" || got.ToDay != "2026-08-28" {
		t.Errorf("window did not survive: %+v", got)
	}
	if string(got.InputJSON) != `{"trades":12}` || string(got.OutputJSON) != `{"diffs":2}` {
		t.Errorf("payloads did not survive: %s / %s", got.InputJSON, got.OutputJSON)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.0412 {
		t.Errorf("CostUSD = %v, want 0.0412", got.CostUSD)
	}
	if !got.GeneratedAt.Equal(retroTime) {
		t.Errorf("GeneratedAt = %v, want %v", got.GeneratedAt, retroTime)
	}

	stored, err := s.DiffsByRetro(ctx, r.ID)
	if err != nil {
		t.Fatalf("DiffsByRetro: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("got %d diffs, want 2", len(stored))
	}
	if stored[0].Index != 1 || stored[1].Index != 2 {
		t.Errorf("diffs came back out of order: %d then %d", stored[0].Index, stored[1].Index)
	}
	if stored[0].Section != "M2 continuation" || stored[0].Change != "tighten" {
		t.Errorf("diff identity did not survive: %+v", stored[0])
	}
	if !strings.HasPrefix(stored[0].Before, "Enter on the first retest of") || stored[1].After != "" {
		t.Errorf("diff text did not survive: %+v / %+v", stored[0], stored[1])
	}
	if stored[0].AppliedAt != nil || stored[0].VersionID != nil {
		t.Errorf("a fresh diff is already applied: %+v", stored[0])
	}
}

func TestInsertRetroStampsGeneratedAt(t *testing.T) {
	s := newStore(t)
	r := newRetro(ModePaper, "2026-08-24", "2026-08-28", time.Time{})
	if err := s.InsertRetro(context.Background(), r, nil); err != nil {
		t.Fatalf("InsertRetro: %v", err)
	}
	if r.GeneratedAt.IsZero() {
		t.Error("GeneratedAt was not stamped")
	}
}

func TestInsertRetroRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	cases := map[string]*Retro{
		"bad mode":     {Mode: "sim", FromDay: "2026-08-24", ToDay: "2026-08-28", InputJSON: []byte(`{}`), OutputJSON: []byte(`{}`)},
		"bad from day": {Mode: ModePaper, FromDay: "nope", ToDay: "2026-08-28", InputJSON: []byte(`{}`), OutputJSON: []byte(`{}`)},
		"inverted":     {Mode: ModePaper, FromDay: "2026-08-28", ToDay: "2026-08-24", InputJSON: []byte(`{}`), OutputJSON: []byte(`{}`)},
		"no input":     {Mode: ModePaper, FromDay: "2026-08-24", ToDay: "2026-08-28", OutputJSON: []byte(`{}`)},
		"no output":    {Mode: ModePaper, FromDay: "2026-08-24", ToDay: "2026-08-28", InputJSON: []byte(`{}`)},
	}
	for name, r := range cases {
		if err := s.InsertRetro(ctx, r, nil); err == nil {
			t.Errorf("InsertRetro accepted %s, want an error", name)
		}
	}
	if err := s.InsertRetro(ctx, newRetro(ModePaper, "2026-08-24", "2026-08-28", retroTime), []*RetroDiff{nil}); err == nil {
		t.Error("InsertRetro accepted a nil diff, want an error")
	}
}

func TestInsertRetroNumbersRepeatedDiffs(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	// Two edits can say the same thing; the position within the retro is what has
	// to be unique.
	r := newRetro(ModePaper, "2026-08-24", "2026-08-28", retroTime)
	diffs := newDiffs()
	diffs[1] = diffs[0]
	if err := s.InsertRetro(ctx, r, diffs); err != nil {
		t.Fatalf("InsertRetro with repeated content: %v", err)
	}
	stored, err := s.DiffsByRetro(ctx, r.ID)
	if err != nil {
		t.Fatalf("DiffsByRetro: %v", err)
	}
	if len(stored) != 2 || stored[0].Index != 1 || stored[1].Index != 2 {
		t.Fatalf("diffs = %+v, want indexes 1 and 2", stored)
	}
}

func TestRetroByIDNotFound(t *testing.T) {
	if _, err := newStore(t).RetroByID(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RetroByID error = %v, want ErrNotFound", err)
	}
}

func TestListRetrosNewestFirstAndByMode(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedRetro(t, s, ModePaper, "2026-08-17", "2026-08-21", retroTime.AddDate(0, 0, -7))
	seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)
	seedRetro(t, s, ModeLive, "2026-08-24", "2026-08-28", retroTime)

	paper, err := s.ListRetros(ctx, ModePaper, 0)
	if err != nil {
		t.Fatalf("ListRetros: %v", err)
	}
	if len(paper) != 2 {
		t.Fatalf("got %d paper retros, want 2", len(paper))
	}
	if paper[0].FromDay != "2026-08-24" {
		t.Errorf("ListRetros returned %s first, want the newest (2026-08-24)", paper[0].FromDay)
	}

	limited, err := s.ListRetros(ctx, ModePaper, 1)
	if err != nil {
		t.Fatalf("ListRetros limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("got %d retros under a limit of 1, want 1", len(limited))
	}
	both, err := s.ListRetros(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListRetros both: %v", err)
	}
	if len(both) != 3 {
		t.Fatalf("got %d retros for both modes, want 3", len(both))
	}
}

func TestMarkDiffAppliedOnce(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	r, diffs := seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)

	v := &PlaybookVersion{SHA256: "abc123", Path: "playbook.md", RetroID: &r.ID, Note: "tightened M2"}
	if err := s.InsertPlaybookVersion(ctx, v); err != nil {
		t.Fatalf("InsertPlaybookVersion: %v", err)
	}
	if err := s.MarkDiffApplied(ctx, diffs[0].ID, v.ID, retroTime); err != nil {
		t.Fatalf("MarkDiffApplied: %v", err)
	}

	stored, err := s.DiffsByRetro(ctx, r.ID)
	if err != nil {
		t.Fatalf("DiffsByRetro: %v", err)
	}
	if stored[0].AppliedAt == nil || !stored[0].AppliedAt.Equal(retroTime) {
		t.Errorf("AppliedAt = %v, want %v", stored[0].AppliedAt, retroTime)
	}
	if stored[0].VersionID == nil || *stored[0].VersionID != v.ID {
		t.Errorf("VersionID = %v, want %d", stored[0].VersionID, v.ID)
	}
	if stored[1].AppliedAt != nil {
		t.Error("the second diff was applied too")
	}

	err = s.MarkDiffApplied(ctx, diffs[0].ID, v.ID, retroTime)
	if err == nil {
		t.Fatal("a second MarkDiffApplied succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "already applied") {
		t.Errorf("error %q does not say the diff was already applied", err)
	}
}

func TestMarkDiffAppliedRejectsBadIDs(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.MarkDiffApplied(ctx, 0, 1, retroTime); err == nil {
		t.Error("MarkDiffApplied accepted a zero diff id")
	}
	if err := s.MarkDiffApplied(ctx, 1, 0, retroTime); err == nil {
		t.Error("MarkDiffApplied accepted a zero version id")
	}
	if err := s.MarkDiffApplied(ctx, 404, 1, retroTime); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkDiffApplied on a missing diff = %v, want ErrNotFound", err)
	}
}

func TestPlaybookVersionRoundTripAndLatest(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	r, _ := seedRetro(t, s, ModePaper, "2026-08-24", "2026-08-28", retroTime)

	first := &PlaybookVersion{
		CreatedAt: retroTime.AddDate(0, 0, -7),
		SHA256:    "1111", Path: "playbook.md", Note: "initial", ConfigHash: "cfg-a",
	}
	second := &PlaybookVersion{
		CreatedAt: retroTime,
		SHA256:    "2222", Path: "playbook.md", RetroID: &r.ID, Note: "tightened M2", ConfigHash: "cfg-b",
	}
	for _, v := range []*PlaybookVersion{first, second} {
		if err := s.InsertPlaybookVersion(ctx, v); err != nil {
			t.Fatalf("InsertPlaybookVersion %s: %v", v.SHA256, err)
		}
		if v.ID == 0 {
			t.Fatalf("version %s was not given an id", v.SHA256)
		}
	}

	latest, err := s.LatestPlaybookVersion(ctx)
	if err != nil {
		t.Fatalf("LatestPlaybookVersion: %v", err)
	}
	if latest.SHA256 != "2222" || latest.ConfigHash != "cfg-b" {
		t.Errorf("latest = %+v, want the 2222 / cfg-b snapshot", latest)
	}
	if latest.RetroID == nil || *latest.RetroID != r.ID {
		t.Errorf("RetroID = %v, want %d", latest.RetroID, r.ID)
	}
	if !latest.CreatedAt.Equal(retroTime) {
		t.Errorf("CreatedAt = %v, want %v", latest.CreatedAt, retroTime)
	}

	all, err := s.ListPlaybookVersions(ctx, 0)
	if err != nil {
		t.Fatalf("ListPlaybookVersions: %v", err)
	}
	if len(all) != 2 || all[0].SHA256 != "2222" {
		t.Fatalf("versions = %+v, want the newest first", all)
	}
	if all[1].RetroID != nil {
		t.Errorf("the first version claims retro %v; it predates any", all[1].RetroID)
	}
	limited, err := s.ListPlaybookVersions(ctx, 1)
	if err != nil {
		t.Fatalf("ListPlaybookVersions limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("got %d versions under a limit of 1, want 1", len(limited))
	}
}

func TestLatestPlaybookVersionNotFound(t *testing.T) {
	if _, err := newStore(t).LatestPlaybookVersion(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestPlaybookVersion error = %v, want ErrNotFound", err)
	}
}

func TestInsertPlaybookVersionRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.InsertPlaybookVersion(ctx, nil); err == nil {
		t.Error("InsertPlaybookVersion accepted nil")
	}
	if err := s.InsertPlaybookVersion(ctx, &PlaybookVersion{SHA256: "  "}); err == nil {
		t.Error("InsertPlaybookVersion accepted a blank sha256")
	}
	zero := int64(0)
	if err := s.InsertPlaybookVersion(ctx, &PlaybookVersion{SHA256: "abc", RetroID: &zero}); err == nil {
		t.Error("InsertPlaybookVersion accepted a zero retro id")
	}
}

func TestInsertPlaybookVersionStampsCreatedAt(t *testing.T) {
	v := &PlaybookVersion{SHA256: "abc123"}
	if err := newStore(t).InsertPlaybookVersion(context.Background(), v); err != nil {
		t.Fatalf("InsertPlaybookVersion: %v", err)
	}
	if v.CreatedAt.IsZero() {
		t.Error("CreatedAt was not stamped")
	}
}
