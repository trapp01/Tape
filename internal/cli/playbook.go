package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/playbook"
)

func newPlaybookCmd() *cobra.Command {
	var write bool

	cmd := &cobra.Command{
		Use:   "playbook",
		Short: "Show the strategy file the briefing applies",
		Long: "playbook prints the rules the model cites every morning. tape never edits this file;\n" +
			"strategy changes are yours to make, ideally against the scored record.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			style.banner(out, cfg.Mode, "playbook")
			path := cfg.PlaybookPath()

			if write {
				switch err := playbook.WriteDefault(path); {
				case err == nil:
					fmt.Fprintf(out, "\nwrote the default playbook to %s\n", path)
				case errors.Is(err, os.ErrExist) || fileExists(path):
					fmt.Fprintf(out, "\n%s already exists; it is yours to edit.\n", path)
				default:
					return err
				}
			}

			text, err := loadPlaybook(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%s\n\n%s", path, text)
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "create the default playbook if none exists")
	cmd.AddCommand(newPlaybookVersionsCmd())
	return cmd
}

func newPlaybookVersionsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "versions",
		Short: "List the playbook snapshots the gate reads from",
		Long: "Every applied edit, and every change to the risk, cost, or model config, is a\n" +
			"snapshot. The gate reads only the sessions after the newest one, so a rule fitted\n" +
			"to the record is never graded on the record that produced it.\n" +
			"Listing is a read: the snapshot itself is taken by stats, gate, and retro.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newRecordApp(cmd, "playbook versions")
			if err != nil {
				return err
			}
			defer a.Close()

			versions, err := a.jnl.ListPlaybookVersions(cmd.Context(), limit)
			if err != nil {
				return err
			}
			renderVersions(a, versions)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "how many snapshots to list, newest first")
	return cmd
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
