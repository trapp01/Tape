package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/retro"
	"github.com/trapp01/tape/internal/stats"
)

const (
	// defaultStatsDays is the trailing window `tape stats` covers with no flags.
	defaultStatsDays = 30
	// allTimeYears is how far --all reaches back. The gate trims itself to the
	// sessions since the rules last moved.
	allTimeYears = 20
)

func newStatsCmd() *cobra.Command {
	var (
		month, all, asJSON bool
		from, to           string
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "What the record says, and where it stands against the gate",
		Long: "stats computes everything from the journal: cost-modelled trades, replayed\n" +
			"proposals, graded calls and notes, and the real-money gate. No number here\n" +
			"comes from a broker balance, and none of them unlocks anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newRecordApp(cmd, "stats")
			if err != nil {
				return err
			}
			defer a.Close()

			w, err := statsWindow(a, month, all, from, to)
			if err != nil {
				return err
			}
			rep, version, err := a.report(cmd.Context(), w)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(a, rep)
			}
			renderReport(a, rep, version, windowSpan(w, all))
			return nil
		},
	}
	cmd.Flags().BoolVar(&month, "month", false, "cover this calendar month")
	cmd.Flags().BoolVar(&all, "all", false, "cover the whole record")
	cmd.Flags().StringVar(&from, "from", "", "first day to cover (YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "last day to cover (YYYY-MM-DD, default today)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the whole report as JSON")
	return cmd
}

func newGateCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Where the whole record stands against the real-money threshold",
		Long: "gate is the stats table's last section on its own, read over the whole record\n" +
			"since the rules last changed. Nothing in tape opens live mode; this only says\n" +
			"what the numbers would have to be.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newRecordApp(cmd, "gate")
			if err != nil {
				return err
			}
			defer a.Close()

			to := startOfLocalDay(timeNow(), a.loc)
			w := stats.Window{From: to.AddDate(-allTimeYears, 0, 0), To: to, Loc: a.loc, Mode: a.cfg.Mode}
			rep, version, err := a.report(cmd.Context(), w)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(a, rep)
			}
			renderSignificance(a, rep)
			renderGate(a, rep, version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the whole report as JSON")
	return cmd
}

// report snapshots the rules in force before computing, so a playbook or config
// edited between two runs shows up as a gate reset rather than as evidence. The
// version it returns is the one the gate window starts at.
func (a *app) report(ctx context.Context, w stats.Window) (stats.Report, *journal.PlaybookVersion, error) {
	if err := a.ensureVersion(ctx); err != nil {
		return stats.Report{}, nil, err
	}
	rep, err := stats.Compute(ctx, a.jnl, w, gateFrom(a.cfg))
	if err != nil {
		return stats.Report{}, nil, err
	}
	version, err := a.latestVersion(ctx)
	if err != nil {
		return stats.Report{}, nil, err
	}
	return rep, version, nil
}

// latestVersion is the newest playbook snapshot, or nil when none has been taken.
func (a *app) latestVersion(ctx context.Context) (*journal.PlaybookVersion, error) {
	v, err := a.jnl.LatestPlaybookVersion(ctx)
	if errors.Is(err, journal.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ensureVersion records a playbook snapshot whenever the strategy file or the
// rules around it have moved, so the gate reads only sessions traded under the
// numbers in force now.
func (a *app) ensureVersion(ctx context.Context) error {
	_, err := retro.EnsureVersion(ctx, a.jnl, a.cfg, a.cfg.PlaybookPath())
	return err
}

func gateFrom(cfg config.Config) stats.Gate {
	return stats.Gate{
		MinMonths:            cfg.Gate.MinMonths,
		MinSessions:          cfg.Gate.MinSessions,
		MinTrades:            cfg.Gate.MinTrades,
		MinProfitFactor:      cfg.Gate.MinProfitFactor,
		MaxDrawdownPct:       cfg.Gate.MaxDrawdownPct,
		MinExpectancyUSD:     cfg.Gate.MinExpectancyUSD,
		MaxRefusalsLastMonth: cfg.Gate.MaxRefusalsLastMonth,
		MaxNullPassRate:      cfg.Gate.MaxNullPassRate,
	}
}

// statsWindow resolves the flags into the span the report covers, measured in the
// account's own zone. With no flags it is the trailing 30 days.
func statsWindow(a *app, month, all bool, from, to string) (stats.Window, error) {
	if err := oneWindowFlag(month, all, from != "" || to != ""); err != nil {
		return stats.Window{}, err
	}
	now := timeNow().In(a.loc)
	end := startOfLocalDay(now, a.loc)
	start := end.AddDate(0, 0, -(defaultStatsDays - 1))

	switch {
	case all:
		start = end.AddDate(-allTimeYears, 0, 0)
	case month:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, a.loc)
	case from != "" || to != "":
		var err error
		if start, end, err = explicitDays(a, from, to, end); err != nil {
			return stats.Window{}, err
		}
	}
	return stats.Window{From: start, To: end, Loc: a.loc, Mode: a.cfg.Mode}, nil
}

// explicitDays parses --from and --to. --to alone ends the default window early
// rather than opening one with no beginning.
func explicitDays(a *app, from, to string, defaultEnd time.Time) (time.Time, time.Time, error) {
	end := defaultEnd
	if to != "" {
		parsed, err := time.ParseInLocation(dayLayout, to, a.loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--to %q is not a %s date", to, dayLayout)
		}
		end = parsed
	}
	start := end.AddDate(0, 0, -(defaultStatsDays - 1))
	if from != "" {
		parsed, err := time.ParseInLocation(dayLayout, from, a.loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--from %q is not a %s date", from, dayLayout)
		}
		start = parsed
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("--from %s is after --to %s", start.Format(dayLayout), end.Format(dayLayout))
	}
	return start, end, nil
}

// oneWindowFlag refuses two ways of asking for a window at once, because the
// second one would silently win.
func oneWindowFlag(month, all, explicit bool) error {
	n := 0
	for _, set := range []bool{month, all, explicit} {
		if set {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("pick one window: --month, --all, or --from/--to")
	}
	return nil
}

// windowSpan describes the report's window for the header. --all reaches back
// further than any record, so it says what it means rather than printing a date
// twenty years old.
func windowSpan(w stats.Window, all bool) string {
	if all {
		return "the whole record, through " + w.To.Format(dayLayout)
	}
	return w.From.Format(dayLayout) + " → " + w.To.Format(dayLayout)
}

func startOfLocalDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

func printJSON(a *app, v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
