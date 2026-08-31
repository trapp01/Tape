package brief

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// testPlaybook defines the two setups the slate replies cite.
const testPlaybook = `# Playbook

### M2 momentum continuation above prior high

### R1 range-edge mean reversion
`

// slateReply carries one idea that clears the limits and one that does not:
// NVDA risks $1.50 a share for $3.00, AAPL risks $1.50 for $1.50.
const slateReply = `{
  "market_read": "Quiet tape, breadth narrow.",
  "regime_note": "Uptrend low vol: M2 continuations are live at normal size.",
  "calendar_note": "",
  "call": {
    "instrument": "SPY",
    "direction": "up",
    "threshold_pct": 0.4,
    "rationale": "M2: price above the 20d.",
    "invalidation": "SPY trades below 509.80, yesterday's low."
  },
  "proposals": [
    {"symbol": "NVDA", "side": "long", "setup_id": "M2", "entry": 120.60, "stop": 119.10,
     "target": 123.60, "thesis": "Holds the breakout shelf.", "invalidation": "Loses 119.10 on volume.",
     "confidence": "medium"},
    {"symbol": "AAPL", "side": "long", "setup_id": "R1", "entry": 226.50, "stop": 225.00,
     "target": 228.00, "thesis": "Range low held twice.", "invalidation": "Closes under 225.",
     "confidence": "low"}
  ],
  "watchlist": [{"symbol": "NVDA", "bias": "bullish", "note": "Holds above 118.40."}],
  "risks": ["CPI can reverse the open."]
}`

// slateDeps is testDeps with the playbook and limits a proposal needs.
func slateDeps(t *testing.T, now time.Time, loc *time.Location) Deps {
	t.Helper()
	d := testDeps(t, fullFeed(now), now, loc)
	d.Playbook = testPlaybook
	d.Limits = goldenLimits()
	return d
}

func TestRunFilesEveryIdeaSizedOrRefused(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)

	res, err := Run(context.Background(), d, &fakeProvider{reply: slateReply})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Proposals) != 2 {
		t.Fatalf("both ideas belong on the record, got %d", len(res.Proposals))
	}
	if res.Input.Equity != 5000 {
		t.Fatalf("the archive must carry the equity the slate was sized against, got %v", res.Input.Equity)
	}

	taken := res.Proposals[0]
	if taken.Index != 1 || taken.Symbol != "NVDA" || taken.Status != journal.ProposalProposed {
		t.Fatalf("proposal 1 = %+v", taken)
	}
	// $25 of budget over a $1.50 stop buys 16 shares and risks $24.
	if taken.Qty != 16 || taken.RiskUSD < 23.99 || taken.RiskUSD > 24.01 {
		t.Fatalf("proposal 1 sized to %d shares risking %v", taken.Qty, taken.RiskUSD)
	}
	if taken.BriefingID != res.Briefing.ID || taken.Day != "2026-08-28" {
		t.Fatalf("proposal 1 is filed under briefing %d day %q", taken.BriefingID, taken.Day)
	}

	refused := res.Proposals[1]
	if refused.Index != 2 || refused.Symbol != "AAPL" || refused.Status != journal.ProposalRejected {
		t.Fatalf("proposal 2 = %+v", refused)
	}
	if refused.Qty != 0 || refused.DecidedAt == nil {
		t.Fatalf("a refused idea buys nothing and is decided: %+v", refused)
	}
	if !strings.Contains(refused.Reason, "reward/risk") {
		t.Fatalf("the rejection must name the rule, got %q", refused.Reason)
	}
}

// An empty slate is a valid morning and files nothing.
func TestRunFilesNoProposalsWhenTheModelOffersNone(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)

	res, err := Run(context.Background(), d, &fakeProvider{reply: goodReply})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Proposals) != 0 {
		t.Fatalf("no ideas were proposed, got %d rows", len(res.Proposals))
	}
}

func TestRunReuseReturnsTheSlate(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)
	p := &fakeProvider{reply: slateReply}

	first, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !second.Reused || len(second.Proposals) != 2 {
		t.Fatalf("a reused briefing must bring its slate back, got %d rows", len(second.Proposals))
	}
	if second.Proposals[0].ID != first.Proposals[0].ID {
		t.Fatalf("the reused slate must be the same rows, got #%d and #%d",
			second.Proposals[0].ID, first.Proposals[0].ID)
	}
}

// Before the bell the slate is still open, so a forced re-run expires it: only
// what was just printed is takeable.
func TestRunForceExpiresTheEarlierSlateBeforeTheOpen(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)
	p := &fakeProvider{reply: slateReply}

	first, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	d.Force = true
	forced, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("forced run: %v", err)
	}
	if forced.SlateKept {
		t.Fatal("a forced run before the open files its own slate")
	}
	if forced.Proposals[0].ID == first.Proposals[0].ID {
		t.Fatal("the forced run must file new rows")
	}

	all, err := d.Journal.ProposalsForDay(context.Background(), d.Mode, "2026-08-28")
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("both slates stay on the record, got %d rows", len(all))
	}
	// The earlier slate's open idea expires; its rejected one keeps the decision
	// it already carries.
	for _, p := range all {
		if p.BriefingID != first.Briefing.ID {
			continue
		}
		want := journal.ProposalExpired
		if p.Index == 2 {
			want = journal.ProposalRejected
		}
		if p.Status != want {
			t.Fatalf("the earlier idea %d is %s, want %s", p.Index, p.Status, want)
		}
	}
}

// After the bell the morning's slate is the one that stands; a second read files
// no ideas of its own.
func TestRunForceKeepsTheSlateAfterTheOpen(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)
	p := &fakeProvider{reply: slateReply}

	first, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	open := now.Add(2 * time.Hour)
	d.Now = func() time.Time { return open }
	d.Clock = func(context.Context) (broker.Clock, error) {
		return broker.Clock{IsOpen: true, NextClose: open.Add(4 * time.Hour)}, nil
	}
	d.Force = true
	forced, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("forced run: %v", err)
	}
	if !forced.SlateKept {
		t.Fatalf("the morning's slate must stand after the open, got %+v", forced)
	}
	if len(forced.Proposals) != 2 || forced.Proposals[0].ID != first.Proposals[0].ID {
		t.Fatalf("the standing slate must come back, got %+v", forced.Proposals)
	}

	all, err := d.Journal.ProposalsForDay(context.Background(), d.Mode, "2026-08-28")
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("a second read files no ideas, got %d rows", len(all))
	}
	if all[0].Status != journal.ProposalProposed {
		t.Fatalf("the standing idea must still be takeable, got %s", all[0].Status)
	}
}

// The model may only name what it was shown, proposals included.
func TestRunRefusesAProposalOnAnUnseenSymbol(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := slateDeps(t, now, loc)
	reply := strings.Replace(slateReply, `"symbol": "NVDA", "side"`, `"symbol": "DOGE", "side"`, 1)

	res, err := Run(context.Background(), d, &fakeProvider{reply: reply})
	if err == nil {
		t.Fatal("a proposal on a symbol outside the data must be refused")
	}
	if !strings.Contains(err.Error(), "DOGE") {
		t.Fatalf("the error must name the symbol, got: %v", err)
	}
	if res.Briefing.ID == 0 {
		t.Fatal("the refused reply must still be archived")
	}
	all, err := d.Journal.ProposalsForDay(context.Background(), d.Mode, "2026-08-28")
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("a reply that failed validation files no slate, got %d rows", len(all))
	}
}
