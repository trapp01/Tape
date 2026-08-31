package cli

import (
	"os"
	"strings"
	"testing"
)

// scoredWeek runs the whole day through the CLI: a briefing with three ideas,
// one taken, the rest decided by the close, then the evening scoring pass. It
// leaves a record with a graded call, graded notes, replays, and one trade.
func scoredWeek(t *testing.T) {
	t.Helper()
	newSlateHome(t)
	if _, err := run(t, "take", "2"); err != nil {
		t.Fatalf("take 2: %v", err)
	}
	if _, err := run(t, "pass", "1", "--reason", "already extended into the open"); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	atClock(t, briefEvening)
	if _, err := run(t, "eod"); err != nil {
		t.Fatalf("eod: %v", err)
	}
}

// One pass settles all three kinds: the call, the ideas nobody took, and the
// bias note on each watchlist symbol.
func TestScoreReplaysIdeasAndGradesNotes(t *testing.T) {
	newSlateHome(t)
	if _, err := run(t, "pass", "1", "--reason", "already extended into the open"); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	atClock(t, briefEvening)

	out, err := run(t, "score")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	for _, want := range []string{
		"Calls through " + briefSession, "SPY", "up ≥0.3%", "actual +0.50%",
		"Replays through " + briefSession, "#1 NVDA", "passed", "target",
		"#3 AAPL", "rejected",
		"Notes through " + briefSession, "NVDA", "bullish",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("score output missing %q:\n%s", want, out)
		}
	}

	// Every grade is written once, so a second pass has nothing left to do.
	again, err := run(t, "score")
	if err != nil {
		t.Fatalf("second score: %v", err)
	}
	for _, gone := range []string{"Replays through", "Notes through"} {
		if strings.Contains(again, gone) {
			t.Fatalf("a second pass regraded %s:\n%s", gone, again)
		}
	}
}

func TestStatsRendersEverySection(t *testing.T) {
	scoredWeek(t)

	out, err := run(t, "stats", "--all")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for _, want := range []string{
		"[paper] stats", "the whole record, through " + briefSession, "· paper",
		"TRADES", "EQUITY", "max drawdown",
		"BY SETUP", "M1", "BY REGIME", "uptrend",
		"CALLS / NOTES", "INSIDE NOISE BAND", "needs 3+ months to mean anything",
		"PROPOSALS", "passes that would have profited", "losses the vetoes avoided",
		"execution drag on takes", "decided by the stop-first rule",
		"REFUSALS", "SIGNIFICANCE", "GATE", "reading from",
		"months covered", "null pass rate", "setups identified",
		"the gate is shut",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("stats wrote colour to a buffer:\n%q", out)
	}
}

// The gate reads only what came after the newest playbook snapshot, and says so.
func TestStatsNamesThePlaybookSnapshotTheGateStartsAt(t *testing.T) {
	newSlateHome(t)

	out, err := run(t, "stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(out, "playbook version #1 (first snapshot)") {
		t.Fatalf("stats must name the snapshot the gate starts at:\n%s", out)
	}
	if !strings.Contains(out, "never graded on the record that produced it") {
		t.Fatalf("stats must say why the window restarts:\n%s", out)
	}
}

func TestGatePrintsTheTableAndSignificance(t *testing.T) {
	scoredWeek(t)

	out, err := run(t, "gate")
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	for _, want := range []string{"[paper] gate", "SIGNIFICANCE", "GATE", "CHECK", "NEEDED", "the gate is shut"} {
		if !strings.Contains(out, want) {
			t.Fatalf("gate output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "BY SETUP") {
		t.Fatalf("gate is the last section on its own, not the whole report:\n%s", out)
	}
}

func TestStatsRefusesTwoWindows(t *testing.T) {
	newHome(t, "5000")

	if _, err := run(t, "stats", "--all", "--month"); err == nil || !strings.Contains(err.Error(), "pick one window") {
		t.Fatalf("two windows must be refused, got: %v", err)
	}
	if _, err := run(t, "stats", "--from", "yesterday"); err == nil || !strings.Contains(err.Error(), "is not a") {
		t.Fatalf("an unparseable day must be refused, got: %v", err)
	}
	if _, err := run(t, "stats", "--from", "2026-08-28", "--to", "2026-08-01"); err == nil || !strings.Contains(err.Error(), "is after") {
		t.Fatalf("a backwards window must be refused, got: %v", err)
	}
}

// countVersions is how many snapshots the gate would restart at.
func countVersions(t *testing.T) int {
	t.Helper()
	out, err := run(t, "playbook", "versions")
	if err != nil {
		t.Fatalf("playbook versions: %v", err)
	}
	return strings.Count(out, "snapshot") + strings.Count(out, "changed")
}

// Adding a symbol changes what the model looks at, not what a trade means. A
// different model is a different analyst, and the record starts again.
func TestOnlyRuleChangesResetTheGateWindow(t *testing.T) {
	path := newHome(t, "5000")
	if _, err := run(t, "stats"); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if n := countVersions(t); n != 1 {
		t.Fatalf("%d snapshots after the first read, want the first one", n)
	}

	if _, err := run(t, "watchlist", "add", "TSLA"); err != nil {
		t.Fatalf("watchlist add: %v", err)
	}
	if _, err := run(t, "stats"); err != nil {
		t.Fatalf("stats after watchlist add: %v", err)
	}
	if n := countVersions(t); n != 1 {
		t.Fatalf("%d snapshots after a watchlist change, want the gate left alone", n)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config: %v", err)
	}
	edited := strings.Replace(string(raw), `model = 'claude-opus-5'`, `model = 'claude-haiku-4-5'`, 1)
	if edited == string(raw) {
		t.Fatalf("the config does not carry the model line:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	if _, err := run(t, "stats"); err != nil {
		t.Fatalf("stats after the model change: %v", err)
	}
	if n := countVersions(t); n != 2 {
		t.Fatalf("%d snapshots after a model change, want the gate to restart", n)
	}
}

// Reading the record must not need keys: stats, gate, and the version list are
// journal reads.
func TestRecordCommandsWorkWithoutKeys(t *testing.T) {
	newHome(t, "5000")

	for _, args := range [][]string{{"stats"}, {"gate"}, {"playbook", "versions"}} {
		out, err := run(t, args...)
		if err != nil {
			t.Fatalf("%v without keys: %v\n%s", args, err, out)
		}
		if !strings.HasPrefix(out, "[paper] ") {
			t.Fatalf("%v must open with the mode tag:\n%s", args, out)
		}
	}
}
