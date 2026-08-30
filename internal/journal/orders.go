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

// ListFilter narrows ListOrders. A zero value returns every order.
type ListFilter struct {
	// Since limits results to orders submitted at or after this time.
	Since time.Time
	// Mode is "paper" or "live"; empty means both.
	Mode string
	// OpenOnly drops orders whose status can take no further fills.
	OpenOnly bool
	// Limit caps the row count; zero or negative means no cap.
	Limit int
}

const orderColumns = `id, broker_order_id, client_order_id, symbol, side, qty, type,
	limit_price, stop_loss, take_profit, status, filled_qty, filled_avg_price,
	source, mode, note, submitted_at, updated_at`

// terminalStatuses can take no further fills, so OpenOnly excludes them.
var terminalStatuses = []string{
	string(broker.StatusFilled),
	string(broker.StatusCanceled),
	string(broker.StatusRejected),
	string(broker.StatusExpired),
}

// InsertOrder writes o and fills in its ID. SubmittedAt and UpdatedAt default to
// now when zero.
func (s *Store) InsertOrder(ctx context.Context, o *Order) error {
	if o == nil {
		return errors.New("journal: insert order: nil order")
	}
	if err := validateOrder(o); err != nil {
		return err
	}
	if o.SubmittedAt.IsZero() {
		o.SubmittedAt = time.Now().UTC()
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = o.SubmittedAt
	}

	const insert = `INSERT INTO orders (
		broker_order_id, client_order_id, symbol, side, qty, type,
		limit_price, stop_loss, take_profit, status, filled_qty, filled_avg_price,
		source, mode, note, submitted_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, insert,
		o.BrokerOrderID, o.ClientOrderID, o.Symbol, o.Side, o.Qty, o.Type,
		nullFloat(o.LimitPrice), nullFloat(o.StopLoss), nullFloat(o.TakeProfit),
		o.Status, o.FilledQty, nullFloat(o.FilledAvgPrice),
		o.Source, o.Mode, o.Note, formatTime(o.SubmittedAt), formatTime(o.UpdatedAt))
	if err != nil {
		return fmt.Errorf("journal: insert order %s %d %s: %w", o.Side, o.Qty, o.Symbol, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert order %s %d %s: reading id: %w", o.Side, o.Qty, o.Symbol, err)
	}
	o.ID = id
	return nil
}

func validateOrder(o *Order) error {
	if o.Symbol == "" {
		return errors.New("journal: insert order: symbol is empty")
	}
	if o.Qty <= 0 {
		return fmt.Errorf("journal: insert order %s: qty must be positive, got %d", o.Symbol, o.Qty)
	}
	if o.Side != string(broker.Buy) && o.Side != string(broker.Sell) {
		return fmt.Errorf("journal: insert order %s: side must be %q or %q, got %q", o.Symbol, broker.Buy, broker.Sell, o.Side)
	}
	if o.Mode != ModePaper && o.Mode != ModeLive {
		return fmt.Errorf("journal: insert order %s: mode must be %q or %q, got %q", o.Symbol, ModePaper, ModeLive, o.Mode)
	}
	return nil
}

// UpdateOrder records the venue's latest view of an order. filledAvg may be nil
// while nothing has filled yet.
func (s *Store) UpdateOrder(ctx context.Context, brokerOrderID string, status string, filledQty int, filledAvg *float64) error {
	if brokerOrderID == "" {
		return errors.New("journal: update order: broker order id is empty")
	}
	const update = `UPDATE orders
		SET status = ?, filled_qty = ?, filled_avg_price = ?, updated_at = ?
		WHERE broker_order_id = ?`

	res, err := s.db.ExecContext(ctx, update, status, filledQty, nullFloat(filledAvg), formatTime(time.Now()), brokerOrderID)
	if err != nil {
		return fmt.Errorf("journal: update order %s: %w", brokerOrderID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: update order %s: counting affected rows: %w", brokerOrderID, err)
	}
	if n == 0 {
		return fmt.Errorf("journal: update order %s: %w", brokerOrderID, ErrNotFound)
	}
	return nil
}

// OrderByBrokerID returns the order with the given venue id, or ErrNotFound.
func (s *Store) OrderByBrokerID(ctx context.Context, id string) (Order, error) {
	return s.orderBy(ctx, "broker_order_id", id)
}

// OrderByClientID returns the order with the given client id, or ErrNotFound.
func (s *Store) OrderByClientID(ctx context.Context, id string) (Order, error) {
	return s.orderBy(ctx, "client_order_id", id)
}

func (s *Store) orderBy(ctx context.Context, column, id string) (Order, error) {
	if id == "" {
		return Order{}, fmt.Errorf("journal: order by %s: id is empty", column)
	}
	query := `SELECT ` + orderColumns + ` FROM orders WHERE ` + column + ` = ?`
	o, err := scanOrder(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, fmt.Errorf("journal: order %s %s: %w", column, id, ErrNotFound)
	}
	if err != nil {
		return Order{}, fmt.Errorf("journal: order %s %s: %w", column, id, err)
	}
	return o, nil
}

// ListOrders returns matching orders, newest submission first.
func (s *Store) ListOrders(ctx context.Context, f ListFilter) ([]Order, error) {
	query := `SELECT ` + orderColumns + ` FROM orders`
	var where []string
	var args []any

	if !f.Since.IsZero() {
		where = append(where, "submitted_at >= ?")
		args = append(args, formatTime(f.Since))
	}
	if f.Mode != "" {
		where = append(where, "mode = ?")
		args = append(args, f.Mode)
	}
	if f.OpenOnly {
		where = append(where, "status NOT IN (?, ?, ?, ?)")
		for _, st := range terminalStatuses {
			args = append(args, st)
		}
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY submitted_at DESC, id DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: list orders: %w", err)
	}
	defer rows.Close()

	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: list orders: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: list orders: %w", err)
	}
	return out, nil
}

func scanOrder(sc scanner) (Order, error) {
	var (
		o           Order
		limitPrice  sql.NullFloat64
		stopLoss    sql.NullFloat64
		takeProfit  sql.NullFloat64
		filledAvg   sql.NullFloat64
		submittedAt string
		updatedAt   string
	)
	err := sc.Scan(&o.ID, &o.BrokerOrderID, &o.ClientOrderID, &o.Symbol, &o.Side, &o.Qty, &o.Type,
		&limitPrice, &stopLoss, &takeProfit, &o.Status, &o.FilledQty, &filledAvg,
		&o.Source, &o.Mode, &o.Note, &submittedAt, &updatedAt)
	if err != nil {
		return Order{}, err
	}
	o.LimitPrice = floatPtr(limitPrice)
	o.StopLoss = floatPtr(stopLoss)
	o.TakeProfit = floatPtr(takeProfit)
	o.FilledAvgPrice = floatPtr(filledAvg)
	if o.SubmittedAt, err = parseTime(submittedAt); err != nil {
		return Order{}, err
	}
	if o.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Order{}, err
	}
	return o, nil
}
