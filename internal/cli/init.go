package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/llm"
)

const alpacaKeysURL = "https://app.alpaca.markets"

func newInitCmd() *cobra.Command {
	var (
		startingEquity float64
		provider       string
		model          string
		force          bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write config.toml and create the journal",
		Long:  "init writes a fresh config.toml and an empty journal, then prints what to export before the first order.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)
			style.banner(out, config.ModePaper, "init")

			path, err := resolveConfigPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("checking %s: %w", path, err)
			}

			preset, ok := llm.FindPreset(provider)
			if !ok {
				return fmt.Errorf("unknown llm provider %q (valid: %s)", provider, strings.Join(llm.PresetNames(), ", "))
			}

			cfg := config.Default()
			cfg.Account.StartingEquity = startingEquity
			cfg.Account.Timezone = localZoneName()
			cfg.LLM.Provider = preset.Name
			cfg.LLM.Model = model
			if cfg.LLM.Model == "" {
				cfg.LLM.Model = preset.DefaultModel
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			if err := config.Write(path, cfg); err != nil {
				return err
			}

			cfg.Home = filepath.Dir(path)
			st, err := openJournal(cfg)
			if err != nil {
				return err
			}
			dbPath := st.Path()
			if err := st.Close(); err != nil {
				return err
			}

			printInitSteps(out, style, cfg, preset, path, dbPath)
			return nil
		},
	}

	cmd.Flags().Float64Var(&startingEquity, "starting-equity", config.Default().Account.StartingEquity, "size of tape's own ledger in dollars")
	cmd.Flags().StringVar(&provider, "llm-provider", config.Default().LLM.Provider, "llm provider ("+strings.Join(llm.PresetNames(), ", ")+")")
	cmd.Flags().StringVar(&model, "llm-model", "", "model id (default: the provider's)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config.toml")
	return cmd
}

func printInitSteps(w io.Writer, style styler, cfg config.Config, preset llm.Preset, cfgFile, dbFile string) {
	fmt.Fprintf(w, "\nconfig   %s\njournal  %s\n", cfgFile, dbFile)
	if cfg.Account.Timezone != "" {
		fmt.Fprintf(w, "timezone %s\n", cfg.Account.Timezone)
	}

	fmt.Fprintln(w, "\nNext steps")
	fmt.Fprintf(w, "  1. Get free Alpaca paper keys at %s\n", alpacaKeysURL)
	fmt.Fprintln(w, "  2. export ALPACA_API_KEY=...")
	fmt.Fprintln(w, "     export ALPACA_API_SECRET=...")
	switch {
	case preset.KeyEnv != "":
		fmt.Fprintf(w, "  3. export %s=...   (llm provider %s)\n", preset.KeyEnv, preset.Name)
	default:
		fmt.Fprintf(w, "  3. no key needed for %s (%s)\n", preset.Name, preset.BaseURL)
	}
	fmt.Fprintln(w, "  4. tape status")

	if cfg.LLM.Model == "" {
		fmt.Fprintf(w, "\n%s has no default model: set llm.model in %s (see %s)\n", preset.Name, cfgFile, preset.Docs)
	}
	fmt.Fprintf(w, "\n%s\n", style.dim(fmt.Sprintf(
		"tape's ledger starts at %s regardless of Alpaca's paper balance.", money(cfg.Account.StartingEquity))))
}
