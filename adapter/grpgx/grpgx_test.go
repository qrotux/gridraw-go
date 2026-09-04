package grpgx

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func openDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GRIDRAW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GRIDRAW_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Pins how pgx surfaces each type through rows.Values(): uuid as [16]byte,
// numeric as pgtype.Numeric, timestamptz and date both as time.Time, time
// as pgtype.Time, jsonb already decoded. normalize relies on every one of these.
func TestScanRowsDB(t *testing.T) {
	pool := openDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE TEMP TABLE gridraw_scan (
		id uuid DEFAULT gen_random_uuid(),
		email text,
		rating numeric,
		created_at timestamptz,
		last_seen_at timestamptz,
		changes jsonb,
		metadata jsonb,
		birthday date,
		opens_at time,
		tags text[],
		days date[]
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO gridraw_scan (email, rating, created_at, last_seen_at, changes, metadata, birthday, opens_at, tags, days)
		VALUES ('scan@test.dev', 4.5, '2026-01-02T03:04:05Z', NULL,
		        '[{"field":"username","old":null,"new":"probe"}]'::jsonb,
		        '{"event":"probe","deviceCount":1}'::jsonb,
		        '2024-02-29', '09:30', '{go,sql}', '{2024-01-01,2024-12-31}')`)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"id", "email", "rating", "created_at", "last_seen_at", "changes", "metadata", "birthday", "opens_at", "tags", "days"}
	got, err := New(pool).Rows(ctx, `SELECT id, email, rating, created_at, last_seen_at, changes, metadata, birthday, opens_at, tags, days FROM gridraw_scan`, nil, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	r := got[0]
	if s, ok := r["id"].(string); !ok || len(s) != 36 {
		t.Errorf("id: want uuid string, got %v (%T)", r["id"], r["id"])
	}
	if r["rating"] != 4.5 {
		t.Errorf("rating: want 4.5 float64, got %v (%T)", r["rating"], r["rating"])
	}
	if r["created_at"] != "2026-01-02T03:04:05Z" {
		t.Errorf("created_at: %v", r["created_at"])
	}
	if r["birthday"] != "2024-02-29" {
		t.Errorf("birthday: want 2024-02-29 date string, got %v (%T)", r["birthday"], r["birthday"])
	}
	if r["opens_at"] != "09:30:00" {
		t.Errorf("opens_at: want 09:30:00, got %v (%T)", r["opens_at"], r["opens_at"])
	}
	if tags := fmt.Sprint(r["tags"]); tags != "[go sql]" {
		t.Errorf("tags: want [go sql], got %s (%T)", tags, r["tags"])
	}
	if days := fmt.Sprint(r["days"]); days != "[2024-01-01 2024-12-31]" {
		t.Errorf("days: want date strings, got %s", days)
	}
	if r["last_seen_at"] != nil {
		t.Errorf("NULL: want nil, got %v", r["last_seen_at"])
	}
	changes, ok := r["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("changes: got %T (%v), want []any of 1", r["changes"], r["changes"])
	}
	if first, ok := changes[0].(map[string]any); !ok || first["field"] != "username" {
		t.Errorf("changes[0] = %v", changes[0])
	}
	meta, ok := r["metadata"].(map[string]any)
	if !ok || meta["event"] != "probe" {
		t.Errorf("metadata: got %T (%v)", r["metadata"], r["metadata"])
	}

	total, err := New(pool).Count(ctx, `SELECT COUNT(*) FROM gridraw_scan`, nil)
	if err != nil || total != 1 {
		t.Errorf("count = %d, err = %v", total, err)
	}
}

func TestScanRowsKeyMismatch(t *testing.T) {
	pool := openDB(t)
	rows, err := pool.Query(context.Background(), `SELECT 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if _, err := ScanRows(rows, []string{"a"}); err == nil {
		t.Fatal("expected key/value count mismatch error")
	}
}
