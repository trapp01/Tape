package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
)

// eveningReply is a one-idea slate, so a forced evening re-run is telling the
// morning's three ideas apart by more than their order.
const eveningReply = `{
  "market_read": "The afternoon faded; Monday opens on the lows.",
  "regime_note": "Uptrend low vol still, but the close was soft.",
  "calendar_note": "",
  "call": {
    "instrument": "SPY",
    "direction": "down",
    "threshold_pct": 0.3,
    "rationale": "M1: the gap filled and the shelf broke.",
    "invalidation": "SPY reclaims 512.50."
  },
  "proposals": [
    {"symbol": "AMZN", "side": "long", "setup_id": "M1", "entry": 178.40, "stop": 176.90,
     "target": 183.00, "thesis": "Held the gap into the close.", "invalidation": "Fills the gap.",
     "confidence": "medium"}
  ],
  "watchlist": [],
  "risks": []
}`

// thursdayEvening is 21:40 Eastern the night before the pinned session, so the
// reader's calendar day and the venue's next session are different days.
var thursdayEvening = time.Date(2026, 8, 28, 1, 40, 0, 0, time.UTC)

// An after-hours briefing is a call on the next session and files its slate
// under that day. take resolved N against the venue's current day, so the slate
// that printed was not the slate that was takeable — the ideas read as missing
// all evening and expired unacted the next morning.
func TestEveningSlateIsTakeableTheSameEvening(t *testing.T) {
	fb, feed, provider := newBriefHome(t)
	provider.reply = slateReply
	priceVenueFromFeed(fb, feed)
	shortPoll(t)
	atClock(t, thursdayEvening)

	out, err := run(t, "brief")
	if err != nil {
		t.Fatalf("evening brief: %v", err)
	}
	if !strings.Contains(out, "PROPOSALS (3)") {
		t.Fatalf("the evening briefing printed no slate:\n%s", out)
	}

	list, err := run(t, "proposals")
	if err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if !strings.Contains(list, briefSession) || !strings.Contains(list, "NVDA") {
		t.Fatalf("proposals must list the slate that was just printed:\n%s", list)
	}

	if _, err := run(t, "why", "1"); err != nil {
		t.Fatalf("why 1 on the evening slate: %v", err)
	}
	taken, err := run(t, "take", "1")
	if err != nil {
		t.Fatalf("take 1 on the evening slate: %v", err)
	}
	if !strings.Contains(taken, "taken: proposal #1 NVDA") {
		t.Fatalf("take output:\n%s", taken)
	}
	if p := proposalRowOn(t, briefSession, 1); p.Status != journal.ProposalTaken {
		t.Fatalf("proposal 1 = %s, want taken", p.Status)
	}
}

// A forced re-run after the close writes a slate for the next session. take has
// to act on that one, never on the morning's, which the session has ended on.
func TestTakeActsOnTheEveningSlateNotTheMorningOne(t *testing.T) {
	fb, feed, provider := newBriefHome(t)
	provider.reply = slateReply
	priceVenueFromFeed(fb, feed)
	shortPoll(t)

	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("morning brief: %v", err)
	}

	// The bell has rung and gone; the next session is Monday.
	const monday = "2026-08-31"
	atClock(t, briefEvening)
	fb.NextOpen = time.Date(2026, 8, 31, 13, 30, 0, 0, time.UTC)
	fb.NextClose = time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC)
	provider.reply = eveningReply

	if _, err := run(t, "brief", "--force"); err != nil {
		t.Fatalf("evening brief --force: %v", err)
	}
	if p := proposalRowOn(t, monday, 1); p.Symbol != "AMZN" {
		t.Fatalf("the evening slate's #1 is %s, want AMZN", p.Symbol)
	}

	out, err := run(t, "take", "1")
	if err != nil {
		t.Fatalf("take 1: %v", err)
	}
	if !strings.Contains(out, "proposal #1 AMZN") {
		t.Fatalf("take must act on the evening slate:\n%s", out)
	}
	if p := proposalRowOn(t, briefSession, 1); p.Status == journal.ProposalTaken {
		t.Fatalf("take reached back into the morning slate: %+v", p)
	}
	if p := proposalRowOn(t, monday, 1); p.Status != journal.ProposalTaken {
		t.Fatalf("the evening idea is %s, want taken", p.Status)
	}
}

// pricySlateReply proposes one high-priced share. Sixteen of them is $8,192 of
// stock, which a $5,000 ledger cannot pay for.
const pricySlateReply = `{
  "market_read": "The index is the only thing trending.",
  "regime_note": "Uptrend low vol.",
  "calendar_note": "",
  "call": {
    "instrument": "SPY",
    "direction": "up",
    "threshold_pct": 0.3,
    "rationale": "M2 continuation over yesterday's high.",
    "invalidation": "SPY loses 510.50."
  },
  "proposals": [
    {"symbol": "SPY", "side": "long", "setup_id": "M2", "entry": 512.00, "stop": 510.50,
     "target": 517.00, "thesis": "Holding above the shelf.", "invalidation": "Loses 510.50.",
     "confidence": "medium"}
  ],
  "watchlist": [],
  "risks": []
}`

// Sizing knew the risk budget and nothing about the bill, so the slate printed
// a trade the account could not pay for and the overspend rule refused the take.
func TestSlateIsCappedOnCashAndStaysTakeable(t *testing.T) {
	fb, feed, provider := newBriefHome(t)
	provider.reply = pricySlateReply
	priceVenueFromFeed(fb, feed)
	shortPoll(t)

	out, err := run(t, "brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	// $4,900 of free cash after 2% of headroom buys 9 shares at $512.00.
	if !strings.Contains(out, "size 9 sh") || !strings.Contains(out, "cash-capped") {
		t.Fatalf("the slate must size on cash and say so:\n%s", out)
	}

	if p := proposalRow(t, 1); p.Qty != 9 {
		t.Fatalf("proposal 1 sized %d shares, want 9", p.Qty)
	}
	taken, err := run(t, "take", "1")
	if err != nil {
		t.Fatalf("a cash-capped idea must be takeable: %v", err)
	}
	if !strings.Contains(taken, "taken: proposal #1 SPY 9 sh") {
		t.Fatalf("take output:\n%s", taken)
	}

	why, err := run(t, "why", "1")
	if err != nil {
		t.Fatalf("why 1: %v", err)
	}
	if !strings.Contains(why, "capped") || !strings.Contains(why, "$4,900.00") {
		t.Fatalf("why must show the cash ceiling that set the size:\n%s", why)
	}
}

// An archive written without a sizing basis cannot show the arithmetic, and
// printing "× 0% = $0.00" reads as a rule that sized nothing.
func TestWhyWithoutASizingBasisSaysSo(t *testing.T) {
	newSlateHome(t)
	seedBlankBasisProposal(t, 4)

	out, err := run(t, "why", "4")
	if err != nil {
		t.Fatalf("why 4: %v", err)
	}
	if !strings.Contains(out, "sizing basis missing from the archive") {
		t.Fatalf("why must say the basis is missing:\n%s", out)
	}
	if strings.Contains(out, "× 0% = $0.00") {
		t.Fatalf("why printed sizing arithmetic it does not have:\n%s", out)
	}
}

// seedBlankBasisProposal files an idea on a briefing whose archived input
// carries no limits and no equity, the way an older archive reads.
func seedBlankBasisProposal(t *testing.T, index int) {
	t.Helper()
	ctx := context.Background()
	st, cfg := openTestJournal(t)
	b := &journal.Briefing{
		Mode:        cfg.Mode,
		GeneratedAt: timeNow().UTC(),
		Day:         briefSession,
		Provider:    "fake",
		Model:       "fake-model-1",
		InputJSON:   []byte(`{"playbook":"# Playbook\n"}`),
		OutputJSON:  []byte(`{}`),
	}
	if err := st.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("seeding the basis-free briefing: %v", err)
	}
	p := &journal.Proposal{
		BriefingID: b.ID, Mode: cfg.Mode, Day: briefSession, Index: index,
		Symbol: "NVDA", Side: "long", SetupID: "M2",
		Entry: 120.60, Stop: 119.10, Target: 123.60, Qty: 16, RiskUSD: 24,
		Thesis: "seeded by the test", Invalidation: "seeded by the test", Confidence: "medium",
	}
	if err := st.InsertProposals(ctx, []*journal.Proposal{p}); err != nil {
		t.Fatalf("seeding proposal %d: %v", index, err)
	}
}

// A re-run can produce a shorter slate. "take 3" against two ideas used to read
// as an expired proposal, which is a different and much worse fact.
func TestTakeBeyondTheSlateSaysHowManyThereAre(t *testing.T) {
	newSlateHome(t)

	_, err := run(t, "take", "4")
	if err == nil || !strings.Contains(err.Error(), "the current slate has 3 idea(s)") {
		t.Fatalf("a number past the slate must say how long the slate is, got: %v", err)
	}
}

// After 22:00 Mountain the venue is already on the next session, and a refusal
// filed under the reader's calendar day lands in a day the desk never traded.
func TestRefusalsAreFiledUnderTheSession(t *testing.T) {
	fb, feed, _ := newBriefHome(t)
	priceVenueFromFeed(fb, feed)
	// 23:00 Mountain on the 27th is 01:00 Eastern on the 28th.
	atClock(t, time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC))

	if _, err := run(t, "buy", "NVDA", "10"); err == nil {
		t.Fatal("a buy with no stop must be refused")
	}

	if got := refusalsOn(t, briefSession); len(got) != 1 {
		t.Fatalf("the refusal must be filed under %s, got %d rows there", briefSession, len(got))
	}
	local := timeNow().In(mountain(t)).Format(dayLayout)
	if got := refusalsOn(t, local); len(got) != 0 {
		t.Fatalf("%d refusals were filed under the reader's day %s", len(got), local)
	}

	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := recapLine(out, "refusals today"); got != "1" {
		t.Fatalf("status counted %q refusals for the session, want 1:\n%s", got, out)
	}
}

func mountain(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(briefZone)
	if err != nil {
		t.Fatalf("loading %s: %v", briefZone, err)
	}
	return loc
}

// priceVenueFromFeed makes the venue quote what the briefing showed the model,
// so an entry the slate proposes is one the freshness rule recognises.
func priceVenueFromFeed(fb *fake.Broker, feed *briefFeed) {
	for symbol, snap := range feed.snaps {
		fb.SetPrice(symbol, snap.Last)
	}
}

// proposalRowOn reads one proposal from a named session, past the renderer.
func proposalRowOn(t *testing.T, day string, index int) journal.Proposal {
	t.Helper()
	st, cfg := openTestJournal(t)
	p, err := st.ProposalByDayIndex(context.Background(), cfg.Mode, day, index)
	if err != nil {
		t.Fatalf("proposal %d on %s: %v", index, day, err)
	}
	return p
}
