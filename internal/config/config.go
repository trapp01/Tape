// Package config loads ~/.tape/config.toml and applies environment overrides
// for secrets, so keys never have to live in the file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrNotFound means no config file exists yet; the CLI should point at `tape init`.
var ErrNotFound = errors.New("config not found")

const (
	ModePaper = "paper"
	ModeLive  = "live"
)

type Config struct {
	// Mode is "paper" or "live". Live is refused until the Phase 4 gate exists.
	Mode    string        `toml:"mode"`
	Account AccountConfig `toml:"account"`
	Broker  BrokerConfig  `toml:"broker"`
	Costs   CostsConfig   `toml:"costs"`
	LLM     LLMConfig     `toml:"llm"`
	Data    DataConfig    `toml:"data"`
	Brief   BriefConfig   `toml:"brief"`

	// Home is the directory the config was loaded from. Not written to the file.
	Home string `toml:"-"`
}

// DataConfig holds keys for the optional calendar sources. Each one is also
// read from the environment: FRED_API_KEY, FINNHUB_API_KEY.
type DataConfig struct {
	FREDAPIKey    string `toml:"fred_api_key"`
	FinnhubAPIKey string `toml:"finnhub_api_key"`
}

// BriefConfig shapes the morning briefing.
type BriefConfig struct {
	// Watchlist is the set of symbols the briefing reads and the model may note.
	Watchlist []string `toml:"watchlist"`
	// IndexSymbols are the broad-market ETFs read for the market line and the regime.
	IndexSymbols []string `toml:"index_symbols"`
	// RegimeSymbol is the instrument the regime is classified from and the
	// default instrument for the call of the day.
	RegimeSymbol string `toml:"regime_symbol"`
	// CallThresholdPct is the move (in percent) an "up" or "down" call must
	// clear to count as correct when the model leaves the threshold null.
	CallThresholdPct  float64 `toml:"call_threshold_pct"`
	NewsLookbackHours int     `toml:"news_lookback_hours"`
	MoversTop         int     `toml:"movers_top"`
	// CalendarDays is how far ahead the calendar looks, in days.
	CalendarDays int `toml:"calendar_days"`
}

type AccountConfig struct {
	// StartingEquity is tape's own ledger size. Alpaca's paper balance is ignored
	// so stats reflect an account the user would actually fund.
	StartingEquity float64 `toml:"starting_equity"`
	// Timezone is the IANA zone day boundaries are measured in, e.g.
	// "America/Edmonton". Empty means the machine's local zone.
	Timezone string `toml:"timezone"`
}

type BrokerConfig struct {
	Name   string       `toml:"name"`
	Alpaca AlpacaConfig `toml:"alpaca"`
}

type AlpacaConfig struct {
	APIKey    string `toml:"api_key"`
	APISecret string `toml:"api_secret"`
	// DataFeed is "iex" (free) or "sip" (paid Algo Trader Plus).
	DataFeed string `toml:"data_feed"`
}

type CostsConfig struct {
	SlippageBps        float64 `toml:"slippage_bps"`
	CommissionPerShare float64 `toml:"commission_per_share"`
	CommissionMin      float64 `toml:"commission_min"`
	CommissionMaxPct   float64 `toml:"commission_max_pct"`
}

type LLMConfig struct {
	// Provider is "anthropic", "claude-code" (the local Claude Code login, personal
	// use), an OpenAI-compatible preset ("openrouter", "zai", "deepseek", "openai",
	// "groq", "ollama"), or "openai-compatible" with BaseURL set.
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	BaseURL  string `toml:"base_url"`
	APIKey   string `toml:"api_key"`
}

// Default returns the config `tape init` writes before the user edits anything.
func Default() Config {
	return Config{
		Mode:    ModePaper,
		Account: AccountConfig{StartingEquity: 5000},
		Broker:  BrokerConfig{Name: "alpaca", Alpaca: AlpacaConfig{DataFeed: "iex"}},
		Costs: CostsConfig{
			SlippageBps:        5,
			CommissionPerShare: 0.005,
			CommissionMin:      1.00,
			CommissionMaxPct:   1.0,
		},
		LLM: LLMConfig{Provider: "anthropic", Model: "claude-opus-5"},
		Brief: BriefConfig{
			Watchlist:         []string{"SPY", "QQQ", "AAPL", "MSFT", "NVDA", "AMZN", "GOOGL", "META"},
			IndexSymbols:      []string{"SPY", "QQQ", "IWM", "DIA"},
			RegimeSymbol:      "SPY",
			CallThresholdPct:  0.3,
			NewsLookbackHours: 18,
			MoversTop:         10,
			CalendarDays:      3,
		},
	}
}

// PlaybookPath is the strategy file next to the config.
func (c Config) PlaybookPath() string {
	return filepath.Join(c.Home, "playbook.md")
}

// HomeDir is $TAPE_HOME if set, otherwise ~/.tape.
func HomeDir() (string, error) {
	if h := os.Getenv("TAPE_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".tape"), nil
}

// envSecrets is the one table of credentials the environment may supply. Load
// reads it to override the file; WithoutEnvSecrets reads it to clear them again,
// so a command that rewrites the file cannot put a key in it.
var envSecrets = []struct {
	env   string
	field func(*Config) *string
}{
	{"ALPACA_API_KEY", func(c *Config) *string { return &c.Broker.Alpaca.APIKey }},
	{"ALPACA_API_SECRET", func(c *Config) *string { return &c.Broker.Alpaca.APISecret }},
	{"FRED_API_KEY", func(c *Config) *string { return &c.Data.FREDAPIKey }},
	{"FINNHUB_API_KEY", func(c *Config) *string { return &c.Data.FinnhubAPIKey }},
}

// WithoutEnvSecrets clears every secret whose value came from the environment.
// A key the user typed into the file stays where they put it.
func (c Config) WithoutEnvSecrets() Config {
	for _, s := range envSecrets {
		field := s.field(&c)
		if v := os.Getenv(s.env); v != "" && v == *field {
			*field = ""
		}
	}
	return c
}

// Load reads the config at path, or the default location when path is empty.
// Secrets in the environment override the file; envSecrets lists them.
func Load(path string) (Config, error) {
	if path == "" {
		home, err := HomeDir()
		if err != nil {
			return Config{}, err
		}
		path = filepath.Join(home, "config.toml")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("%w at %s (run `tape init`)", ErrNotFound, path)
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg := Default()
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	cfg.Home = filepath.Dir(path)
	for _, s := range envSecrets {
		if v := os.Getenv(s.env); v != "" {
			*s.field(&cfg) = v
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Write serialises cfg to path, creating the directory if needed.
func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	raw, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

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
	return c.Brief.validate()
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

// DBPath is the SQLite journal next to the config file.
func (c Config) DBPath() string {
	return filepath.Join(c.Home, "tape.db")
}

// Location resolves account.timezone. Day boundaries — today's recap, flat by
// close — are measured here rather than in UTC.
func (c Config) Location() (*time.Location, error) {
	if c.Account.Timezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(c.Account.Timezone)
	if err != nil {
		return nil, fmt.Errorf("account.timezone %q: %w", c.Account.Timezone, err)
	}
	return loc, nil
}
