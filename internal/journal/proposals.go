package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const proposalColumns = `id, briefing_id, mode, day, idx, symbol, side, setup_id,
	entry, stop, target, qty, risk_usd, thesis, invalidation, confidence,
	status, reason, decided_at, order_id, taken_qty, taken_risk_usd, created_at`

// InsertProposals writes a briefing's whole slate in one transaction and fills in
// the IDs. Every idea is recorded, including the ones a guardrail will refuse:
// the refusal is a decision on the row, not a reason to skip it.
func (s *Store) InsertProposals(ctx context.Context, ps []*Proposal) error {
	if len(ps) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i, p := range ps {
		if p == nil {
			return fmt.Errorf("journal: insert proposals: proposal %d is nil", i)
		}
		if err := validateProposal(p); err != nil {
			return err
		}
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
	}

	const insert = `INSERT INTO proposals (
		briefing_id, mode, day, idx, symbol, side, setup_id,
		entry, stop, target, qty, risk_usd, thesis, invalidation, confidence,
		status, reason, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: insert proposals for briefing %d: %w", ps[0].BriefingID, err)
	}
	defer tx.Rollback()

	ids := make([]int64, len(ps))
	for i, p := range ps {
		res, err := tx.ExecContext(ctx, insert,
			p.BriefingID, p.Mode, p.Day, p.Index, p.Symbol, p.Side, p.SetupID,
			p.Entry, p.Stop, p.Target, p.Qty, p.RiskUSD, p.Thesis, p.Invalidation, p.Confidence,
			p.Status, p.Reason, formatTime(p.CreatedAt))
		if err != nil {
			return fmt.Errorf("journal: insert proposal %d (%s) for briefing %d: %w", p.Index, p.Symbol, p.BriefingID, err)
		}
		if ids[i], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("journal: insert proposal %d (%s) for briefing %d: reading id: %w", p.Index, p.Symbol, p.BriefingID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: insert proposals for briefing %d: %w", ps[0].BriefingID, err)
	}
	for i, p := range ps {
		p.ID = ids[i]
	}
	return nil
}

// validateProposal checks what the record needs. Prices are not checked: an idea
// with numbers a rule refuses is exactly what a rejected row holds.
func validateProposal(p *Proposal) error {
	if p.BriefingID <= 0 {
		return errors.New("journal: insert proposal: briefing id is not set")
	}
	if p.Mode != ModePaper && p.Mode != ModeLive {
		return fmt.Errorf("journal: insert proposal for briefing %d: mode must be %q or %q, got %q", p.BriefingID, ModePaper, ModeLive, p.Mode)
	}
	if err := validateDay(p.Day); err != nil {
		return fmt.Errorf("journal: insert proposal for briefing %d: %w", p.BriefingID, err)
	}
	if p.Index < 1 {
		return fmt.Errorf("journal: insert proposal for briefing %d: index is 1-based, got %d", p.BriefingID, p.Index)
	}
	if p.Symbol == "" {
		return fmt.Errorf("journal: insert proposal %d for briefing %d: symbol is empty", p.Index, p.BriefingID)
	}
	if p.Status == "" {
		p.Status = ProposalProposed
	}
	if p.Status != ProposalProposed {
		return fmt.Errorf("journal: insert proposal %d (%s): status is %q, but a new proposal starts as %q; record the decision with DecideProposal",
			p.Index, p.Symbol, p.Status, ProposalProposed)
	}
	return nil
}

// ProposalsByBriefing returns one briefing's slate in the order it was presented.
func (s *Store) ProposalsByBriefing(ctx context.Context, briefingID int64) ([]Proposal, error) {
	query := `SELECT ` + proposalColumns + ` FROM proposals WHERE briefing_id = ? ORDER BY idx`
	ps, err := s.listProposals(ctx, query, briefingID)
	if err != nil {
		return nil, fmt.Errorf("journal: proposals for briefing %d: %w", briefingID, err)
	}
	return ps, nil
}

// ProposalsForDay returns every idea filed for a session, by index. A re-run
// briefing files a second slate, so an index can appear twice; the later row is
// the one the trader was shown last.
func (s *Store) ProposalsForDay(ctx context.Context, mode, day string) ([]Proposal, error) {
	if err := validateDay(day); err != nil {
		return nil, fmt.Errorf("journal: proposals for day: %w", err)
	}
	query := `SELECT ` + proposalColumns + ` FROM proposals WHERE mode = ? AND day = ? ORDER BY idx, id`
	ps, err := s.listProposals(ctx, query, mode, day)
	if err != nil {
		return nil, fmt.Errorf("journal: proposals for %s %s: %w", mode, day, err)
	}
	return ps, nil
}

// ProposalByDayIndex resolves the number the trader types, or ErrNotFound.
func (s *Store) ProposalByDayIndex(ctx context.Context, mode, day string, index int) (Proposal, error) {
	if err := validateDay(day); err != nil {
		return Proposal{}, fmt.Errorf("journal: proposal by index: %w", err)
	}
	query := `SELECT ` + proposalColumns + ` FROM proposals
		WHERE mode = ? AND day = ? AND idx = ? ORDER BY id DESC LIMIT 1`
	p, err := scanProposal(s.db.QueryRowContext(ctx, query, mode, day, index))
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, fmt.Errorf("journal: proposal %d for %s %s: %w", index, mode, day, ErrNotFound)
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("journal: proposal %d for %s %s: %w", index, mode, day, err)
	}
	return p, nil
}

func (s *Store) listProposals(ctx context.Context, query string, args ...any) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProposal(sc scanner) (Proposal, error) {
	var (
		p            Proposal
		decidedAt    sql.NullString
		orderID      sql.NullInt64
		takenQty     sql.NullInt64
		takenRiskUSD sql.NullFloat64
		createdAt    string
	)
	err := sc.Scan(&p.ID, &p.BriefingID, &p.Mode, &p.Day, &p.Index, &p.Symbol, &p.Side, &p.SetupID,
		&p.Entry, &p.Stop, &p.Target, &p.Qty, &p.RiskUSD, &p.Thesis, &p.Invalidation, &p.Confidence,
		&p.Status, &p.Reason, &decidedAt, &orderID, &takenQty, &takenRiskUSD, &createdAt)
	if err != nil {
		return Proposal{}, err
	}
	p.OrderID = intPtr(orderID)
	p.TakenRiskUSD = floatPtr(takenRiskUSD)
	if takenQty.Valid {
		n := int(takenQty.Int64)
		p.TakenQty = &n
	}
	if decidedAt.Valid {
		t, err := parseTime(decidedAt.String)
		if err != nil {
			return Proposal{}, err
		}
		p.DecidedAt = &t
	}
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return Proposal{}, err
	}
	return p, nil
}
