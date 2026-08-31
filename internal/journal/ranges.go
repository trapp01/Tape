package journal

import (
	"context"
	"fmt"
	"strings"
)

// idChunk caps an IN list so a long id slice cannot blow SQLite's variable limit.
const idChunk = 500

// validateDayRange checks an inclusive [fromDay, toDay] pair. Both bounds are
// required: a report window always has two ends.
func validateDayRange(what, fromDay, toDay string) error {
	if err := validateDay(fromDay); err != nil {
		return fmt.Errorf("journal: %s: from %w", what, err)
	}
	if err := validateDay(toDay); err != nil {
		return fmt.Errorf("journal: %s: to %w", what, err)
	}
	if fromDay > toDay {
		return fmt.Errorf("journal: %s: from %s is after to %s", what, fromDay, toDay)
	}
	return nil
}

// dayRangeQuery appends the inclusive day bounds and the optional mode filter
// shared by every range reader.
func dayRangeQuery(query, mode, fromDay, toDay, order string) (string, []any) {
	query += ` WHERE day >= ? AND day <= ?`
	args := []any{fromDay, toDay}
	if mode != "" {
		query += ` AND mode = ?`
		args = append(args, mode)
	}
	return query + ` ORDER BY ` + order, args
}

// queryList runs query and scans every row. what names the read in any error.
func queryList[T any](ctx context.Context, s *Store, what, query string, args []any, scan func(scanner) (T, error)) ([]T, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: %s: %w", what, err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: %s: %w", what, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: %s: %w", what, err)
	}
	return out, nil
}

// ProposalsInRange returns every idea filed in [fromDay, toDay], oldest first.
// An empty mode covers both paper and live.
func (s *Store) ProposalsInRange(ctx context.Context, mode, fromDay, toDay string) ([]Proposal, error) {
	what := fmt.Sprintf("proposals for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+proposalColumns+` FROM proposals`, mode, fromDay, toDay, `day, id`)
	return queryList(ctx, s, what, query, args, scanProposal)
}

// CallsInRange returns the calls filed in [fromDay, toDay], scored or not,
// oldest first. An empty mode covers both paper and live.
func (s *Store) CallsInRange(ctx context.Context, mode, fromDay, toDay string) ([]Call, error) {
	what := fmt.Sprintf("calls for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+callColumns+` FROM calls`, mode, fromDay, toDay, `day, id`)
	return queryList(ctx, s, what, query, args, scanCall)
}

// RefusalsInRange returns every guardrail refusal in [fromDay, toDay], oldest
// first. An empty mode covers both paper and live.
func (s *Store) RefusalsInRange(ctx context.Context, mode, fromDay, toDay string) ([]Refusal, error) {
	what := fmt.Sprintf("refusals for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+refusalColumns+` FROM refusals`, mode, fromDay, toDay, `day, at, id`)
	return queryList(ctx, s, what, query, args, scanRefusal)
}

// BriefingsInRange returns the briefings written for [fromDay, toDay], oldest
// first. A re-run day carries more than one row. An empty mode covers both.
func (s *Store) BriefingsInRange(ctx context.Context, mode, fromDay, toDay string) ([]Briefing, error) {
	what := fmt.Sprintf("briefings for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+briefingColumns+` FROM briefings`, mode, fromDay, toDay, `day, generated_at, id`)
	return queryList(ctx, s, what, query, args, scanBriefing)
}

// OrdersByIDs looks up orders by journal id. An id with no row is absent from
// the map; an empty slice queries nothing.
func (s *Store) OrdersByIDs(ctx context.Context, ids []int64) (map[int64]Order, error) {
	return byIDs(ctx, s, "orders by ids", `SELECT `+orderColumns+` FROM orders WHERE id IN `, ids,
		scanOrder, func(o Order) int64 { return o.ID })
}

// ProposalsByIDs looks up proposals by journal id. An id with no row is absent
// from the map; an empty slice queries nothing.
func (s *Store) ProposalsByIDs(ctx context.Context, ids []int64) (map[int64]Proposal, error) {
	return byIDs(ctx, s, "proposals by ids", `SELECT `+proposalColumns+` FROM proposals WHERE id IN `, ids,
		scanProposal, func(p Proposal) int64 { return p.ID })
}

// byIDs runs prefix plus an IN list over ids, at most idChunk at a time, keyed
// by whatever key reports.
func byIDs[T any](ctx context.Context, s *Store, what, prefix string, ids []int64,
	scan func(scanner) (T, error), key func(T) int64) (map[int64]T, error) {

	out := make(map[int64]T, len(ids))
	for start := 0; start < len(ids); start += idChunk {
		chunk := ids[start:min(start+idChunk, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := queryList(ctx, s, what, prefix+placeholders(len(chunk)), args, scan)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[key(r)] = r
		}
	}
	return out, nil
}

// placeholders builds the "(?, ?, ?)" list for an IN clause of n values.
func placeholders(n int) string {
	return "(" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")"
}
