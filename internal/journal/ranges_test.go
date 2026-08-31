package journal

import (
	"context"
	"testing"
	"time"
)

var rangeDays = []string{"2026-08-24", "2026-08-26", "2026-08-28"}

func TestProposalsInRangeBoundsAndMode(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i, day := range rangeDays {
		seedProposal(t, s, ModePaper, day, i+1, "NVDA", ProposalPassed)
	}
	seedProposal(t, s, ModeLive, rangeDays[1], 9, "AAPL", ProposalPassed)

	full, err := s.ProposalsInRange(ctx, ModePaper, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("ProposalsInRange: %v", err)
	}
	if len(full) != 3 {
		t.Fatalf("got %d proposals over the full range, want 3", len(full))
	}
	if full[0].Day != rangeDays[0] || full[2].Day != rangeDays[2] {
		t.Errorf("proposals came back out of order: %s then %s", full[0].Day, full[2].Day)
	}

	// Both bounds are inclusive, so a one-day window holds that day's row.
	edge, err := s.ProposalsInRange(ctx, ModePaper, rangeDays[0], rangeDays[0])
	if err != nil {
		t.Fatalf("ProposalsInRange edge: %v", err)
	}
	if len(edge) != 1 {
		t.Fatalf("got %d proposals on the from-bound day, want 1", len(edge))
	}

	live, err := s.ProposalsInRange(ctx, ModeLive, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("ProposalsInRange live: %v", err)
	}
	if len(live) != 1 || live[0].Symbol != "AAPL" {
		t.Fatalf("live = %+v, want only the live proposal", live)
	}
	both, err := s.ProposalsInRange(ctx, "", rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("ProposalsInRange both: %v", err)
	}
	if len(both) != 4 {
		t.Fatalf("got %d proposals for both modes, want 4", len(both))
	}
}

func TestCallsInRangeBoundsAndMode(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, day := range rangeDays {
		insertBriefingWithCall(t, s, ModePaper, day, "SPY", "up", proposalTime)
	}
	insertBriefingWithCall(t, s, ModeLive, rangeDays[1], "QQQ", "down", proposalTime)

	got, err := s.CallsInRange(ctx, ModePaper, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("CallsInRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d calls, want 3", len(got))
	}
	if got[0].Day != rangeDays[0] || got[2].Day != rangeDays[2] {
		t.Errorf("calls came back out of order: %s then %s", got[0].Day, got[2].Day)
	}

	inner, err := s.CallsInRange(ctx, ModePaper, rangeDays[1], rangeDays[1])
	if err != nil {
		t.Fatalf("CallsInRange inner: %v", err)
	}
	if len(inner) != 1 {
		t.Fatalf("got %d calls on the middle day, want 1", len(inner))
	}
	live, err := s.CallsInRange(ctx, ModeLive, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("CallsInRange live: %v", err)
	}
	if len(live) != 1 || live[0].Instrument != "QQQ" {
		t.Fatalf("live = %+v, want only the live call", live)
	}
}

func TestRefusalsInRangeBoundsAndMode(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, day := range rangeDays {
		if err := s.InsertRefusal(ctx, newRefusal(ModePaper, day, "no overspend", proposalTime)); err != nil {
			t.Fatalf("InsertRefusal %s: %v", day, err)
		}
	}
	if err := s.InsertRefusal(ctx, newRefusal(ModeLive, rangeDays[1], "risk cap", proposalTime)); err != nil {
		t.Fatalf("InsertRefusal live: %v", err)
	}

	got, err := s.RefusalsInRange(ctx, ModePaper, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("RefusalsInRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d refusals, want 3", len(got))
	}
	if got[0].Day != rangeDays[0] || got[2].Day != rangeDays[2] {
		t.Errorf("refusals came back out of order: %s then %s", got[0].Day, got[2].Day)
	}

	trimmed, err := s.RefusalsInRange(ctx, ModePaper, rangeDays[0], rangeDays[1])
	if err != nil {
		t.Fatalf("RefusalsInRange trimmed: %v", err)
	}
	if len(trimmed) != 2 {
		t.Fatalf("got %d refusals through the middle day, want 2", len(trimmed))
	}
	both, err := s.RefusalsInRange(ctx, "", rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("RefusalsInRange both: %v", err)
	}
	if len(both) != 4 {
		t.Fatalf("got %d refusals for both modes, want 4", len(both))
	}
}

func TestBriefingsInRangeBoundsAndMode(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, day := range rangeDays {
		seedBriefing(t, s, ModePaper, day)
	}
	seedBriefing(t, s, ModeLive, rangeDays[2])

	got, err := s.BriefingsInRange(ctx, ModePaper, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("BriefingsInRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d briefings, want 3", len(got))
	}
	if got[0].Day != rangeDays[0] || got[2].Day != rangeDays[2] {
		t.Errorf("briefings came back out of order: %s then %s", got[0].Day, got[2].Day)
	}
	if string(got[0].InputJSON) == "" || string(got[0].OutputJSON) == "" {
		t.Error("the archived payloads did not survive the range read")
	}

	live, err := s.BriefingsInRange(ctx, ModeLive, rangeDays[0], rangeDays[2])
	if err != nil {
		t.Fatalf("BriefingsInRange live: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("got %d live briefings, want 1", len(live))
	}
}

func TestRangeReadersRejectBadBounds(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	readers := map[string]func(from, to string) error{
		"ProposalsInRange": func(from, to string) error {
			_, err := s.ProposalsInRange(ctx, ModePaper, from, to)
			return err
		},
		"OutcomesInRange": func(from, to string) error {
			_, err := s.OutcomesInRange(ctx, ModePaper, from, to)
			return err
		},
		"CallsInRange": func(from, to string) error {
			_, err := s.CallsInRange(ctx, ModePaper, from, to)
			return err
		},
		"RefusalsInRange": func(from, to string) error {
			_, err := s.RefusalsInRange(ctx, ModePaper, from, to)
			return err
		},
		"BriefingsInRange": func(from, to string) error {
			_, err := s.BriefingsInRange(ctx, ModePaper, from, to)
			return err
		},
		"NoteScoresInRange": func(from, to string) error {
			_, err := s.NoteScoresInRange(ctx, ModePaper, from, to)
			return err
		},
	}
	bad := []struct{ name, from, to string }{
		{"empty from", "", "2026-08-28"},
		{"empty to", "2026-08-24", ""},
		{"not a day", "24/08/2026", "2026-08-28"},
		{"inverted", "2026-08-28", "2026-08-24"},
	}
	for name, read := range readers {
		for _, b := range bad {
			if err := read(b.from, b.to); err == nil {
				t.Errorf("%s accepted %s (%q..%q), want an error", name, b.name, b.from, b.to)
			}
		}
	}
}

func TestOrdersByIDs(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first := insertFill(t, s, ModePaper, "NVDA", "buy", 10, 100, time.Now().UTC(), 1, 0)
	second := insertFill(t, s, ModePaper, "AAPL", "buy", 5, 200, time.Now().UTC(), 1, 0)

	got, err := s.OrdersByIDs(ctx, []int64{first.OrderID, second.OrderID, 9999})
	if err != nil {
		t.Fatalf("OrdersByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d orders, want 2 (the missing id is simply absent)", len(got))
	}
	if got[first.OrderID].Symbol != "NVDA" || got[second.OrderID].Symbol != "AAPL" {
		t.Errorf("orders came back keyed wrong: %+v", got)
	}
}

func TestOrdersByIDsEmptyInput(t *testing.T) {
	got, err := newStore(t).OrdersByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("OrdersByIDs: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %+v, want an empty map", got)
	}
}

func TestProposalsByIDsChunksLongIDLists(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)

	// More than one chunk, so the IN list has to be split.
	const count = idChunk + 7
	slate := make([]*Proposal, count)
	for i := range slate {
		slate[i] = newProposal(briefingID, ModePaper, proposalDay, i+1, "NVDA")
	}
	if err := s.InsertProposals(ctx, slate); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	ids := make([]int64, count)
	for i, p := range slate {
		ids[i] = p.ID
	}

	got, err := s.ProposalsByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("ProposalsByIDs: %v", err)
	}
	if len(got) != count {
		t.Fatalf("got %d proposals, want %d", len(got), count)
	}
	for _, id := range ids {
		if got[id].ID != id {
			t.Fatalf("proposal %d is missing from the map", id)
		}
	}
}

func TestProposalsByIDsEmptyInput(t *testing.T) {
	got, err := newStore(t).ProposalsByIDs(context.Background(), []int64{})
	if err != nil {
		t.Fatalf("ProposalsByIDs: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %+v, want an empty map", got)
	}
}
