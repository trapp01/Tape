package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/config"
)

func TestWatchlistRoundTrips(t *testing.T) {
	path := newHome(t, "5000")

	out, err := run(t, "watchlist", "add", "tsla", "AMD", "tsla")
	if err != nil {
		t.Fatalf("watchlist add: %v", err)
	}
	if !strings.HasPrefix(out, "[paper] watchlist add\n") {
		t.Fatalf("watchlist add banner:\n%s", out)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	want := append(config.Default().Brief.Watchlist, "TSLA", "AMD")
	if strings.Join(cfg.Brief.Watchlist, ",") != strings.Join(want, ",") {
		t.Fatalf("watchlist = %v, want %v", cfg.Brief.Watchlist, want)
	}

	if _, err := run(t, "watchlist", "rm", "spy", "AMD"); err != nil {
		t.Fatalf("watchlist rm: %v", err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	for _, gone := range []string{"SPY", "AMD"} {
		if strings.Contains(strings.Join(cfg.Brief.Watchlist, ","), gone) {
			t.Fatalf("%s should be gone, watchlist = %v", gone, cfg.Brief.Watchlist)
		}
	}

	ls, err := run(t, "watchlist", "ls")
	if err != nil {
		t.Fatalf("watchlist ls: %v", err)
	}
	if !strings.Contains(ls, "TSLA") || strings.Contains(ls, " AMD") {
		t.Fatalf("watchlist ls:\n%s", ls)
	}
}

// No env-supplied key may end up in the file a command rewrites, whichever
// source it belongs to.
func TestConfigEditsKeepEverySecretOutOfTheFile(t *testing.T) {
	secrets := map[string]string{
		"ALPACA_API_KEY":    "PKTESTKEY",
		"ALPACA_API_SECRET": "PKTESTSECRET",
		"FRED_API_KEY":      "FREDTESTKEY",
		"FINNHUB_API_KEY":   "FINNHUBTESTKEY",
	}

	for _, edit := range [][]string{{"watchlist", "add", "TSLA"}, {"mode", "paper"}} {
		t.Run(edit[0], func(t *testing.T) {
			path := newHome(t, "5000")
			for name, value := range secrets {
				t.Setenv(name, value)
			}

			if _, err := run(t, edit...); err != nil {
				t.Fatalf("%v: %v", edit, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}
			for name, value := range secrets {
				if bytes.Contains(raw, []byte(value)) {
					t.Errorf("config.toml gained %s from the environment:\n%s", name, raw)
				}
			}
		})
	}
}

func TestPlaybookPrintsAndWrites(t *testing.T) {
	newHome(t, "5000")

	out, err := run(t, "playbook")
	if err != nil {
		t.Fatalf("playbook: %v", err)
	}
	if !strings.Contains(out, "[paper] playbook") || !strings.Contains(out, "playbook.md") || !strings.Contains(out, "M1 gap-and-go") {
		t.Fatalf("playbook output:\n%s", out)
	}

	// init already wrote one, so --write must leave it alone.
	written, err := run(t, "playbook", "--write")
	if err != nil {
		t.Fatalf("playbook --write: %v", err)
	}
	if !strings.Contains(written, "already exists") {
		t.Fatalf("playbook --write must not clobber the user's file:\n%s", written)
	}
}
