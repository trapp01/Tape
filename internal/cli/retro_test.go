package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/llm"
)

// retroReply proposes two edits the default playbook can carry: a new setup under
// Setups, and a line under the regime posture.
const retroReply = `{
  "summary": "One trade and one call is not a sample; the only defensible change is a note, not a rule.",
  "findings": [
    {"title": "M1 is the only rule that traded", "evidence": "1 trade, 1 replay, +$0 expectancy", "confidence": "low"},
    {"title": "Every veto so far was on an extended open", "evidence": "1 pass, replayed to its target", "confidence": "low"}
  ],
  "diffs": [
    {"section": "## Setups", "change": "add", "rationale": "The record shows no second continuation rule.",
     "before": "", "after": "### M3 midday continuation\n\n- When: the noon high breaks on rising volume.\n- Invalidation: a close back under the noon high."},
    {"section": "## Posture by regime", "change": "add", "rationale": "The week's only regime was uptrend, low vol.",
     "before": "", "after": "**Note.** Every session in the record so far has been uptrend, low vol."}
  ]
}`

// useRetroProvider points `tape retro` at a canned reply for the rest of the test.
func useRetroProvider(t *testing.T, reply string) *fakeLLM {
	t.Helper()
	provider := &fakeLLM{reply: reply}
	previous := newRetroProvider
	newRetroProvider = func(config.Config) (llm.Provider, error) { return provider, nil }
	t.Cleanup(func() { newRetroProvider = previous })
	return provider
}

func TestRetroDryRunAsksNothing(t *testing.T) {
	scoredWeek(t)
	provider := useRetroProvider(t, retroReply)

	out, err := run(t, "retro", "--dry-run")
	if err != nil {
		t.Fatalf("retro --dry-run: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("a dry run must not ask the model, calls = %d", provider.calls)
	}
	for _, want := range []string{
		"--- system prompt", "--- user prompt", "WINDOW", "PASSES AND THEIR REPLAYS",
		"GATE", "RISK LIMITS", "PLAYBOOK", "nothing was archived",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry run output missing %q:\n%s", want, out)
		}
	}

	if _, err := run(t, "retro", "show", "latest"); err == nil {
		t.Fatal("a dry run must archive nothing")
	}
}

func TestRetroArchivesAndRenders(t *testing.T) {
	scoredWeek(t)
	useRetroProvider(t, retroReply)

	out, err := run(t, "retro")
	if err != nil {
		t.Fatalf("retro: %v", err)
	}
	for _, want := range []string{
		"[paper] retro", "review #1", "fake-model-1", "SUMMARY", "not a sample",
		"FINDINGS", "low confidence", "PLAYBOOK DIFFS", "add under ## Setups",
		"### M3 midday continuation", "tape retro apply 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("retro output missing %q:\n%s", want, out)
		}
	}

	shown, err := run(t, "retro", "show", "latest")
	if err != nil {
		t.Fatalf("retro show latest: %v", err)
	}
	if !strings.Contains(shown, "review #1") || !strings.Contains(shown, "### M3 midday continuation") {
		t.Fatalf("retro show did not re-render the archive:\n%s", shown)
	}
}

func TestRetroApplyRewritesThePlaybookAndRecordsAVersion(t *testing.T) {
	scoredWeek(t)
	useRetroProvider(t, retroReply)
	if _, err := run(t, "retro"); err != nil {
		t.Fatalf("retro: %v", err)
	}

	before := playbookText(t)
	out, err := run(t, "retro", "apply", "1", "--diff", "1")
	if err != nil {
		t.Fatalf("retro apply: %v", err)
	}
	for _, want := range []string{"applied 1 edit(s)", "previous playbook", "version", "the gate now reads only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("apply output missing %q:\n%s", want, out)
		}
	}

	after := playbookText(t)
	if !strings.Contains(after, "### M3 midday continuation") {
		t.Fatalf("the edit did not reach the file:\n%s", after)
	}
	if strings.Index(after, "### M3") > strings.Index(after, "## Risk rules") {
		t.Fatalf("the edit landed outside its section:\n%s", after)
	}
	if before == after {
		t.Fatal("the playbook did not change")
	}

	versions, err := run(t, "playbook", "versions")
	if err != nil {
		t.Fatalf("playbook versions: %v", err)
	}
	if !strings.Contains(versions, "applied 1 diff(s) from review #1") {
		t.Fatalf("the snapshot is not listed:\n%s", versions)
	}

	// The gate now reads only what came after the edit, and names the snapshot.
	stats, err := run(t, "stats", "--all")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.Contains(stats, "applied 1 diff(s) from review #1") {
		t.Fatalf("stats must name the snapshot the gate restarts at:\n%s", stats)
	}

	// An edit lands once.
	if _, err := run(t, "retro", "apply", "1", "--diff", "1"); err == nil || !strings.Contains(err.Error(), "lands once") {
		t.Fatalf("a second apply of the same edit must be refused, got: %v", err)
	}
}

func TestRetroApplyNeedsAChoice(t *testing.T) {
	scoredWeek(t)
	useRetroProvider(t, retroReply)
	if _, err := run(t, "retro"); err != nil {
		t.Fatalf("retro: %v", err)
	}

	if _, err := run(t, "retro", "apply", "1"); err == nil || !strings.Contains(err.Error(), "--diff 1,3, or --all") {
		t.Fatalf("apply with no choice must be refused, got: %v", err)
	}
	if _, err := run(t, "retro", "apply", "1", "--all", "--diff", "1"); err == nil || !strings.Contains(err.Error(), "pick one") {
		t.Fatalf("--all with --diff must be refused, got: %v", err)
	}
	if _, err := run(t, "retro", "show", "99"); err == nil {
		t.Fatal("showing a review that does not exist must fail")
	}
}

// A review that reaches for the risk rules is archived and refused: those
// numbers are enforced in Go.
func TestRetroRefusesADiffOnTheRiskRules(t *testing.T) {
	scoredWeek(t)
	useRetroProvider(t, `{
	  "summary": "The stops are too tight for the volatility in the record.",
	  "findings": [{"title": "Stops", "evidence": "1 stop-out", "confidence": "low"}],
	  "diffs": [{"section": "## Risk rules", "change": "add", "rationale": "Bigger size would have paid.",
	             "before": "", "after": "Per trade 2% of equity."}]
	}`)

	out, err := run(t, "retro")
	if err == nil {
		t.Fatalf("a diff on the risk rules must be refused:\n%s", out)
	}
	if !strings.Contains(err.Error(), "not the model's to edit") {
		t.Fatalf("refusal = %v", err)
	}
	// The reply is on the record even though it was refused.
	if _, err := run(t, "retro", "show", "latest"); err != nil {
		t.Fatalf("the refused review must still be archived: %v", err)
	}
}

func playbookText(t *testing.T) string {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	raw, err := os.ReadFile(cfg.PlaybookPath())
	if err != nil {
		t.Fatalf("reading the playbook: %v", err)
	}
	return string(raw)
}
