package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/retro"
)

func newRetroApplyCmd() *cobra.Command {
	var (
		chosen string
		all    bool
	)

	cmd := &cobra.Command{
		Use:   "apply <id>",
		Short: "Write the chosen playbook edits and snapshot the result",
		Long: "apply resolves each chosen edit against the playbook as it stands now, keeps the\n" +
			"file it replaces under playbook.history, and records a version so the gate stops\n" +
			"reading the sessions traded under the old rules.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newRecordApp(cmd, "retro apply "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			r, err := findRetro(cmd.Context(), a, args[0])
			if err != nil {
				return err
			}
			indexes, err := chooseIndexes(cmd.Context(), a, r.ID, chosen, all)
			if err != nil {
				return err
			}
			// A playbook edited by hand since the review is snapshotted first, so
			// the edit that follows is a version of its own rather than of both.
			if err := a.ensureVersion(cmd.Context()); err != nil {
				return err
			}
			deps, err := a.retroDeps(0)
			if err != nil {
				return err
			}
			report, err := retro.Apply(cmd.Context(), deps, r.ID, indexes)
			if err != nil {
				return err
			}
			renderApplied(a, report)
			return nil
		},
	}
	cmd.Flags().StringVar(&chosen, "diff", "", "the diff numbers to apply, e.g. 1,3")
	cmd.Flags().BoolVar(&all, "all", false, "apply every diff the review proposed")
	return cmd
}

// chooseIndexes resolves --diff and --all into the numbers the trader accepted.
// Neither flag means nothing was chosen, and nothing is applied.
func chooseIndexes(ctx context.Context, a *app, retroID int64, chosen string, all bool) ([]int, error) {
	if all && chosen != "" {
		return nil, fmt.Errorf("pick one: --all or --diff")
	}
	if all {
		rows, err := a.jnl.DiffsByRetro(ctx, retroID)
		if err != nil {
			return nil, err
		}
		indexes := make([]int, 0, len(rows))
		for _, r := range rows {
			if r.AppliedAt == nil {
				indexes = append(indexes, r.Index)
			}
		}
		if len(indexes) == 0 {
			return nil, fmt.Errorf("review #%d has no unapplied diffs", retroID)
		}
		return indexes, nil
	}
	if strings.TrimSpace(chosen) == "" {
		return nil, fmt.Errorf("name the edits to apply: --diff 1,3, or --all")
	}
	var indexes []int
	for _, field := range strings.Split(chosen, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%q is not a diff number; they start at 1", strings.TrimSpace(field))
		}
		indexes = append(indexes, n)
	}
	return indexes, nil
}
