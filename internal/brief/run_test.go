package brief

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

const goodReply = `{
  "market_read": "Quiet tape, breadth narrow.",
  "regime_note": "Uptrend low vol: M2 continuations are live at normal size.",
  "calendar_note": "CPI at 06:30 MDT is the session's only scheduled risk.",
  "call": {
    "instrument": "SPY",
    "direction": "up",
    "threshold_pct": 0.4,
    "rationale": "M2: price above the 20d with yesterday's high taken out.",
    "invalidation": "SPY trades below 509.80, yesterday's low."
  },
  "watchlist": [{"symbol": "NVDA", "bias": "bullish", "note": "Holds above 118.40."}],
  "risks": ["CPI can reverse the open."]
}`

func TestRunArchivesAndFilesTheCall(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	p := &fakeProvider{reply: goodReply}

	res, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reused || res.Briefing.ID == 0 || res.Call == nil {
		t.Fatalf("first run must archive and file a call: %+v", res)
	}
	if res.Briefing.Day != "2026-08-28" || res.Call.Day != "2026-08-28" {
		t.Fatalf("days = briefing %q call %q", res.Briefing.Day, res.Call.Day)
	}
	if res.Call.Instrument != "SPY" || res.Call.Direction != "up" || res.Call.ThresholdPct != 0.4 {
		t.Fatalf("call = %+v", res.Call)
	}
	if res.Briefing.Provider != "fake" || res.Briefing.Model != "fake-model-1" {
		t.Fatalf("provider and model must be recorded: %+v", res.Briefing)
	}
	if res.Briefing.InputTokens != 1200 || res.Briefing.OutputTokens != 340 {
		t.Fatalf("tokens = %d in / %d out", res.Briefing.InputTokens, res.Briefing.OutputTokens)
	}
	if res.Output.Call.Invalidation == "" {
		t.Fatal("the decoded output must carry the invalidation")
	}
}

// The ritual is idempotent: reading the briefing again must not cost a second
// call or rewrite what was predicted.
func TestRunReusesTodaysBriefing(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	p := &fakeProvider{reply: goodReply}

	first, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if p.calls != 1 {
		t.Fatalf("the model was asked %d times, want 1", p.calls)
	}
	if !second.Reused || second.Briefing.ID != first.Briefing.ID {
		t.Fatalf("second run must return briefing #%d, got %+v", first.Briefing.ID, second.Briefing)
	}
	if second.Output.Call.Instrument != "SPY" || second.Input.Regime.Trend == "" {
		t.Fatalf("the reused result must decode the archive: %+v", second.Output)
	}
	if second.Call == nil || second.Call.ID != first.Call.ID {
		t.Fatalf("the reused result must carry the day's call, got %+v", second.Call)
	}
}

// Before the bell the call of the day is still open: a forced re-run replaces
// it rather than filing a prediction the record will never grade.
func TestRunForceReplacesTheCallBeforeTheOpen(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	p := &fakeProvider{reply: goodReply}

	first, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	d.Force = true
	p.reply = strings.Replace(goodReply, `"instrument": "SPY"`, `"instrument": "QQQ"`, 1)
	forced, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("forced run: %v", err)
	}
	if p.calls != 2 {
		t.Fatalf("force must ask the model again, calls = %d", p.calls)
	}
	if forced.Briefing.ID == first.Briefing.ID {
		t.Fatal("force must archive a new briefing")
	}
	if !forced.CallReplaced || forced.CallKept || forced.Call == nil {
		t.Fatalf("the call must be replaced before the open, got %+v", forced)
	}

	held, err := d.Journal.CallByDay(context.Background(), d.Mode, "2026-08-28")
	if err != nil {
		t.Fatalf("CallByDay: %v", err)
	}
	if held.ID != first.Call.ID {
		t.Fatalf("the session must keep one call row, got #%d and #%d", held.ID, first.Call.ID)
	}
	if held.Instrument != "QQQ" || held.BriefingID != forced.Briefing.ID {
		t.Fatalf("call = %+v, want the forced run's", held)
	}
}

// Once the bell rings the call is the record. A second read is a second read.
func TestRunForceKeepsTheCallAfterTheOpen(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	p := &fakeProvider{reply: goodReply}

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
	p.reply = strings.Replace(goodReply, `"instrument": "SPY"`, `"instrument": "QQQ"`, 1)
	forced, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("forced run: %v", err)
	}
	if !forced.CallKept || forced.CallReplaced || forced.Call != nil {
		t.Fatalf("the session's first call must stand, got %+v", forced)
	}

	held, err := d.Journal.CallByDay(context.Background(), d.Mode, "2026-08-28")
	if err != nil {
		t.Fatalf("CallByDay: %v", err)
	}
	if held.ID != first.Call.ID || held.Instrument != "SPY" {
		t.Fatalf("call = %+v, want the first one", held)
	}
}

// An evening run is a call on the next session, and the briefing is filed there
// too, so the next morning finds it instead of asking again.
func TestRunReusesAnEveningBriefingTheNextMorning(t *testing.T) {
	loc := mountain(t)
	friday := time.Date(2026, 8, 28, 19, 40, 0, 0, loc)
	monday := time.Date(2026, 8, 31, 7, 30, 0, 0, loc)

	d := testDeps(t, fullFeed(friday), friday, loc)
	d.Clock = func(context.Context) (broker.Clock, error) {
		return broker.Clock{IsOpen: false, NextOpen: monday, NextClose: monday.Add(6*time.Hour + 30*time.Minute)}, nil
	}
	p := &fakeProvider{reply: goodReply}

	evening, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("evening run: %v", err)
	}
	if evening.Briefing.Day != "2026-08-31" || evening.Call.Day != "2026-08-31" {
		t.Fatalf("the briefing and its call belong to the session they are for, got %q and %q",
			evening.Briefing.Day, evening.Call.Day)
	}

	d.Now = func() time.Time { return monday.Add(-38 * time.Minute) }
	morning, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("morning run: %v", err)
	}
	if p.calls != 1 {
		t.Fatalf("the morning must find Friday evening's briefing, calls = %d", p.calls)
	}
	if !morning.Reused || morning.Briefing.ID != evening.Briefing.ID {
		t.Fatalf("morning run = %+v, want evening briefing #%d", morning.Briefing, evening.Briefing.ID)
	}
}

// A reply that fails validation is still archived. Losing it would hide exactly
// the failure the record exists to show.
func TestRunArchivesAnInvalidReply(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)

	cases := map[string]string{
		"not json":       "I would rather write prose about the tape.",
		"fails validate": `{"market_read":"","regime_note":"x","calendar_note":"","call":{"instrument":"spy","direction":"up","threshold_pct":null,"rationale":"r","invalidation":"i"},"watchlist":[],"risks":[]}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			d := testDeps(t, fullFeed(now), now, loc)
			res, err := Run(context.Background(), d, &fakeProvider{reply: reply})
			if err == nil {
				t.Fatal("an unusable reply must be an error")
			}
			if !strings.Contains(err.Error(), "archived but its reply failed validation") {
				t.Fatalf("the error must say the briefing was kept, got: %v", err)
			}
			if res.Briefing.ID == 0 {
				t.Fatal("the briefing was not archived")
			}
			if !strings.Contains(err.Error(), "#"+strconv.FormatInt(res.Briefing.ID, 10)) {
				t.Fatalf("the error must name briefing #%d, got: %v", res.Briefing.ID, err)
			}

			stored, err := d.Journal.BriefingByID(context.Background(), res.Briefing.ID)
			if err != nil {
				t.Fatalf("BriefingByID: %v", err)
			}
			if len(stored.OutputJSON) == 0 {
				t.Fatal("the raw reply must be on the row")
			}
			if _, err := d.Journal.CallByDay(context.Background(), d.Mode, "2026-08-28"); err == nil {
				t.Fatal("an unvalidated reply must not file a call")
			}
		})
	}
}

// An archived reply that failed validation must not read back as a good one:
// reprinting it would put a call in front of the trader that was never accepted.
func TestRunRefusesToReuseAnInvalidBriefing(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	p := &fakeProvider{reply: `{"market_read":"","regime_note":"x","calendar_note":"",` +
		`"call":{"instrument":"SPY","direction":"up","threshold_pct":0.4,"rationale":"r","invalidation":"i"},` +
		`"watchlist":[],"risks":[]}`}

	first, err := Run(context.Background(), d, p)
	if err == nil {
		t.Fatal("an unusable reply must be an error")
	}

	second, err := Run(context.Background(), d, p)
	if err == nil {
		t.Fatalf("re-reading a failed briefing must not succeed, got %+v", second.Output)
	}
	if p.calls != 1 {
		t.Fatalf("the reuse path must not ask the model again, calls = %d", p.calls)
	}
	for _, want := range []string{
		"#" + strconv.FormatInt(first.Briefing.ID, 10), "failed validation", "market_read", "--force",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if second.Output.Call.Instrument != "" {
		t.Fatalf("the rejected call must not come back, got %+v", second.Output.Call)
	}

	d.Force = true
	p.reply = goodReply
	third, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatalf("--force must get past a failed briefing: %v", err)
	}
	if third.Call == nil {
		t.Fatal("the forced run must file the day's call")
	}
}

// A Saturday briefing is a call on Monday's session.
func TestRunFilesTheCallOnTheNextSession(t *testing.T) {
	loc := mountain(t)
	saturday := time.Date(2026, 8, 29, 9, 0, 0, 0, loc)
	monday := time.Date(2026, 8, 31, 7, 30, 0, 0, loc)

	d := testDeps(t, fullFeed(saturday), saturday, loc)
	d.Clock = func(context.Context) (broker.Clock, error) {
		return broker.Clock{IsOpen: false, NextOpen: monday, NextClose: monday.Add(6*time.Hour + 30*time.Minute)}, nil
	}

	res, err := Run(context.Background(), d, &fakeProvider{reply: goodReply})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Briefing.Day != "2026-08-31" || res.Call.Day != "2026-08-31" {
		t.Fatalf("both are filed on the session they are for, got %q and %q", res.Briefing.Day, res.Call.Day)
	}
}

// A null threshold takes the desk's configured default rather than zero.
func TestRunFallsBackToTheConfiguredThreshold(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	reply := strings.Replace(goodReply, `"threshold_pct": 0.4`, `"threshold_pct": null`, 1)

	res, err := Run(context.Background(), d, &fakeProvider{reply: reply})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Call.ThresholdPct != d.Cfg.CallThresholdPct {
		t.Fatalf("threshold = %v, want the configured %v", res.Call.ThresholdPct, d.Cfg.CallThresholdPct)
	}
}

// The prompt asks for a symbol from the data; Go is what enforces it. A call on
// something the briefing never carried cannot be graded against anything.
func TestRunRefusesACallOnAnUnseenSymbol(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	reply := strings.Replace(goodReply, `"instrument": "SPY"`, `"instrument": "DOGEUSD"`, 1)

	res, err := Run(context.Background(), d, &fakeProvider{reply: reply})
	if err == nil {
		t.Fatal("a call on a symbol outside the data must be refused")
	}
	if !strings.Contains(err.Error(), "DOGEUSD") {
		t.Fatalf("the error must name the symbol, got: %v", err)
	}
	if res.Briefing.ID == 0 {
		t.Fatal("the refused reply must still be archived")
	}
	if _, err := d.Journal.CallByDay(context.Background(), d.Mode, "2026-08-28"); err == nil {
		t.Fatal("a refused reply must not file a call")
	}
}

func TestRunRefusesWithoutAJournal(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	d.Journal = nil

	if _, err := Run(context.Background(), d, &fakeProvider{reply: goodReply}); err == nil {
		t.Fatal("Run must refuse to work off the record")
	}
}

// Which session this is decides which call the day carries, and that call locks
// at the open. A clock nobody can read is a failed morning, not a guess.
func TestRunRefusesWhenTheClockCannotBeRead(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	d.Clock = func(context.Context) (broker.Clock, error) {
		return broker.Clock{}, errors.New("venue unreachable")
	}

	_, err := Run(context.Background(), d, &fakeProvider{reply: goodReply})
	if err == nil {
		t.Fatal("Run must refuse when it cannot name the session")
	}
	if !strings.Contains(err.Error(), "venue clock") {
		t.Fatalf("error = %v, want it to name the clock", err)
	}
	calls, err := d.Journal.CallsInRange(context.Background(), d.Mode, "2026-08-01", "2026-09-30")
	if err != nil {
		t.Fatalf("CallsInRange: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("a call was filed on a session nobody could name: %+v", calls)
	}
}
