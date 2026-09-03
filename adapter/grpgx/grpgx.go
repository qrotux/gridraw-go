// Package grpgx executes gridraw statements through pgx and flattens rows
// into JSON-ready maps.
package grpgx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qrotux/gridraw-go"
)

// Querier is the subset of pgx satisfied by *pgxpool.Pool, *pgx.Conn and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Executor implements gridraw.Executor over a Querier.
type Executor struct{ DB Querier }

var _ gridraw.Executor = Executor{}

// New returns an Executor over db.
func New(db Querier) Executor { return Executor{DB: db} }

// Rows runs sql and scans every row onto keys.
func (e Executor) Rows(ctx context.Context, sql string, args []any, keys []string) ([]map[string]any, error) {
	rows, err := e.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanRows(rows, keys)
}

// Count runs sql and scans its single value.
func (e Executor) Count(ctx context.Context, sql string, args []any) (int64, error) {
	var total int64
	err := e.DB.QueryRow(ctx, sql, args...).Scan(&total)
	return total, err
}

// ScanRows maps every row positionally onto keys. jsonb arrives already
// decoded by pgx (map/slice), so it passes through unchanged.
func ScanRows(rows pgx.Rows, keys []string) ([]map[string]any, error) {
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		if len(vals) != len(keys) {
			return nil, fmt.Errorf("scan: %d values for %d keys", len(vals), len(keys))
		}
		m := make(map[string]any, len(keys))
		for i, v := range vals {
			m[keys[i]] = normalize(v)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func normalize(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case pgtype.Numeric:
		f, err := x.Float64Value()
		if err != nil || !f.Valid {
			return nil
		}
		return f.Float64
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	default:
		return v
	}
}
