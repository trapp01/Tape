package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MarkUnfilled records that a taken idea never traded a share. Its order has to
// be terminal and empty: a working order may still fill, and a partial one did.
func (s *Store) MarkUnfilled(ctx context.Context, id int64, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	const reason = "the order was cancelled or expired without filling a share"
	update := `UPDATE proposals SET status = ?, reason = ?, decided_at = ?
		WHERE id = ? AND status = ? AND ` + unfilledOrderClause

	args := append([]any{ProposalUnfilled, reason, formatTime(at), id, ProposalTaken}, terminalArgs()...)
	res, err := s.db.ExecContext(ctx, update, args...)
	if err != nil {
		return fmt.Errorf("journal: mark proposal %d unfilled: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: mark proposal %d unfilled: counting affected rows: %w", id, err)
	}
	if n == 0 {
		return s.unfilledRefusal(ctx, id)
	}
	return nil
}

// UnfilledForDay marks every one of a session's dead takes and returns them. It
// is what eod runs once the venue has been cancelled and flattened.
func (s *Store) UnfilledForDay(ctx context.Context, mode, day string, at time.Time) ([]int64, error) {
	if err := validateDay(day); err != nil {
		return nil, fmt.Errorf("journal: unfilled proposals: %w", err)
	}
	query := `SELECT id FROM proposals WHERE mode = ? AND day = ? AND status = ? AND ` + unfilledOrderClause

	args := append([]any{mode, day, ProposalTaken}, terminalArgs()...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: unfilled proposals for %s %s: %w", mode, day, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("journal: unfilled proposals for %s %s: %w", mode, day, err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: unfilled proposals for %s %s: %w", mode, day, err)
	}

	for _, id := range ids {
		if err := s.MarkUnfilled(ctx, id, at); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// unfilledOrderClause is true when the proposal's order can take no more fills
// and took none.
const unfilledOrderClause = `EXISTS (SELECT 1 FROM orders o
	WHERE o.id = proposals.order_id AND o.filled_qty = 0 AND o.status IN (?, ?, ?, ?))`

func terminalArgs() []any {
	args := make([]any, 0, len(terminalStatuses))
	for _, st := range terminalStatuses {
		args = append(args, st)
	}
	return args
}

// unfilledRefusal names why the row did not move: the wrong status, no order, or
// an order that is still working or already traded.
func (s *Store) unfilledRefusal(ctx context.Context, id int64) error {
	status, err := s.proposalStatus(ctx, id)
	if err != nil {
		return err
	}
	if status != ProposalTaken {
		return fmt.Errorf("journal: mark proposal %d unfilled: it is %s, and only a %s proposal can go unfilled",
			id, status, ProposalTaken)
	}

	const query = `SELECT o.status, o.filled_qty FROM proposals p
		JOIN orders o ON o.id = p.order_id WHERE p.id = ?`
	var (
		orderStatus string
		filledQty   int
	)
	err = s.db.QueryRowContext(ctx, query, id).Scan(&orderStatus, &filledQty)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("journal: mark proposal %d unfilled: it is %s but links to no order", id, ProposalTaken)
	}
	if err != nil {
		return fmt.Errorf("journal: mark proposal %d unfilled: %w", id, err)
	}
	if filledQty > 0 {
		return fmt.Errorf("journal: mark proposal %d unfilled: its order filled %d share(s)", id, filledQty)
	}
	return fmt.Errorf("journal: mark proposal %d unfilled: its order is %q, which is not terminal (%s), so it may still fill",
		id, orderStatus, strings.Join(terminalStatuses, ", "))
}
