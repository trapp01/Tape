package config

import (
	"errors"
	"fmt"
)

func (c Config) Validate() error {
	if c.Mode != ModePaper && c.Mode != ModeLive {
		return fmt.Errorf("mode must be %q or %q, got %q", ModePaper, ModeLive, c.Mode)
	}
	if c.Account.StartingEquity <= 0 {
		return fmt.Errorf("account.starting_equity must be positive, got %v", c.Account.StartingEquity)
	}
	if c.Broker.Name != "alpaca" {
		return fmt.Errorf("broker.name %q is not supported (only \"alpaca\" for now)", c.Broker.Name)
	}
	if err := c.Costs.validate(); err != nil {
		return err
	}
	if err := c.Brief.validate(); err != nil {
		return err
	}
	if err := c.Risk.validate(); err != nil {
		return err
	}
	if c.Retro.Weeks < 1 {
		return fmt.Errorf("retro.weeks must be at least 1, got %d", c.Retro.Weeks)
	}
	return c.Gate.validate()
}

// validate keeps the gate from being lowered below what the design promised.
func (g GateConfig) validate() error {
	if g.MinMonths < 1 || g.MinSessions < 1 {
		return fmt.Errorf("gate.min_months and gate.min_sessions must be at least 1, got %d and %d", g.MinMonths, g.MinSessions)
	}
	if g.MinProfitFactor < 1 {
		return fmt.Errorf("gate.min_profit_factor must be at least 1, got %v", g.MinProfitFactor)
	}
	if g.MaxDrawdownPct <= 0 || g.MaxDrawdownPct > 50 {
		return fmt.Errorf("gate.max_drawdown_pct must be within (0, 50], got %v", g.MaxDrawdownPct)
	}
	if g.MaxRefusalsLastMonth < 0 {
		return fmt.Errorf("gate.max_refusals_last_month must not be negative, got %d", g.MaxRefusalsLastMonth)
	}
	if g.MinTrades < 30 {
		return fmt.Errorf("gate.min_trades must be at least 30, got %d; fewer trades cannot separate edge from noise", g.MinTrades)
	}
	if g.MaxNullPassRate <= 0 || g.MaxNullPassRate > 0.5 {
		return fmt.Errorf("gate.max_null_pass_rate must be within (0, 0.5], got %v", g.MaxNullPassRate)
	}
	return nil
}

// validate keeps the risk limits from being loosened into nothing. The gate is
// measured inside these walls, so a wall set to zero flatters every stat.
func (r RiskConfig) validate() error {
	if r.PerTradePct <= 0 || r.PerTradePct > 5 {
		return fmt.Errorf("risk.per_trade_pct must be within (0, 5], got %v", r.PerTradePct)
	}
	if r.MaxPositions < 1 {
		return fmt.Errorf("risk.max_positions must be at least 1, got %d", r.MaxPositions)
	}
	if r.MaxDailyLosses < 1 {
		return fmt.Errorf("risk.max_daily_losses must be at least 1, got %d", r.MaxDailyLosses)
	}
	if r.NoEntriesBeforeCloseMinutes < 0 {
		return fmt.Errorf("risk.no_entries_before_close_minutes must not be negative, got %d", r.NoEntriesBeforeCloseMinutes)
	}
	if r.MinRewardRisk < 1 {
		return fmt.Errorf("risk.min_reward_risk must be at least 1, got %v", r.MinRewardRisk)
	}
	if r.MaxEntryDeviationPct <= 0 {
		return fmt.Errorf("risk.max_entry_deviation_pct must be positive, got %v", r.MaxEntryDeviationPct)
	}
	return nil
}

func (b BriefConfig) validate() error {
	if b.RegimeSymbol == "" {
		return errors.New("brief.regime_symbol must be set")
	}
	// A zero threshold leaves "flat" undefined and makes an unchanged close both
	// up and down, so the call it grades could never be wrong.
	if b.CallThresholdPct <= 0 {
		return fmt.Errorf("brief.call_threshold_pct must be positive, got %v", b.CallThresholdPct)
	}
	if b.NewsLookbackHours < 0 || b.MoversTop < 0 || b.CalendarDays < 0 {
		return errors.New("brief.news_lookback_hours, movers_top, and calendar_days must not be negative")
	}
	return nil
}

// validate keeps the cost model from being switched off or inverted. The gate
// that decides whether tape ever trades real money is measured after these
// costs, so a negative one would flatter every stat it feeds.
func (c CostsConfig) validate() error {
	for _, f := range []struct {
		key   string
		value float64
	}{
		{"costs.slippage_bps", c.SlippageBps},
		{"costs.commission_per_share", c.CommissionPerShare},
		{"costs.commission_min", c.CommissionMin},
		{"costs.commission_max_pct", c.CommissionMaxPct},
	} {
		if f.value < 0 {
			return fmt.Errorf("%s must not be negative, got %v; a negative cost pays the trader "+
				"and the real-money gate is measured after costs", f.key, f.value)
		}
	}
	return nil
}
