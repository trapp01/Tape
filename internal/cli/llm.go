package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/llm"
)

func newLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Inspect and test the configured model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newLLMPingCmd(), newLLMProvidersCmd())
	return cmd
}

func newLLMPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check the configured provider answers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			style.banner(out, cfg.Mode, "llm ping")

			provider, err := llm.New(llm.Config{
				Provider: cfg.LLM.Provider,
				Model:    cfg.LLM.Model,
				BaseURL:  cfg.LLM.BaseURL,
				APIKey:   cfg.LLM.APIKey,
			})
			if err != nil {
				return err
			}
			res, err := llm.Ping(cmd.Context(), provider)
			if err != nil {
				return err
			}

			tw := table(out)
			fmt.Fprintln(out)
			pair(tw, "provider", provider.Name())
			pair(tw, "model", res.Model)
			pair(tw, "latency", res.Latency.Round(1e6).String())
			pair(tw, "reply", res.Reply)
			return tw.Flush()
		},
	}
}

func newLLMProvidersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "List the known model providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)
			style.banner(out, bannerMode(), "llm providers")
			fmt.Fprintln(out)

			tw := table(out)
			row(tw, "NAME", "BASE URL", "KEY ENV", "DEFAULT MODEL", "DOCS")
			for _, p := range llm.Presets() {
				row(tw, p.Name, dash(p.BaseURL), dash(p.KeyEnv), dash(p.DefaultModel), p.Docs)
			}
			return tw.Flush()
		},
	}
}

// dash keeps an empty cell visible in a table.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
