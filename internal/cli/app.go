package cli

import (
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
		Broker:  bk,
		Data:    data,
		Journal: st,
		Costs:   costModel(cfg),
		Mode:    cfg.Mode,
		Loc:     loc,
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
	return &app{cfg: cfg, loc: loc, jnl: st, out: out, style: style}, nil
}

func (a *app) Close() error { return a.jnl.Close() }

// today is the current calendar day in the configured zone, which is where every
// day boundary in tape is measured.
func (a *app) today() string { return timeNow().In(a.loc).Format(dayLayout) }

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
