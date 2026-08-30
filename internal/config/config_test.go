package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F11: Validate ignored [costs] entirely, so slippage_bps = -25 made every
// modeled fill favourable and flattered the record the gate is measured on.
func TestValidateRejectsAFavourableCostModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"negative slippage", func(c *Config) { c.Costs.SlippageBps = -25 }, "costs.slippage_bps"},
		{"negative per-share commission", func(c *Config) { c.Costs.CommissionPerShare = -0.005 }, "costs.commission_per_share"},
		{"negative commission floor", func(c *Config) { c.Costs.CommissionMin = -1 }, "costs.commission_min"},
		{"negative commission cap", func(c *Config) { c.Costs.CommissionMaxPct = -1 }, "costs.commission_max_pct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the setting", err)
			}
			if !strings.Contains(err.Error(), "gate") {
				t.Fatalf("error %q does not say why the cost model matters", err)
			}
		})
	}

	// A zero model is still allowed; only a negative one pays the trader.
	cfg := Default()
	cfg.Costs = CostsConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a zero cost model must validate: %v", err)
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("the default cost model must validate: %v", err)
	}
}

func TestLoadRejectsNegativeSlippage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "mode = \"paper\"\n\n[account]\nstarting_equity = 5000\n\n[broker]\nname = \"alpaca\"\n\n[costs]\nslippage_bps = -25\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("a config with negative slippage loaded")
	}
	if !strings.Contains(err.Error(), "costs.slippage_bps") {
		t.Fatalf("error = %v, want it to name costs.slippage_bps", err)
	}
}
