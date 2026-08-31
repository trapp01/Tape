package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
)

// slateReply carries two ideas the limits allow and one they do not: AAPL pays
// only one dollar of target for every dollar of stop.
const slateReply = `{
  "market_read": "Breadth is narrow; the index is carried by semis.",
  "regime_note": "Uptrend low vol: M2 continuations are live at normal size.",
  "calendar_note": "",
  "call": {
    "instrument": "SPY",
    "direction": "up",
    "threshold_pct": 0.3,
    "rationale": "M2: price is above the 20d and took out yesterday's high.",
    "invalidation": "SPY trades below 509.80, yesterday's low."
  },
  "proposals": [
    {"symbol": "NVDA", "side": "long", "setup_id": "M2", "entry": 120.60, "stop": 119.10,
     "target": 123.60, "thesis": "Holds the breakout shelf.", "invalidation": "Loses 119.10 on volume.",
     "confidence": "medium"},
    {"symbol": "AMZN", "side": "long", "setup_id": "M1", "entry": 178.40, "stop": 176.90,
     "target": 183.00, "thesis": "Gapped over the shelf and held it.", "invalidation": "Fills the gap.",
     "confidence": "low"},
    {"symbol": "AAPL", "side": "long", "setup_id": "R1", "entry": 226.50, "stop": 225.00,
     "target": 228.00, "thesis": "Range low held twice.", "invalidation": "Closes under 225.",
     "confidence": "low"}
  ],
  "watchlist": [{"symbol": "NVDA", "bias": "bullish", "note": "Holds above 118.40."}],
  "risks": ["A quiet open can turn into an afternoon fade."]
}`

// newSlateHome is newBriefHome with a model that proposes trades, and the
// briefing already run. The venue quotes what the feed showed the model, so an
// entry the slate proposes is one the freshness rule recognises.
func newSlateHome(t *testing.T) *fake.Broker {
	t.Helper()
	fb, feed, provider := newBriefHome(t)
	provider.reply = slateReply
	priceVenueFromFeed(fb, feed)
	shortPoll(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}
	return fb
}

// shortPoll keeps a resting limit order from costing the suite five real seconds.
func shortPoll(t *testing.T) {
	t.Helper()
	previous := pollWindow
	pollWindow = 20 * time.Millisecond
	t.Cleanup(func() { pollWindow = previous })
}

func TestBriefRendersTheSizedSlate(t *testing.T) {
	_, _, provider := newBriefHome(t)
	provider.reply = slateReply

	out, err := run(t, "brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	for _, want := range []string{
		"PROPOSALS (3)",
		"#1  LONG NVDA — M2 momentum continuation",
		// $25 of budget over a $1.50 stop buys 16 shares.
		"size 16 sh (~$1,929 · risks $24 = 0.5%)  2.0R",
		"thesis: Holds the breakout shelf.",
		"invalid if: Loses 119.10 on volume.   confidence: medium",
		"#3  LONG AAPL — R1 range-edge mean reversion   ✗ rejected:",
		"rule: reward/risk",
		`act: tape take 1 · tape pass 1 --reason "…" · tape why 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief output missing %q:\n%s", want, out)
		}
	}

	// The archive re-renders the same slate, decisions and all.
	shown, err := run(t, "briefs", "show", "today")
	if err != nil {
		t.Fatalf("briefs show today: %v", err)
	}
	if !strings.Contains(shown, "PROPOSALS (3)") || !strings.Contains(shown, "size 16 sh") {
		t.Fatalf("the archived briefing must carry its slate:\n%s", shown)
	}
}

// A briefing with nothing to trade says so, and says why.
func TestBriefRendersAnEmptySlate(t *testing.T) {
	newBriefHome(t)

	out, err := run(t, "brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if !strings.Contains(out, "PROPOSALS none today") {
		t.Fatalf("an empty slate must say so:\n%s", out)
	}
	if !strings.Contains(out, "M2 continuations are live at normal size.") {
		t.Fatalf("an empty slate carries the model's reason:\n%s", out)
	}
}

func TestTakeSubmitsTheBracketAndLinksTheOrder(t *testing.T) {
	fb := newSlateHome(t)

	out, err := run(t, "take", "1")
	if err != nil {
		t.Fatalf("take 1: %v", err)
	}
	for _, want := range []string{"[paper] take 1", "taken: proposal #1 NVDA 16 sh → order #1", "limit $120.60", "$119.10", "$123.60"} {
		if !strings.Contains(out, want) {
			t.Fatalf("take output missing %q:\n%s", want, out)
		}
	}

	entry := venueOrder(t, fb, "NVDA", broker.Limit)
	if entry.Qty != 16 || entry.LimitPrice == nil || *entry.LimitPrice != 120.60 {
		t.Fatalf("the venue got %+v, want a limit for 16 shares at 120.60", entry)
	}
	if stop := venueOrder(t, fb, "NVDA", broker.OrderType("stop")); stop.Side != broker.Sell {
		t.Fatalf("the bracket must rest a stop leg, got %+v", stop)
	}

	p := proposalRow(t, 1)
	if p.Status != journal.ProposalTaken || p.OrderID == nil {
		t.Fatalf("proposal 1 = %+v, want taken with an order", p)
	}

	list, err := run(t, "proposals")
	if err != nil {
		t.Fatalf("proposals: %v", err)
	}
	for _, want := range []string{"NVDA", "M2", "$120.60", "taken", "order #1", "AAPL", "rejected"} {
		if !strings.Contains(list, want) {
			t.Fatalf("proposals table missing %q:\n%s", want, list)
		}
	}
}

// --qty may lower the sized quantity; raising it would put the trade outside the
// cap the size was computed from.
func TestTakeQtyMayOnlyLowerTheSize(t *testing.T) {
	newSlateHome(t)

	if _, err := run(t, "take", "1", "--qty", "20"); err == nil || !strings.Contains(err.Error(), "never raise it") {
		t.Fatalf("raising the size must be refused, got: %v", err)
	}
	out, err := run(t, "take", "1", "--qty", "4")
	if err != nil {
		t.Fatalf("take 1 --qty 4: %v", err)
	}
	if !strings.Contains(out, "proposal #1 NVDA 4 sh") {
		t.Fatalf("take must use the lowered size:\n%s", out)
	}
}

func TestTakeRefusesADecidedProposal(t *testing.T) {
	newSlateHome(t)

	// #3 was rejected by the sizing rules before the trader ever saw it.
	if _, err := run(t, "take", "3"); err == nil || !strings.Contains(err.Error(), "already rejected") {
		t.Fatalf("taking a rejected idea must be refused, got: %v", err)
	}
	if _, err := run(t, "pass", "1", "--reason", "no conviction"); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if _, err := run(t, "take", "1"); err == nil || !strings.Contains(err.Error(), "already passed") {
		t.Fatalf("taking a passed idea must be refused, got: %v", err)
	}
}

// A situational refusal is about today, not about the idea. The position limit
// clears when something closes, so the proposal has to still be there when it
// does — rejecting it permanently threw the idea away for the session.
func TestTakeRefusedByASituationalRuleStaysOpen(t *testing.T) {
	newSlateHome(t)
	setMaxPositions(t, 1)

	if _, err := run(t, "take", "1"); err != nil {
		t.Fatalf("take 1: %v", err)
	}
	out, err := run(t, "take", "2")
	if err == nil || !strings.Contains(err.Error(), "rule: max positions") {
		t.Fatalf("the second entry must trip the position limit, got %v:\n%s", err, out)
	}
	if !strings.Contains(out, "proposal #2 AMZN is still open") {
		t.Fatalf("take must say the idea survives the refusal:\n%s", out)
	}

	p := proposalRow(t, 2)
	if p.Status != journal.ProposalProposed || p.Reason != "" {
		t.Fatalf("proposal 2 = %+v, want it still open with no decision", p)
	}
	if got := refusalsOn(t, briefSession); len(got) != 1 || got[0].Rule != "max positions" {
		t.Fatalf("the refusal must be on the record, got %+v", got)
	}

	// The limit lifts and the idea is takeable again, which is the point.
	setMaxPositions(t, 3)
	if _, err := run(t, "take", "2"); err != nil {
		t.Fatalf("take 2 after the limit lifted: %v", err)
	}
	if p := proposalRow(t, 2); p.Status != journal.ProposalTaken {
		t.Fatalf("proposal 2 = %s, want taken", p.Status)
	}
}

// A rule that refuses the idea itself decides it: the numbers will not change.
func TestTakeRefusedByAnIntrinsicRuleIsRejected(t *testing.T) {
	newSlateHome(t)
	// A target under the entry is a losing bracket however the day goes.
	seedProposal(t, 4, "NVDA", 120.60, 119.10, 119.50)

	out, err := run(t, "take", "4")
	if err == nil || !strings.Contains(err.Error(), "rule: target above entry") {
		t.Fatalf("a target under the entry must be refused, got %v:\n%s", err, out)
	}
	if !strings.Contains(out, "proposal #4 NVDA is rejected on the record.") {
		t.Fatalf("take must say the idea was rejected:\n%s", out)
	}
	p := proposalRow(t, 4)
	if p.Status != journal.ProposalRejected || !strings.Contains(p.Reason, "rule: target above entry") {
		t.Fatalf("proposal 4 = %+v, want rejected naming the rule", p)
	}
}

func TestPassNeedsAReasonAndRecordsIt(t *testing.T) {
	newSlateHome(t)

	if _, err := run(t, "pass", "1"); err == nil || !strings.Contains(err.Error(), "--reason is required") {
		t.Fatalf("a pass without a reason must be refused, got: %v", err)
	}
	out, err := run(t, "pass", "1", "--reason", "gap already ran")
	if err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if !strings.Contains(out, "passed: proposal #1 NVDA — gap already ran") {
		t.Fatalf("pass output:\n%s", out)
	}
	if p := proposalRow(t, 1); p.Status != journal.ProposalPassed || p.Reason != "gap already ran" {
		t.Fatalf("proposal 1 = %+v, want passed with the reason", p)
	}
}

func TestWhyShowsTheSizingMath(t *testing.T) {
	newSlateHome(t)

	out, err := run(t, "why", "1")
	if err != nil {
		t.Fatalf("why 1: %v", err)
	}
	for _, want := range []string{
		"[paper] why 1", "#1  LONG NVDA — M2 momentum continuation",
		"reward/risk", "2.00R", "Holds the breakout shelf.",
		"$5,000.00 equity × 0.5% = $25.00",
		"$25.00 / ($120.60 − $119.10) = 16, rounded down",
		"risked at the stop", "$24.00", "notional", "$1,929.60",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("why output missing %q:\n%s", want, out)
		}
	}
}

// An idea nobody decided is closed out with the session, not carried overnight.
func TestEODExpiresTheOpenProposals(t *testing.T) {
	newSlateHome(t)

	out, err := run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	if !strings.Contains(out, "expired 2 open proposal(s)") {
		t.Fatalf("eod must expire the undecided ideas:\n%s", out)
	}
	if p := proposalRow(t, 1); p.Status != journal.ProposalExpired {
		t.Fatalf("proposal 1 = %s, want expired", p.Status)
	}
	// The one a rule already refused keeps the decision it carries.
	if p := proposalRow(t, 3); p.Status != journal.ProposalRejected {
		t.Fatalf("proposal 3 = %s, want rejected", p.Status)
	}
}

func TestProposalsWithoutASlateSaysSo(t *testing.T) {
	newBriefHome(t)

	out, err := run(t, "proposals")
	if err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if !strings.Contains(out, "no proposals for") || !strings.Contains(out, "tape brief") {
		t.Fatalf("proposals output:\n%s", out)
	}
}

// setMaxPositions rewrites the config's position limit for one test.
func setMaxPositions(t *testing.T, n int) {
	t.Helper()
	path, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolving config path: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	cfg.Risk.MaxPositions = n
	if err := config.Write(path, cfg.WithoutEnvSecrets()); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}

// seedProposal writes an idea straight into today's slate, so a test can reach
// a rule the briefing's own validation would never let a model propose.
func seedProposal(t *testing.T, index int, symbol string, entry, stop, target float64) {
	t.Helper()
	st, cfg := openTestJournal(t)
	p := &journal.Proposal{
		BriefingID:   1,
		Mode:         cfg.Mode,
		Day:          briefSession,
		Index:        index,
		Symbol:       symbol,
		Side:         "long",
		SetupID:      "M2",
		Entry:        entry,
		Stop:         stop,
		Target:       target,
		Qty:          10,
		RiskUSD:      float64(10) * (entry - stop),
		Thesis:       "seeded by the test",
		Invalidation: "seeded by the test",
		Confidence:   "medium",
	}
	if err := st.InsertProposals(context.Background(), []*journal.Proposal{p}); err != nil {
		t.Fatalf("seeding proposal %d: %v", index, err)
	}
}

// proposalRow reads one proposal straight from the journal, past the renderer.
func proposalRow(t *testing.T, index int) journal.Proposal {
	t.Helper()
	st, cfg := openTestJournal(t)
	p, err := st.ProposalByDayIndex(context.Background(), cfg.Mode, briefSession, index)
	if err != nil {
		t.Fatalf("proposal %d: %v", index, err)
	}
	return p
}

// venueOrder finds the order of a given type the fake venue is holding.
func venueOrder(t *testing.T, fb *fake.Broker, symbol string, kind broker.OrderType) broker.Order {
	t.Helper()
	orders, err := fb.ListOrders(context.Background(), broker.ListOrdersFilter{})
	if err != nil {
		t.Fatalf("listing venue orders: %v", err)
	}
	for _, o := range orders {
		if o.Symbol == symbol && o.Type == kind {
			return o
		}
	}
	t.Fatalf("no %s order for %s at the venue: %+v", kind, symbol, orders)
	return broker.Order{}
}

func openTestJournal(t *testing.T) (*journal.Store, config.Config) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	st, err := journal.Open(cfg.DBPath(), cfg.Account.StartingEquity)
	if err != nil {
		t.Fatalf("opening journal: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, cfg
}
