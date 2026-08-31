package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/journal"
)

// A limit that never traded is not a trade. The proposal read as taken, so the
// pass side would have been scored against a position nobody ever held.
func TestEODMarksATakenProposalThatNeverFilled(t *testing.T) {
	newSlateHome(t)

	// The entry rests at 120.60 while the venue prints 120.50, so nothing fills.
	if _, err := run(t, "take", "1"); err != nil {
		t.Fatalf("take 1: %v", err)
	}
	if p := proposalRow(t, 1); p.Status != journal.ProposalTaken {
		t.Fatalf("proposal 1 = %s, want taken before the day ends", p.Status)
	}

	out, err := run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	if !strings.Contains(out, "1 taken proposal(s) never filled") {
		t.Fatalf("eod must say what never filled:\n%s", out)
	}
	if p := proposalRow(t, 1); p.Status != journal.ProposalUnfilled {
		t.Fatalf("proposal 1 = %s, want %s", p.Status, journal.ProposalUnfilled)
	}

	list, err := run(t, "proposals")
	if err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if !strings.Contains(list, journal.ProposalUnfilled) {
		t.Fatalf("the table must carry the unfilled status:\n%s", list)
	}
}

// The day's refusal count is a fact about the day, and eod is where the day is
// read back.
func TestEODRecapCountsTheDaysRefusals(t *testing.T) {
	newSlateHome(t)
	if _, err := run(t, "buy", "NVDA", "10"); err == nil {
		t.Fatal("a buy with no stop must be refused")
	}

	out, err := run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	if got := recapLine(out, "refusals today"); got != "1" {
		t.Fatalf("the recap counted %q refusals, want 1:\n%s", got, out)
	}
}

// recapLine is the value beside a label in a two-column block.
func recapLine(out, label string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), label); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// eod exists to end the day flat. A slate that will not close is a line in the
// report, never a reason to leave a position on overnight.
func TestEODFlattensEvenWhenTheSlateWillNotClose(t *testing.T) {
	fb := newSlateHome(t)
	fb.SetPrice("AAPL", 100)
	if _, err := run(t, "buy", "AAPL", "10", "--stop", "98"); err != nil {
		t.Fatalf("buy: %v", err)
	}

	previous := expireSlate
	expireSlate = func(context.Context, *app, string) error {
		return errors.New("the journal went away mid-close")
	}
	t.Cleanup(func() { expireSlate = previous })

	out, err := run(t, "eod")
	if err == nil {
		t.Fatal("a day that could not fully close must fail the command")
	}
	if !strings.Contains(out, "the journal went away mid-close") {
		t.Fatalf("the failure must be reported, not swallowed:\n%s", out)
	}
	if !strings.Contains(out, "positions closed") {
		t.Fatalf("the flatten must still have run:\n%s", out)
	}

	st, cfg := openTestJournal(t)
	held, err := st.OpenPositions(context.Background(), cfg.Mode)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(held) != 0 {
		t.Fatalf("the ledger still holds %+v; eod stopped before flattening", held)
	}
}
