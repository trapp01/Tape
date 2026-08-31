package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/calendar/finnhub"
	"github.com/trapp01/tape/internal/calendar/fomc"
	"github.com/trapp01/tape/internal/calendar/fred"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/llm"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/market/alpacadata"
	"github.com/trapp01/tape/internal/playbook"
)

// The briefing's sources are package-level so tests inject fakes, the same way
// newBroker and openJournal work for the venue and the journal.
var (
	newMarketFeed = func(cfg config.Config) (marketFeed, error) {
		return alpacadata.New(alpacadata.Options{
			APIKey:    cfg.Broker.Alpaca.APIKey,
			APISecret: cfg.Broker.Alpaca.APISecret,
			DataFeed:  cfg.Broker.Alpaca.DataFeed,
		})
	}

	// newCalendarSources turns an unkeyed provider into a warning. A missing
	// calendar is a gap in the briefing, not a failed morning.
	newCalendarSources = func(cfg config.Config) (calendar.Sources, []string) {
		sources := calendar.Sources{Economic: []calendar.EconomicProvider{fomc.New()}}
		var warnings []string
		if p, err := fred.New(cfg.Data.FREDAPIKey); err != nil {
			warnings = append(warnings, calendar.Warning("FRED", err))
		} else {
			sources.Economic = append(sources.Economic, p)
		}
		if p, err := finnhub.New(cfg.Data.FinnhubAPIKey); err != nil {
			warnings = append(warnings, calendar.Warning("Finnhub", err))
		} else {
			sources.Earnings = append(sources.Earnings, p)
		}
		return sources, warnings
	}

	newLLMProvider = func(cfg config.Config) (llm.Provider, error) {
		return llm.New(llm.Config{
			Provider: cfg.LLM.Provider,
			Model:    cfg.LLM.Model,
			BaseURL:  cfg.LLM.BaseURL,
			APIKey:   cfg.LLM.APIKey,
		})
	}
)

// marketFeed is the briefing's read-only view of the market. One data client
// satisfies all four contracts; tests swap in a fake.
type marketFeed interface {
	market.SnapshotProvider
	market.BarsProvider
	market.SessionProvider
	market.IntradayProvider
	market.MoversProvider
	market.NewsProvider
}

// loadPlaybook reads the strategy file and names the command that writes one.
func loadPlaybook(cfg config.Config) (string, error) {
	text, err := playbook.Load(cfg.PlaybookPath())
	if errors.Is(err, playbook.ErrMissing) {
		return "", fmt.Errorf("no playbook at %s: run `tape init` or `tape playbook --write`", cfg.PlaybookPath())
	}
	return text, err
}

func newBriefCmd() *cobra.Command {
	var dryRun, asJSON, force bool

	cmd := &cobra.Command{
		Use:   "brief",
		Short: "Read the market, apply the playbook, make one falsifiable call",
		Long: "brief is the morning ritual. It reads quotes, news, the calendars, and the regime,\n" +
			"hands them to the model with the playbook, and archives the reply with the one call\n" +
			"that gets graded after the close. A second run the same day reprints the archive.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "brief")
			if err != nil {
				return err
			}
			defer a.Close()

			deps, err := a.briefDeps(force)
			if err != nil {
				return err
			}
			if dryRun {
				return dryRunBrief(cmd.Context(), a, deps)
			}
			if err := a.ensureVersion(cmd.Context()); err != nil {
				return err
			}

			provider, err := newLLMProvider(a.cfg)
			if err != nil {
				return err
			}
			res, err := brief.Run(cmd.Context(), deps, provider)
			if err != nil {
				return err
			}
			if asJSON {
				return printBriefJSON(a, res)
			}
			renderBriefing(a, res, true)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "assemble and print the prompts without asking the model or archiving anything")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the input, the output, and the briefing id as JSON")
	cmd.Flags().BoolVar(&force, "force", false, "archive a new briefing even if today already has one; the day's first call stands")
	return cmd
}

// briefDeps wires the briefing's sources. The venue supplies the clock, the
// journal supplies the cash, and neither knows what the other is for.
func (a *app) briefDeps(force bool) (brief.Deps, error) {
	feed, err := newMarketFeed(a.cfg)
	if err != nil {
		return brief.Deps{}, err
	}
	text, err := loadPlaybook(a.cfg)
	if err != nil {
		return brief.Deps{}, err
	}
	sources, warnings := newCalendarSources(a.cfg)

	return brief.Deps{
		Snapshots:        feed,
		Bars:             feed,
		Movers:           feed,
		News:             feed,
		Calendar:         sources,
		CalendarWarnings: warnings,
		Clock:            a.broker.Clock,
		Ledger: func(ctx context.Context) (journal.Ledger, error) {
			return a.jnl.Ledger(ctx, a.cfg.Mode)
		},
		Equity:   a.engine.Equity,
		Cash:     a.engine.FreeCash,
		Limits:   riskLimits(a.cfg),
		Playbook: text,
		Journal:  a.jnl,
		Mode:     a.cfg.Mode,
		Loc:      a.loc,
		Now:      timeNow,
		Cfg:      a.cfg.Brief,
		Force:    force,
	}, nil
}

// dryRunBrief shows exactly what the model would be sent and stops there.
func dryRunBrief(ctx context.Context, a *app, deps brief.Deps) error {
	in, err := brief.Assemble(ctx, deps)
	if err != nil {
		return err
	}
	renderBriefing(a, brief.Result{Input: in}, false)

	system, user := brief.BuildPrompt(in)
	fmt.Fprintf(a.out, "\n--- system prompt (%d chars) ---\n%s\n", len(system), system)
	fmt.Fprintf(a.out, "\n--- user prompt (%d chars) ---\n%s\n", len(user), user)
	fmt.Fprintln(a.out, a.style.dim("dry run: nothing was asked, nothing was archived."))
	return nil
}

func printBriefJSON(a *app, res brief.Result) error {
	payload := struct {
		Input      brief.Input  `json:"input"`
		Output     brief.Output `json:"output"`
		BriefingID int64        `json:"briefing_id"`
	}{res.Input, res.Output, res.Briefing.ID}

	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
