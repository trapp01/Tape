package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

const fillColumns = `f.id, f.order_id, f.broker_fill_id, f.symbol, f.side, f.qty,
	f.raw_price, f.modeled_price, f.commission, f.fees, f.filled_at`

// InsertFill writes f and fills in its ID. It is idempotent: replaying a fill the
// journal already holds leaves the row alone, sets f.ID to the stored row, and
// returns nil, so a reconnecting fill poller cannot double-count an execution.
func (s *Store) InsertFill(ctx context.Context, f *Fill) error {
	if f == nil {
		return errors.New("journal: insert fill: nil fill")
	}
	if err := validateFill(f); err != nil {
		return err
	}
	if f.FilledAt.IsZero() {
		f.FilledAt = time.Now().UTC()
	}
	filledAt := formatTime(f.FilledAt)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: insert fill for order %d: %w", f.OrderID, err)
	}
	defer tx.Rollback()

	existing, err := findDuplicateFill(ctx, tx, f, filledAt)
	if err != nil {
		return fmt.Errorf("journal: insert fill for order %d: checking for a duplicate: %w", f.OrderID, err)
	}
	if existing != 0 {
		f.ID = existing
		return nil
	}

	const insert = `INSERT INTO fills (
		order_id, broker_fill_id, symbol, side, qty,
		raw_price, modeled_price, commission, fees, filled_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, insert,
		f.OrderID, f.BrokerFillID, f.Symbol, f.Side, f.Qty,
		f.RawPrice, f.ModeledPrice, f.Commission, f.Fees, filledAt)
	if err != nil {
		return fmt.Errorf("journal: insert fill for order %d: %w", f.OrderID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert fill for order %d: reading id: %w", f.OrderID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: insert fill for order %d: %w", f.OrderID, err)
	}
	f.ID = id
	return nil
}

// findDuplicateFill returns the id of a fill already recording this execution, or
// zero. Venues that report no fill id fall back to the execution's own shape.
// The same venue fill id under a different order is a collision, not a replay.
func findDuplicateFill(ctx context.Context, tx *sql.Tx, f *Fill, filledAt string) (int64, error) {
	var (
		id      int64
		orderID int64
		err     error
	)
	if f.BrokerFillID != "" {
		const byBrokerID = `SELECT id, order_id FROM fills WHERE broker_fill_id = ?`
		err = tx.QueryRowContext(ctx, byBrokerID, f.BrokerFillID).Scan(&id, &orderID)
	} else {
		const byShape = `SELECT id, order_id FROM fills
			WHERE broker_fill_id = '' AND order_id = ? AND filled_at = ? AND qty = ? AND raw_price = ?`
		err = tx.QueryRowContext(ctx, byShape, f.OrderID, filledAt, f.Qty, f.RawPrice).Scan(&id, &orderID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if orderID != f.OrderID {
		return 0, fmt.Errorf("venue fill id %q is already recorded against order %d", f.BrokerFillID, orderID)
	}
	return id, nil
}

func validateFill(f *Fill) error {
	if f.OrderID <= 0 {
		return errors.New("journal: insert fill: order id is not set")
	}
	if f.Symbol == "" {
		return fmt.Errorf("journal: insert fill for order %d: symbol is empty", f.OrderID)
	}
	if f.Qty <= 0 {
		return fmt.Errorf("journal: insert fill for order %d: qty must be positive, got %d", f.OrderID, f.Qty)
	}
	if f.Side != string(broker.Buy) && f.Side != string(broker.Sell) {
		return fmt.Errorf("journal: insert fill for order %d: side must be %q or %q, got %q", f.OrderID, broker.Buy, broker.Sell, f.Side)
	}
	return nil
}

// Fills returns executions in [from, to), oldest first. A zero from or to drops
// that bound; an empty mode covers both paper and live.
func (s *Store) Fills(ctx context.Context, from, to time.Time, mode string) ([]Fill, error) {
	query := `SELECT ` + fillColumns + ` FROM fills f JOIN orders o ON o.id = f.order_id`
	var where []string
	var args []any

	if !from.IsZero() {
		where = append(where, "f.filled_at >= ?")
		args = append(args, formatTime(from))
	}
	if !to.IsZero() {
		where = append(where, "f.filled_at < ?")
		args = append(args, formatTime(to))
	}
	if mode != "" {
		where = append(where, "o.mode = ?")
		args = append(args, mode)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY f.filled_at, f.id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: list fills: %w", err)
	}
	defer rows.Close()

	var out []Fill
	for rows.Next() {
		f, err := scanFill(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: list fills: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: list fills: %w", err)
	}
	return out, nil
}

func scanFill(sc scanner) (Fill, error) {
	var (
		f        Fill
		filledAt string
	)
	err := sc.Scan(&f.ID, &f.OrderID, &f.BrokerFillID, &f.Symbol, &f.Side, &f.Qty,
		&f.RawPrice, &f.ModeledPrice, &f.Commission, &f.Fees, &filledAt)
	if err != nil {
		return Fill{}, err
	}
	if f.FilledAt, err = parseTime(filledAt); err != nil {
		return Fill{}, err
	}
	return f, nil
}
