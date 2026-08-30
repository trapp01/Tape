package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/config"
)

// run executes one command against a fresh root and returns everything it wrote.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// useFake points the CLI at an in-memory venue for the rest of the test.
func useFake(t *testing.T, fb *fake.Broker) {
	t.Helper()
	previous := newBroker
	newBroker = func(config.Config) (broker.Broker, broker.MarketData, error) { return fb, fb, nil }
	t.Cleanup(func() { newBroker = previous })
}

// newHome runs `tape init` into a temp TAPE_HOME and returns the config path.
func newHome(t *testing.T, equity string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TAPE_HOME", dir)
	if _, err := run(t, "init", "--starting-equity", equity); err != nil {
		t.Fatalf("init: %v", err)
	}
	return filepath.Join(dir, "config.toml")
}

func TestInitWritesConfigAndJournal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAPE_HOME", dir)

	out, err := run(t, "init", "--starting-equity", "2500", "--llm-provider", "deepseek")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.HasPrefix(out, "[paper] init\n") {
		t.Fatalf("first line must carry the mode tag, got:\n%s", out)
	}
	for _, want := range []string{"ALPACA_API_KEY", "ALPACA_API_SECRET", "DEEPSEEK_API_KEY", alpacaKeysURL, "$2,500.00", "regardless of Alpaca's paper balance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("init output missing %q:\n%s", want, out)
		}
	}

	for _, name := range []string{"config.toml", "tape.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("init did not create %s: %v", name, err)
		}
	}

	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("loading written config: %v", err)
	}
	if cfg.Account.StartingEquity != 2500 || cfg.LLM.Provider != "deepseek" || cfg.LLM.Model != "deepseek-chat" {
		t.Fatalf("written config = %+v", cfg)
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAPE_HOME", dir)
	if _, err := run(t, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}

	_, err := run(t, "init")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second init must refuse and mention --force, got: %v", err)
	}
	if _, err := run(t, "init", "--force"); err != nil {
		t.Fatalf("forced init: %v", err)
	}
}

func TestModeLiveIsRefused(t *testing.T) {
	newHome(t, "5000")

	out, err := run(t, "mode", "live")
	if err == nil {
		t.Fatal("mode live must refuse")
	}
	if !strings.Contains(err.Error(), "real-money gate") || !strings.Contains(err.Error(), "paper-only") {
		t.Fatalf("refusal must name the gate, got: %v", err)
	}
	if !strings.HasPrefix(out, "[paper] mode\n") {
		t.Fatalf("mode live still prints the tag first, got:\n%s", out)
	}
}

func TestModePrintsAndSetsPaper(t *testing.T) {
	path := newHome(t, "5000")

	out, err := run(t, "mode")
	if err != nil {
		t.Fatalf("mode: %v", err)
	}
	if !strings.Contains(out, "mode    paper") {
		t.Fatalf("mode output:\n%s", out)
	}
	if _, err := run(t, "mode", "paper"); err != nil {
		t.Fatalf("mode paper: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if cfg.Mode != config.ModePaper {
		t.Fatalf("mode = %q", cfg.Mode)
	}
}

func TestLLMProvidersLists(t *testing.T) {
	newHome(t, "5000")

	out, err := run(t, "llm", "providers")
	if err != nil {
		t.Fatalf("llm providers: %v", err)
	}
	for _, want := range []string{"[paper] llm providers", "anthropic", "ANTHROPIC_API_KEY", "openrouter", "ollama"} {
		if !strings.Contains(out, want) {
			t.Fatalf("providers output missing %q:\n%s", want, out)
		}
	}
}

func TestWatchBoardFrame(t *testing.T) {
	b := newBoard([]string{"AAPL", "MSFT"})
	b.update(broker.Quote{Symbol: "AAPL", Bid: 190.20, Ask: 190.30, Last: 190.25, Timestamp: time.Date(2026, 8, 30, 14, 5, 6, 0, time.UTC)})

	lines := b.frame(&app{loc: time.UTC})
	if len(lines) != 3 {
		t.Fatalf("want a header and two rows, got %d lines: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "SPREAD") {
		t.Fatalf("header = %q", lines[0])
	}
	// (190.30 - 190.20) / 190.25 * 10000 = 5.3 bps
	if !strings.Contains(lines[1], "$190.20") || !strings.Contains(lines[1], "5.3") || !strings.Contains(lines[1], "14:05:06") {
		t.Fatalf("AAPL row = %q", lines[1])
	}
	if !strings.Contains(lines[2], "waiting") {
		t.Fatalf("a symbol with no quote yet should say so, got %q", lines[2])
	}
}

func TestMoneyFormatting(t *testing.T) {
	cases := map[float64]string{
		0:           "$0.00",
		1234.5:      "$1,234.50",
		-1234.567:   "-$1,234.57",
		1000000:     "$1,000,000.00",
		-0.5:        "-$0.50",
		999.994:     "$999.99",
		12345678.91: "$12,345,678.91",
	}
	for in, want := range cases {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
	if got := signedMoney(12.3); got != "+$12.30" {
		t.Errorf("signedMoney(12.3) = %q", got)
	}
	if got := signedMoney(-12.3); got != "-$12.30" {
		t.Errorf("signedMoney(-12.3) = %q", got)
	}
}

// An amount that rounds to nothing has no sign; -$0.00 reads as a loss.
func TestMoneyNormalisesNegativeZero(t *testing.T) {
	for _, v := range []float64{-0.001, -0.0049, -0.0} {
		if got := money(v); got != "$0.00" {
			t.Errorf("money(%v) = %q, want $0.00", v, got)
		}
	}
	for _, v := range []float64{-0.001, 0.001} {
		if got := signedMoney(v); got != "$0.00" {
			t.Errorf("signedMoney(%v) = %q, want $0.00", v, got)
		}
	}
}

// The real-money gate is shut, so no command may print a bare [LIVE] tag over a
// paper account it is not allowed to leave.
func TestLiveBannerSaysLocked(t *testing.T) {
	path := newHome(t, "5000")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	cfg.Mode = config.ModeLive
	if err := config.Write(path, cfg); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	out, err := run(t, "mode")
	if err != nil {
		t.Fatalf("mode: %v", err)
	}
	if !strings.HasPrefix(out, "[LIVE — locked] mode\n") {
		t.Fatalf("banner must say the gate is shut, got:\n%s", out)
	}
	if strings.Contains(out, "[LIVE]") {
		t.Fatalf("bare [LIVE] tag in:\n%s", out)
	}
}
