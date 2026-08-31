package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrAlreadyScored is returned when a proposal already carries a replayed
// outcome. An idea is graded once, so a second replay cannot overwrite the first.
var ErrAlreadyScored = errors.New("proposal already scored")

const outcomeColumns = `id, proposal_id, mode, day, filled, fill_price, filled_at,
	exit_kind, exit_price, exit_at, qty, gross_pl, costs, net_pl, r_multiple,
	ambiguous, scored_at`

// exitKinds are the ways a replay can end. An unfilled entry ends as ExitNone.
var exitKinds = []string{ExitStop, ExitTarget, ExitClose, ExitNone}

// InsertProposalOutcome records what a proposal would have done at its own levels
// and fills in the ID. ScoredAt defaults to now when zero.
func (s *Store) InsertProposalOutcome(ctx context.Context, o *ProposalOutcome) error {
	if o == nil {
		return errors.New("journal: insert outcome: nil outcome")
	}
	if err := validateOutcome(o); err != nil {
		return err
	}
	if o.ScoredAt.IsZero() {
		o.ScoredAt = time.Now().UTC()
	}

	const insert = `INSERT INTO proposal_outcomes (
		proposal_id, mode, day, filled, fill_price, filled_at,
		exit_kind, exit_price, exit_at, qty, gross_pl, costs, net_pl, r_multiple,
		ambiguous, scored_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, insert,
		o.ProposalID, o.Mode, o.Day, boolToInt(o.Filled), nullFloat(o.FillPrice), nullTime(o.FilledAt),
		o.ExitKind, nullFloat(o.ExitPrice), nullTime(o.ExitAt), o.Qty, o.GrossPL, o.Costs, o.NetPL,
		o.RMultiple, boolToInt(o.Ambiguous), formatTime(o.ScoredAt))
	if err != nil {
		return s.outcomeInsertError(ctx, o.ProposalID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert outcome for proposal %d: reading id: %w", o.ProposalID, err)
	}
	o.ID = id
	return nil
}

// outcomeInsertError names a rejected insert: a proposal already replayed, or
// something else the database refused.
func (s *Store) outcomeInsertError(ctx context.Context, proposalID int64, cause error) error {
	var scoredAt string
	const query = `SELECT scored_at FROM proposal_outcomes WHERE proposal_id = ?`
	if err := s.db.QueryRowContext(ctx, query, proposalID).Scan(&scoredAt); err == nil {
		return fmt.Errorf("journal: insert outcome for proposal %d: already scored at %s: %w",
			proposalID, scoredAt, ErrAlreadyScored)
	}
	return fmt.Errorf("journal: insert outcome for proposal %d: %w", proposalID, cause)
}

func validateOutcome(o *ProposalOutcome) error {
	if o.ProposalID <= 0 {
		return errors.New("journal: insert outcome: proposal id is not set")
	}
	if o.Mode != ModePaper && o.Mode != ModeLive {
		return fmt.Errorf("journal: insert outcome for proposal %d: mode must be %q or %q, got %q",
			o.ProposalID, ModePaper, ModeLive, o.Mode)
	}
	if err := validateDay(o.Day); err != nil {
		return fmt.Errorf("journal: insert outcome for proposal %d: %w", o.ProposalID, err)
	}
	if o.ExitKind == "" {
		o.ExitKind = ExitNone
	}
	if !slices.Contains(exitKinds, o.ExitKind) {
		return fmt.Errorf("journal: insert outcome for proposal %d: exit kind %q is not one of %s",
			o.ProposalID, o.ExitKind, strings.Join(exitKinds, ", "))
	}
	if o.Qty < 0 {
		return fmt.Errorf("journal: insert outcome for proposal %d: qty must not be negative, got %d", o.ProposalID, o.Qty)
	}
	return nil
}

// OutcomeByProposal returns the replay of one idea, or ErrNotFound.
func (s *Store) OutcomeByProposal(ctx context.Context, proposalID int64) (ProposalOutcome, error) {
	query := `SELECT ` + outcomeColumns + ` FROM proposal_outcomes WHERE proposal_id = ?`
	o, err := scanOutcome(s.db.QueryRowContext(ctx, query, proposalID))
	if errors.Is(err, sql.ErrNoRows) {
		return ProposalOutcome{}, fmt.Errorf("journal: outcome for proposal %d: %w", proposalID, ErrNotFound)
	}
	if err != nil {
		return ProposalOutcome{}, fmt.Errorf("journal: outcome for proposal %d: %w", proposalID, err)
	}
	return o, nil
}

// OutcomesInRange returns replayed outcomes in [fromDay, toDay], oldest first.
// An empty mode covers both paper and live.
func (s *Store) OutcomesInRange(ctx context.Context, mode, fromDay, toDay string) ([]ProposalOutcome, error) {
	what := fmt.Sprintf("outcomes for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+outcomeColumns+` FROM proposal_outcomes`, mode, fromDay, toDay, `day, id`)
	return queryList(ctx, s, what, query, args, scanOutcome)
}

// scorableStatuses are the decided states a replay grades. An idea still on the
// slate, or claimed mid-submission, has not finished happening yet.
var scorableStatuses = []string{ProposalTaken, ProposalPassed, ProposalRejected, ProposalExpired, ProposalUnfilled}

// UnscoredProposals returns decided ideas from on or before throughDay that no
// replay has graded yet, oldest first. An empty mode covers both.
func (s *Store) UnscoredProposals(ctx context.Context, mode, throughDay string) ([]Proposal, error) {
	if err := validateDay(throughDay); err != nil {
		return nil, fmt.Errorf("journal: unscored proposals: %w", err)
	}
	query := `SELECT ` + proposalColumns + ` FROM proposals
		WHERE day <= ?
			AND status IN (?, ?, ?, ?, ?)
			AND id NOT IN (SELECT proposal_id FROM proposal_outcomes)`
	args := []any{throughDay}
	for _, st := range scorableStatuses {
		args = append(args, st)
	}
	if mode != "" {
		query += ` AND mode = ?`
		args = append(args, mode)
	}
	query += ` ORDER BY day, id`

	ps, err := s.listProposals(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: unscored proposals for mode %q: %w", mode, err)
	}
	return ps, nil
}

func scanOutcome(sc scanner) (ProposalOutcome, error) {
	var (
		o         ProposalOutcome
		filled    int64
		fillPrice sql.NullFloat64
		filledAt  sql.NullString
		exitPrice sql.NullFloat64
		exitAt    sql.NullString
		ambiguous int64
		scoredAt  string
	)
	err := sc.Scan(&o.ID, &o.ProposalID, &o.Mode, &o.Day, &filled, &fillPrice, &filledAt,
		&o.ExitKind, &exitPrice, &exitAt, &o.Qty, &o.GrossPL, &o.Costs, &o.NetPL, &o.RMultiple,
		&ambiguous, &scoredAt)
	if err != nil {
		return ProposalOutcome{}, err
	}
	o.Filled = filled == 1
	o.Ambiguous = ambiguous == 1
	o.FillPrice = floatPtr(fillPrice)
	o.ExitPrice = floatPtr(exitPrice)
	if o.FilledAt, err = timePtr(filledAt); err != nil {
		return ProposalOutcome{}, err
	}
	if o.ExitAt, err = timePtr(exitAt); err != nil {
		return ProposalOutcome{}, err
	}
	if o.ScoredAt, err = parseTime(scoredAt); err != nil {
		return ProposalOutcome{}, err
	}
	return o, nil
}
