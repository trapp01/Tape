package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/llm"
	"github.com/trapp01/tape/internal/retro"
)

func newRetroCmd() *cobra.Command {
	var (
		weeks          int
		dryRun, asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "retro",
		Short: "Review the scored record and propose playbook edits",
		Long: "retro reads the last weeks of the journal — trades, replays, calls, notes, and\n" +
			"refusals — and asks the model what the numbers show. It proposes exact edits to\n" +
			"the playbook; `tape retro apply` is what actually changes a rule.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newRecordApp(cmd, "retro")
			if err != nil {
				return err
			}
			defer a.Close()

			deps, err := a.retroDeps(weeks)
			if err != nil {
				return err
			}
			if dryRun {
				return dryRunRetro(cmd.Context(), a, deps)
			}
			if err := a.ensureVersion(cmd.Context()); err != nil {
				return err
			}

			provider, err := newRetroProvider(a.cfg)
			if err != nil {
				return err
			}
			res, err := retro.Run(cmd.Context(), deps, provider)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(a, res)
			}
			renderRetro(a, res.Retro, res.Output, res.Diffs)
			return nil
		},
	}
	cmd.Flags().IntVar(&weeks, "weeks", 0, "how many weeks of sessions to review (default: [retro] weeks)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "assemble and print the prompts without asking the model or archiving anything")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the input, the reply, and the diffs as JSON")
	cmd.AddCommand(newRetroShowCmd(), newRetroApplyCmd())
	return cmd
}

func newRetroShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id|latest>",
		Short: "Re-render an archived review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newRecordApp(cmd, "retro show "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			r, err := findRetro(cmd.Context(), a, args[0])
			if err != nil {
				return err
			}
			var out retro.Output
			if err := json.Unmarshal(r.OutputJSON, &out); err != nil {
				return fmt.Errorf("review #%d was archived without a usable reply (the raw text is on the row): %w", r.ID, err)
			}
			diffs, err := a.jnl.DiffsByRetro(cmd.Context(), r.ID)
			if err != nil {
				return err
			}
			renderRetro(a, r, out, diffs)
			return nil
		},
	}
}

// retroDeps wires the review's sources. Weeks of zero takes the configured default.
func (a *app) retroDeps(weeks int) (retro.Deps, error) {
	text, err := loadPlaybook(a.cfg)
	if err != nil {
		return retro.Deps{}, err
	}
	if weeks < 1 {
		weeks = max(a.cfg.Retro.Weeks, 1)
	}
	return retro.Deps{
		Journal:      a.jnl,
		Mode:         a.cfg.Mode,
		Loc:          a.loc,
		Now:          timeNow,
		Weeks:        weeks,
		Gate:         gateFrom(a.cfg),
		Limits:       riskLimits(a.cfg),
		Playbook:     text,
		PlaybookPath: a.cfg.PlaybookPath(),
		Cfg:          a.cfg,
	}, nil
}

// newRetroProvider is the configured model, with [retro] model overriding [llm]
// model: the weekly review is worth a bigger model than the daily briefing.
var newRetroProvider = func(cfg config.Config) (llm.Provider, error) {
	model := cfg.LLM.Model
	if cfg.Retro.Model != "" {
		model = cfg.Retro.Model
	}
	return llm.New(llm.Config{
		Provider: cfg.LLM.Provider,
		Model:    model,
		BaseURL:  cfg.LLM.BaseURL,
		APIKey:   cfg.LLM.APIKey,
	})
}

// dryRunRetro shows exactly what the model would be sent and stops there.
func dryRunRetro(ctx context.Context, a *app, deps retro.Deps) error {
	in, err := retro.Assemble(ctx, deps)
	if err != nil {
		return err
	}
	system, user := retro.BuildPrompt(in)
	fmt.Fprintf(a.out, "\n--- system prompt (%d chars) ---\n%s\n", len(system), system)
	fmt.Fprintf(a.out, "\n--- user prompt (%d chars) ---\n%s\n", len(user), user)
	fmt.Fprintln(a.out, a.style.dim("dry run: nothing was asked, nothing was archived."))
	return nil
}

// findRetro takes a journal id or "latest", which is what a reader means when
// they do not have an id in front of them.
func findRetro(ctx context.Context, a *app, ref string) (journal.Retro, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if strings.EqualFold(ref, "latest") {
		rows, err := a.jnl.ListRetros(ctx, a.cfg.Mode, 1)
		if err != nil {
			return journal.Retro{}, err
		}
		if len(rows) == 0 {
			return journal.Retro{}, fmt.Errorf("no reviews archived yet; run `tape retro`")
		}
		return rows[0], nil
	}
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return journal.Retro{}, fmt.Errorf("%q is not a review id or \"latest\"", ref)
	}
	return a.jnl.RetroByID(ctx, id)
}
