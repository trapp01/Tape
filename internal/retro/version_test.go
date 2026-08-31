package retro

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/config"
)

// The first read of a record snapshots the rules in force, so every later stat
// has something to be compared against.
func TestEnsureVersionTakesTheFirstSnapshot(t *testing.T) {
	st := newFakeStore()
	path := applyHome(t)
	cfg := config.Default()

	v, err := EnsureVersion(context.Background(), st, cfg, path)
	if err != nil {
		t.Fatalf("EnsureVersion: %v", err)
	}
	if v == nil || v.Note != "first snapshot" || v.Path != path {
		t.Fatalf("version = %+v", v)
	}
	if v.SHA256 != sha256Hex([]byte(testPlaybook)) {
		t.Fatalf("the snapshot does not fingerprint the file")
	}
}

// Nothing moved, so nothing is recorded. A snapshot per command would reset the
// gate window every morning.
func TestEnsureVersionIsQuietWhenNothingChanged(t *testing.T) {
	st := newFakeStore()
	path := applyHome(t)
	cfg := config.Default()

	if _, err := EnsureVersion(context.Background(), st, cfg, path); err != nil {
		t.Fatalf("first EnsureVersion: %v", err)
	}
	v, err := EnsureVersion(context.Background(), st, cfg, path)
	if err != nil {
		t.Fatalf("second EnsureVersion: %v", err)
	}
	if v != nil || len(st.versions) != 1 {
		t.Fatalf("a second snapshot of an unchanged record: %+v", st.versions)
	}
}

// A rule edited by hand resets the gate exactly like one a review applied.
func TestEnsureVersionSeesAPlaybookEditedByHand(t *testing.T) {
	st := newFakeStore()
	path := applyHome(t)
	cfg := config.Default()

	if _, err := EnsureVersion(context.Background(), st, cfg, path); err != nil {
		t.Fatalf("first EnsureVersion: %v", err)
	}
	if err := os.WriteFile(path, []byte(testPlaybook+"\n### M2 a rule I typed\n"), 0o600); err != nil {
		t.Fatalf("editing the playbook: %v", err)
	}

	v, err := EnsureVersion(context.Background(), st, cfg, path)
	if err != nil {
		t.Fatalf("EnsureVersion: %v", err)
	}
	if v == nil || !strings.Contains(v.Note, "playbook changed") {
		t.Fatalf("version = %+v", v)
	}
}

// The risk, cost, and brief numbers are what a stat is comparable within, so
// moving one starts the window again.
func TestEnsureVersionSeesAConfigChange(t *testing.T) {
	st := newFakeStore()
	path := applyHome(t)
	cfg := config.Default()

	if _, err := EnsureVersion(context.Background(), st, cfg, path); err != nil {
		t.Fatalf("first EnsureVersion: %v", err)
	}
	cfg.Risk.PerTradePct = 1.0

	v, err := EnsureVersion(context.Background(), st, cfg, path)
	if err != nil {
		t.Fatalf("EnsureVersion: %v", err)
	}
	if v == nil || v.Note != "config changed" {
		t.Fatalf("version = %+v", v)
	}
}

func hashOf(t *testing.T, cfg config.Config) string {
	t.Helper()
	sum, err := ConfigHash(cfg)
	if err != nil {
		t.Fatalf("ConfigHash: %v", err)
	}
	return sum
}

// The fingerprint covers what changes the meaning of a trade. Adding a symbol to
// the watchlist changes what the model looks at, not what a trade means, and a
// key is not a rule at all.
func TestConfigHashIgnoresWhatDoesNotChangeATrade(t *testing.T) {
	base := hashOf(t, config.Default())

	for name, edit := range map[string]func(*config.Config){
		"a watchlist symbol": func(c *config.Config) { c.Brief.Watchlist = append(c.Brief.Watchlist, "TSLA") },
		"the index symbols":  func(c *config.Config) { c.Brief.IndexSymbols = []string{"SPY"} },
		"the news lookback":  func(c *config.Config) { c.Brief.NewsLookbackHours = 48 },
		"the movers count":   func(c *config.Config) { c.Brief.MoversTop = 20 },
		"the calendar span":  func(c *config.Config) { c.Brief.CalendarDays = 9 },
		"an api key":         func(c *config.Config) { c.LLM.APIKey = "sk-not-a-rule" },
		"a base url":         func(c *config.Config) { c.LLM.BaseURL = "https://elsewhere.example" },
		"the timezone":       func(c *config.Config) { c.Account.Timezone = "America/Denver" },
		"the venue keys":     func(c *config.Config) { c.Broker.Alpaca.APIKey = "not-a-rule" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			edit(&cfg)
			if got := hashOf(t, cfg); got != base {
				t.Fatalf("%s reset the gate window", name)
			}
		})
	}
}

// A different model is a different analyst, and the record it produced is a
// different record. So are the walls, the costs, and the bar a call clears.
func TestConfigHashCoversWhatChangesATrade(t *testing.T) {
	base := hashOf(t, config.Default())

	for name, edit := range map[string]func(*config.Config){
		"the model":          func(c *config.Config) { c.LLM.Model = "some-other-model" },
		"the provider":       func(c *config.Config) { c.LLM.Provider = "openrouter" },
		"the risk cap":       func(c *config.Config) { c.Risk.PerTradePct = 1.0 },
		"the cost model":     func(c *config.Config) { c.Costs.SlippageBps = 25 },
		"the regime symbol":  func(c *config.Config) { c.Brief.RegimeSymbol = "QQQ" },
		"the call threshold": func(c *config.Config) { c.Brief.CallThresholdPct = 0.6 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			edit(&cfg)
			if got := hashOf(t, cfg); got == base {
				t.Fatalf("%s moved and the fingerprint did not", name)
			}
		})
	}
}

// A strategy file the gate cannot fingerprint is a gap in the record, not a
// silent pass: `tape init` and `tape playbook --write` both write one.
func TestEnsureVersionNamesAMissingPlaybook(t *testing.T) {
	st := newFakeStore()
	path := filepath.Join(t.TempDir(), "playbook.md")

	_, err := EnsureVersion(context.Background(), st, config.Default(), path)
	if err == nil {
		t.Fatal("a missing playbook must be reported")
	}
	for _, want := range []string{path, "playbook --write"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(st.versions) != 0 {
		t.Fatalf("a missing playbook was snapshotted: %+v", st.versions)
	}
}
