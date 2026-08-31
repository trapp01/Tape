package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// The stop requirement is a guardrail, not a flag check, so the refusal reaches
// the record the same way every other rule does.
func TestBuyWithoutAStopIsRefusedAndRecorded(t *testing.T) {
	newHome(t, "5000")
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	useFake(t, fb)

	_, err := run(t, "buy", "AAPL", "10")
	if err == nil || !strings.Contains(err.Error(), "rule: no entry without a stop") {
		t.Fatalf("a buy with no stop must be refused by the rule, got: %v", err)
	}

	got := refusalsOn(t, sessionDayOf(t))
	if len(got) != 1 {
		t.Fatalf("want one recorded refusal, got %d", len(got))
	}
	if got[0].Rule != "no entry without a stop" || got[0].Symbol != "AAPL" || got[0].Source != journal.SourceHuman {
		t.Fatalf("refusal = %+v", got[0])
	}
}

func TestStatusShowsTheRiskWallsAndTheHalt(t *testing.T) {
	newHome(t, "5000")
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	useFake(t, fb)

	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"Risk (enforced in Go)",
		"per trade", "0.5% of equity = $25.00",
		"max positions", "daily loss limit", "2 closed losses",
		"entries stop", "30 min before the close",
		"refusals today",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "HALT") {
		t.Fatalf("a clean day is not halted:\n%s", out)
	}

	// Two losing positions, not two losing exits: the halt counts symbols.
	losingTrade(t, fb, "AAPL", 100, "98", 99)
	losingTrade(t, fb, "MSFT", 200, "198", 199)

	out, err = run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "HALT") || !strings.Contains(out, "2 positions closed at a loss today") {
		t.Fatalf("two losses must halt the day:\n%s", out)
	}

	_, err = run(t, "buy", "AAPL", "1", "--stop", "97")
	if err == nil || !strings.Contains(err.Error(), "rule: daily halt") {
		t.Fatalf("a halted day must refuse a new entry, got: %v", err)
	}
}

// losingTrade opens ten shares with a stop and closes them lower. The exit
// clears its own resting stop leg out of the way.
func losingTrade(t *testing.T, fb *fake.Broker, symbol string, entry float64, stop string, exit float64) {
	t.Helper()
	fb.SetPrice(symbol, entry)
	if _, err := run(t, "buy", symbol, "10", "--stop", stop); err != nil {
		t.Fatalf("buy %s at %v: %v", symbol, entry, err)
	}
	fb.SetPrice(symbol, exit)
	if _, err := run(t, "sell", symbol, "10"); err != nil {
		t.Fatalf("sell %s at %v: %v", symbol, exit, err)
	}
}

// refusalsOn reads the guardrail refusals recorded for one session.
func refusalsOn(t *testing.T, day string) []journal.Refusal {
	t.Helper()
	st, cfg := openTestJournal(t)
	got, err := st.RefusalsForDay(context.Background(), cfg.Mode, day)
	if err != nil {
		t.Fatalf("refusals for %s: %v", day, err)
	}
	return got
}

// sessionDayOf is the day refusals are stamped with: the venue's session, not
// the reader's calendar day.
func sessionDayOf(t *testing.T) string {
	t.Helper()
	return market.SessionDate(timeNow())
}
