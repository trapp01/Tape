package journal

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newRefusal(mode, day, rule string, at time.Time) *Refusal {
	return &Refusal{
		Mode:   mode,
		Day:    day,
		At:     at,
		Rule:   rule,
		Symbol: "NVDA",
		Detail: "ledger cash $1,204.10 < cost $2,054.40",
		Source: SourceProposal,
	}
}

func TestInsertRefusalRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	r := newRefusal(ModePaper, proposalDay, "no overspend", proposalTime)
	if err := s.InsertRefusal(ctx, r); err != nil {
		t.Fatalf("InsertRefusal: %v", err)
	}
	if r.ID == 0 {
		t.Fatal("the refusal was not given an id")
	}

	got, err := s.RefusalsForDay(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("RefusalsForDay: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d refusals, want 1", len(got))
	}
	if got[0].Rule != "no overspend" || got[0].Symbol != "NVDA" || got[0].Source != SourceProposal {
		t.Errorf("the refusal did not survive: %+v", got[0])
	}
	if got[0].Detail != r.Detail {
		t.Errorf("detail = %q, want %q", got[0].Detail, r.Detail)
	}
	if !got[0].At.Equal(proposalTime) {
		t.Errorf("at = %v, want %v", got[0].At, proposalTime)
	}
}

func TestInsertRefusalDefaultsTheTime(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	r := newRefusal(ModePaper, proposalDay, "flat by close", time.Time{})
	before := time.Now().UTC().Add(-time.Second)
	if err := s.InsertRefusal(ctx, r); err != nil {
		t.Fatalf("InsertRefusal: %v", err)
	}
	if r.At.Before(before) {
		t.Errorf("at = %v, want a stamp from this run", r.At)
	}
}

func TestInsertRefusalRefusals(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	tests := []struct {
		name    string
		mutate  func(*Refusal)
		wantErr string
	}{
		{"an unknown mode", func(r *Refusal) { r.Mode = "shadow" }, "mode"},
		{"no day", func(r *Refusal) { r.Day = "" }, "day"},
		{"a variable-width day", func(r *Refusal) { r.Day = "2026-8-28" }, dayLayout},
		{"no rule", func(r *Refusal) { r.Rule = "" }, "rule"},
		{"a blank rule", func(r *Refusal) { r.Rule = "  " }, "rule"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRefusal(ModePaper, proposalDay, "no overspend", proposalTime)
			tc.mutate(r)
			err := s.InsertRefusal(ctx, r)
			if err == nil {
				t.Fatal("InsertRefusal accepted the refusal, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}

	if err := s.InsertRefusal(ctx, nil); err == nil {
		t.Error("InsertRefusal accepted nil")
	}
}

// Refusals read back oldest first, and paper never counts live's.
func TestRefusalsForDayAndCount(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	rows := []*Refusal{
		newRefusal(ModePaper, "2026-08-26", "max positions", proposalTime.AddDate(0, 0, -2)),
		newRefusal(ModePaper, proposalDay, "no overspend", proposalTime.Add(time.Hour)),
		newRefusal(ModePaper, proposalDay, "daily loss halt", proposalTime),
		newRefusal(ModeLive, proposalDay, "no shorting", proposalTime),
	}
	for _, r := range rows {
		if err := s.InsertRefusal(ctx, r); err != nil {
			t.Fatalf("InsertRefusal: %v", err)
		}
	}

	got, err := s.RefusalsForDay(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("RefusalsForDay: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d refusals, want 2", len(got))
	}
	if got[0].Rule != "daily loss halt" || got[1].Rule != "no overspend" {
		t.Errorf("refusals are not oldest first: %s then %s", got[0].Rule, got[1].Rule)
	}

	counts := []struct {
		name           string
		mode, from, to string
		want           int
	}{
		{"paper, the whole record", ModePaper, "", "", 3},
		{"paper, one day", ModePaper, proposalDay, proposalDay, 2},
		{"paper, from the earlier day", ModePaper, "2026-08-26", "", 3},
		{"paper, up to the earlier day", ModePaper, "", "2026-08-26", 1},
		{"paper, a day with none", ModePaper, "2026-08-27", "2026-08-27", 0},
		{"live", ModeLive, "", "", 1},
		{"both modes", "", "", "", 4},
	}
	for _, tc := range counts {
		t.Run(tc.name, func(t *testing.T) {
			n, err := s.RefusalCount(ctx, tc.mode, tc.from, tc.to)
			if err != nil {
				t.Fatalf("RefusalCount: %v", err)
			}
			if n != tc.want {
				t.Errorf("count = %d, want %d", n, tc.want)
			}
		})
	}

	if _, err := s.RefusalCount(ctx, ModePaper, "2026-8-1", ""); err == nil {
		t.Error("RefusalCount accepted a variable-width from day")
	}
	if _, err := s.RefusalCount(ctx, ModePaper, "", "2026-8-1"); err == nil {
		t.Error("RefusalCount accepted a variable-width to day")
	}
	if _, err := s.RefusalsForDay(ctx, ModePaper, "2026-8-1"); err == nil {
		t.Error("RefusalsForDay accepted a variable-width day")
	}
}
