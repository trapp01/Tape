package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
)

// liveLocked is the only answer `tape mode live` gets. No flag, env var, or
// config value bypasses it.
const liveLocked = "live mode is locked until the real-money gate opens (see docs/DESIGN.md); tape is paper-only for now"

func newModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [paper|live]",
		Short: "Show or set the trading mode",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, style := bareStyler(cmd)

			want := ""
			if len(args) == 1 {
				want = strings.ToLower(strings.TrimSpace(args[0]))
			}
			if want == config.ModeLive {
				style.banner(out, config.ModePaper, "mode")
				return errors.New(liveLocked)
			}

			path, err := resolveConfigPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			style.banner(out, cfg.Mode, "mode")

			switch want {
			case "":
				fmt.Fprintf(out, "\nmode    %s\nconfig  %s\n", cfg.Mode, path)
				return nil
			case config.ModePaper:
				cfg.Mode = config.ModePaper
				if err := config.Write(path, withoutEnvSecrets(cfg)); err != nil {
					return err
				}
				fmt.Fprintf(out, "\nmode set to paper in %s\n", path)
				return nil
			default:
				return fmt.Errorf("mode must be %q or %q, got %q", config.ModePaper, config.ModeLive, args[0])
			}
		},
	}
}
