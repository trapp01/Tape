package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// decidedStatuses are what a proposal can become; it never returns to proposed.
var decidedStatuses = []string{ProposalTaken, ProposalPassed, ProposalRejected, ProposalExpired}

// DecideProposal records what became of an idea. A proposal is decided once: the
// second attempt names the decision already on the row, so a pass cannot be
// quietly rewritten into a take after the fact.
func (s *Store) DecideProposal(ctx context.Context, id int64, status, reason string, orderID *int64, at time.Time) error {
	return s.decide(ctx, id, status, reason, orderID, nil, nil, at)
}

// DecideTaken is DecideProposal for a take, recording the size and risk actually
// submitted next to the ones the slate proposed. `take --qty` may lower both.
func (s *Store) DecideTaken(ctx context.Context, id, orderID int64, qty int, riskUSD float64, at time.Time) error {
	if qty <= 0 {
		return fmt.Errorf("journal: decide proposal %d as %s: traded qty must be positive, got %d", id, ProposalTaken, qty)
	}
	return s.decide(ctx, id, ProposalTaken, "", &orderID, &qty, &riskUSD, at)
}

func (s *Store) decide(ctx context.Context, id int64, status, reason string, orderID *int64, qty *int, riskUSD *float64, at time.Time) error {
	if id <= 0 {
		return fmt.Errorf("journal: decide proposal: id must be positive, got %d", id)
	}
	switch status {
	case ProposalTaken:
		if orderID == nil {
			return fmt.Errorf("journal: decide proposal %d: %q needs the order it was submitted as", id, status)
		}
	case ProposalPassed, ProposalRejected:
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("journal: decide proposal %d: %q needs a reason; an unexplained decision cannot be scored", id, status)
		}
	// An expiry is the session ending, which needs neither.
	case ProposalExpired:
	default:
		return fmt.Errorf("journal: decide proposal %d: status %q is not one of %s", id, status, strings.Join(decidedStatuses, ", "))
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	// A claim is the open window a take is decided out of, so both undecided
	// states are decidable; anything else already carries an answer.
	const update = `UPDATE proposals
		SET status = ?, reason = ?, order_id = ?, taken_qty = ?, taken_risk_usd = ?, decided_at = ?
		WHERE id = ? AND status IN (?, ?)`

	res, err := s.db.ExecContext(ctx, update, status, reason, nullInt(orderID), nullQty(qty), nullFloat(riskUSD),
		formatTime(at), id, ProposalProposed, ProposalSubmitting)
	if err != nil {
		return fmt.Errorf("journal: decide proposal %d as %s: %w", id, status, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: decide proposal %d as %s: counting affected rows: %w", id, status, err)
	}
	if n == 0 {
		return s.decidedRefusal(ctx, id, status)
	}
	return nil
}

// decidedRefusal names why an update guarded on the proposed status matched
// nothing: no such proposal, or one that already carries a decision.
func (s *Store) decidedRefusal(ctx context.Context, id int64, want string) error {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM proposals WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("journal: decide proposal %d as %s: %w", id, want, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("journal: decide proposal %d as %s: %w", id, want, err)
	}
	return fmt.Errorf("journal: decide proposal %d as %s: it is already %s; a proposal is decided once", id, want, status)
}

// ExpireOpenProposals closes out a session's undecided ideas and returns how many
// it closed. An idea nobody acted on is still part of the record.
func (s *Store) ExpireOpenProposals(ctx context.Context, mode, day string, at time.Time) (int, error) {
	if err := validateDay(day); err != nil {
		return 0, fmt.Errorf("journal: expire proposals: %w", err)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	const update = `UPDATE proposals
		SET status = ?, reason = ?, decided_at = ?
		WHERE mode = ? AND day = ? AND status = ?`
	const reason = "the session ended with no decision"

	res, err := s.db.ExecContext(ctx, update, ProposalExpired, reason, formatTime(at), mode, day, ProposalProposed)
	if err != nil {
		return 0, fmt.Errorf("journal: expire proposals for %s %s: %w", mode, day, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("journal: expire proposals for %s %s: counting affected rows: %w", mode, day, err)
	}
	return int(n), nil
}
