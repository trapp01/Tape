package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// claimReason sits on the row while an order is in flight, so a proposal stuck
// here says what it is waiting on.
const claimReason = "submitting to the venue"

// ClaimProposal takes the idea out of circulation while its order is in flight.
// A crash after the venue accepts and before the decision lands leaves the claim
// standing, which is what stops the next `take` from sending a second order.
func (s *Store) ClaimProposal(ctx context.Context, id int64, at time.Time) error {
	if id <= 0 {
		return fmt.Errorf("journal: claim proposal: id must be positive, got %d", id)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	const update = `UPDATE proposals SET status = ?, reason = ?, decided_at = ?
		WHERE id = ? AND status = ?`

	res, err := s.db.ExecContext(ctx, update, ProposalSubmitting, claimReason, formatTime(at), id, ProposalProposed)
	if err != nil {
		return fmt.Errorf("journal: claim proposal %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: claim proposal %d: counting affected rows: %w", id, err)
	}
	if n == 0 {
		return s.claimRefusal(ctx, id)
	}
	return nil
}

// claimRefusal names why the claim matched nothing: no such proposal, one that
// already carries a decision, or one another run is already submitting.
func (s *Store) claimRefusal(ctx context.Context, id int64) error {
	status, err := s.proposalStatus(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("journal: claim proposal %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if status == ProposalSubmitting {
		return fmt.Errorf("journal: claim proposal %d: an order for it may already be live at the venue; "+
			"reconcile before taking it again", id)
	}
	return fmt.Errorf("journal: claim proposal %d: it is already %s; a proposal is decided once", id, status)
}

// ReleaseProposal puts a claimed idea back on the slate. A situational refusal —
// a cap, the clock — decided nothing, so the idea is takeable again later.
func (s *Store) ReleaseProposal(ctx context.Context, id int64) error {
	const update = `UPDATE proposals SET status = ?, reason = '', decided_at = NULL
		WHERE id = ? AND status = ?`

	res, err := s.db.ExecContext(ctx, update, ProposalProposed, id, ProposalSubmitting)
	if err != nil {
		return fmt.Errorf("journal: release proposal %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: release proposal %d: counting affected rows: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("journal: release proposal %d: it holds no claim to release", id)
	}
	return nil
}

// ReconcileSubmittedProposals closes the crash window: a claim with an order at
// the venue was taken, whatever happened to the process that sent it. It returns
// the proposals it decided.
func (s *Store) ReconcileSubmittedProposals(ctx context.Context, mode string, at time.Time) ([]int64, error) {
	claims, err := s.submittedClaims(ctx, mode)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(claims))
	for _, c := range claims {
		// The order is what actually reached the venue, so its quantity is what
		// the trade risked, whatever the slate had sized.
		risk := float64(c.qty) * (c.entry - c.stop)
		if err := s.DecideTaken(ctx, c.proposalID, c.orderID, c.qty, risk, at); err != nil {
			return out, err
		}
		out = append(out, c.proposalID)
	}
	return out, nil
}

// claim is one proposal left submitting with an order against it, and the first
// such order.
type claim struct {
	proposalID, orderID int64
	qty                 int
	entry, stop         float64
}

func (s *Store) submittedClaims(ctx context.Context, mode string) ([]claim, error) {
	const query = `SELECT p.id, o.id, o.qty, p.entry, p.stop FROM proposals p
		JOIN orders o ON o.proposal_id = p.id
		WHERE p.mode = ? AND p.status = ?
		ORDER BY p.id, o.id`

	rows, err := s.db.QueryContext(ctx, query, mode, ProposalSubmitting)
	if err != nil {
		return nil, fmt.Errorf("journal: reconciling submitted proposals for %s: %w", mode, err)
	}
	defer rows.Close()

	var out []claim
	for rows.Next() {
		var c claim
		if err := rows.Scan(&c.proposalID, &c.orderID, &c.qty, &c.entry, &c.stop); err != nil {
			return nil, fmt.Errorf("journal: reconciling submitted proposals for %s: %w", mode, err)
		}
		if len(out) > 0 && out[len(out)-1].proposalID == c.proposalID {
			continue
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: reconciling submitted proposals for %s: %w", mode, err)
	}
	return out, nil
}

func (s *Store) proposalStatus(ctx context.Context, id int64) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM proposals WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("journal: proposal %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("journal: proposal %d: %w", id, err)
	}
	return status, nil
}
