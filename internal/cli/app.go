package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/alpaca"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/risk"
	"github.com/trapp01/tape/internal/trading"
)

// newBroker and openJournal are package-level so tests can swap in an in-memory
// venue and a temporary journal without reaching the network.
var (
	newBroker = func(cfg config.Config) (broker.Broker, broker.MarketData, error) {
		c, err := alpaca.New(alpaca.Options{
			APIKey:    cfg.Broker.Alpaca.APIKey,
			APISecret: cfg.Broker.Alpaca.APISecret,
			Paper:     cfg.Mode == config.ModePaper,
			DataFeed:  cfg.Broker.Alpaca.DataFeed,
		})
		if err != nil {
			return nil, nil, err
		}
		return c, c, nil
	}

	openJournal = func(cfg config.Config) (*journal.Store, error) {
		return journal.Open(cfg.DBPath(), cfg.Account.StartingEquity)
	}

	// timeNow is the wall clock every command reads, so a test can pin the day
	// and the venue's hour without waiting for one.
	timeNow = time.Now

	// pollWindow is how long a submission waits for fills. Zero takes the engine's
	// default; a test shortens it so a resting limit order costs no real seconds.
	pollWindow time.Duration
)

// app is everything a command needs: config, journal, venue, engine, and the
// writer to print to.
type app struct {
	cfg    config.Config
	loc    *time.Location
	jnl    *journal.Store
	broker broker.Broker
	data   broker.MarketData
	engine *trading.Engine
	out    io.Writer
	style  styler
}

// newApp loads the config, opens the journal, builds the venue client, and wires
// the engine. The mode banner goes out as soon as the mode is known, so a command
// that fails on setup still says which account it was talking about.
func newApp(cmd *cobra.Command, headline string) (*app, error) {
	out := cmd.OutOrStdout()
	style := newStyler(out)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	style.banner(out, cfg.Mode, headline)

	loc, err := cfg.Location()
	if err != nil {
		return nil, err
	}
	st, err := openJournal(cfg)
	if err != nil {
		return nil, err
	}
	bk, data, err := newBroker(cfg)
	if err != nil {
		st.Close()
		return nil, err
	}
	engine, err := trading.New(trading.Deps{
		Broker:   bk,
		Data:     data,
		Journal:  st,
		Costs:    costModel(cfg),
		Limits:   riskLimits(cfg),
		Refusals: journalRefusals{st},
		Mode:     cfg.Mode,
		Loc:      loc,
		// One clock for the whole command, so a journal row, a refusal, and the day
		// the recap covers cannot disagree about what time it is.
		Now:        timeNow,
		PollWindow: pollWindow,
	})
	if err != nil {
		st.Close()
		return nil, err
	}

	return &app{cfg: cfg, loc: loc, jnl: st, broker: bk, data: data, engine: engine, out: out, style: style}, nil
}

// newRecordApp is newApp without a venue, for commands that only read the
// journal. Reading the archive must not require keys.
func newRecordApp(cmd *cobra.Command, headline string) (*app, error) {
	out := cmd.OutOrStdout()
	style := newStyler(out)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	style.banner(out, cfg.Mode, headline)

	loc, err := cfg.Location()
	if err != nil {
		return nil, err
	}
	st, err := openJournal(cfg)
	if err != nil {
		return nil, err
	}
	// The venue is optional here: reading the archive must not require keys. When
	// keys are present its clock is what says which session the slate belongs to.
	bk, data, err := newBroker(cfg)
	if err != nil {
		bk, data = nil, nil
	}
	return &app{cfg: cfg, loc: loc, jnl: st, broker: bk, data: data, out: out, style: style}, nil
}

func (a *app) Close() error { return a.jnl.Close() }

// sessionDay is the Eastern trading day the venue is in right now.
func (a *app) sessionDay() string { return market.SessionDate(timeNow()) }

// slateDay is the session the takeable slate is filed under, resolved the way a
// briefing files it: today while the market is open, otherwise the next session
// to open. An evening briefing is a call on tomorrow, so `tape take 1` has to
// look for it there. Without a clock the venue's current day is the best answer.
func (a *app) slateDay(ctx context.Context) string {
	if a.broker == nil {
		return a.sessionDay()
	}
	clk, err := a.broker.Clock(ctx)
	if err != nil || clk.IsOpen || clk.NextOpen.IsZero() {
		return a.sessionDay()
	}
	return market.SessionDate(clk.NextOpen)
}

// journalRefusals puts every guardrail refusal on the record. "Zero guardrail
// breaches in the final month" is counted from these rows.
type journalRefusals struct{ jnl *journal.Store }

func (j journalRefusals) Record(ctx context.Context, r journal.Refusal) error {
	return j.jnl.InsertRefusal(ctx, &r)
}

// riskLimits resolves the config's [risk] section into the walls the engine
// enforces and the briefing plans inside.
func riskLimits(cfg config.Config) risk.Limits {
	return risk.Limits{
		RequireStop:                 cfg.Risk.RequireStop,
		PerTradePct:                 cfg.Risk.PerTradePct,
		MaxPositions:                cfg.Risk.MaxPositions,
		MaxDailyLosses:              cfg.Risk.MaxDailyLosses,
		NoEntriesBeforeCloseMinutes: cfg.Risk.NoEntriesBeforeCloseMinutes,
		MinRewardRisk:               cfg.Risk.MinRewardRisk,
		MaxEntryDeviationPct:        cfg.Risk.MaxEntryDeviationPct,
	}
}

// costModel starts from the built-in defaults so regulatory fees stay set, then
// applies what config overrides.
func costModel(cfg config.Config) costs.Model {
	m := costs.Default()
	m.SlippageBps = cfg.Costs.SlippageBps
	m.CommissionPerShare = cfg.Costs.CommissionPerShare
	m.CommissionMin = cfg.Costs.CommissionMin
	m.CommissionMaxPct = cfg.Costs.CommissionMaxPct
	return m
}

// resolveConfigPath is the --config flag when set, otherwise $TAPE_HOME (or
// ~/.tape) plus config.toml.
func resolveConfigPath() (string, error) {
	if cfgPath != "" {
		return cfgPath, nil
	}
	home, err := config.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.toml"), nil
}

// bannerMode reads the mode without failing, so a command that runs before
// `tape init` still opens with a tag.
func bannerMode() string {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return config.ModePaper
	}
	return cfg.Mode
}

// bareStyler is for commands that print before any app exists.
func bareStyler(cmd *cobra.Command) (io.Writer, styler) {
	out := cmd.OutOrStdout()
	return out, newStyler(out)
}

// localZoneName resolves an IANA name for the machine's zone, or "" when the
// system only offers an abbreviation.
func localZoneName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		const marker = "zoneinfo/"
		if i := strings.LastIndex(target, marker); i >= 0 {
			name := target[i+len(marker):]
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	if name := time.Now().Location().String(); name != "" && name != "Local" {
		return name
	}
	return ""
}
