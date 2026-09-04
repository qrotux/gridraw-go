package grpgx

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestNormalize(t *testing.T) {
	ts := time.Date(2024, 2, 29, 13, 4, 5, 0, time.FixedZone("x", 3*3600))
	cases := []struct {
		name string
		in   any
		oid  uint32
		want any
	}{
		{"nil", nil, 0, nil},
		{"timestamptz to UTC RFC3339", ts, pgtype.TimestamptzOID, "2024-02-29T10:04:05Z"},
		{"date keeps the calendar day", time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), pgtype.DateOID, "2024-02-29"},
		{"time of day", pgtype.Time{Microseconds: (13*3600 + 4*60 + 5) * 1_000_000, Valid: true}, pgtype.TimeOID, "13:04:05"},
		{"time NULL", pgtype.Time{}, pgtype.TimeOID, nil},
		{"time 24:00:00", pgtype.Time{Microseconds: 24 * 3600 * 1_000_000, Valid: true}, pgtype.TimeOID, "24:00:00"},
		{"numeric", pgtype.Numeric{Int: big.NewInt(45), Exp: -1, Valid: true}, pgtype.NumericOID, 4.5},
		{"numeric NULL", pgtype.Numeric{}, pgtype.NumericOID, nil},
		{"uuid bytes", [16]byte{0x0f, 0x40, 0xe1, 0x10, 0x94, 0x4f, 0x46, 0xa3, 0x8a, 0x9f, 0x0a, 0x81, 0x22, 0x1c, 0xbc, 0x41}, pgtype.UUIDOID, "0f40e110-944f-46a3-8a9f-0a81221cbc41"},
		{"passthrough", "text", pgtype.TextOID, "text"},
	}
	arrays := []struct {
		name string
		in   any
		oid  uint32
		want string
	}{
		{"text[]", []any{"a", "b"}, pgtype.TextArrayOID, "[a b]"},
		{"date[]", []any{time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)}, pgtype.DateArrayOID, "[2024-02-29]"},
		{"uuid[]", []any{[16]byte{0x0f, 0x40, 0xe1, 0x10, 0x94, 0x4f, 0x46, 0xa3, 0x8a, 0x9f, 0x0a, 0x81, 0x22, 0x1c, 0xbc, 0x41}}, pgtype.UUIDArrayOID, "[0f40e110-944f-46a3-8a9f-0a81221cbc41]"},
		{"numeric[]", []any{pgtype.Numeric{Int: big.NewInt(150), Exp: -2, Valid: true}}, pgtype.NumericArrayOID, "[1.5]"},
		{"empty", []any{}, pgtype.TextArrayOID, "[]"},
	}
	for _, tc := range arrays {
		if got := fmt.Sprint(normalize(tc.in, tc.oid)); got != tc.want {
			t.Errorf("%s: normalize = %s, want %s", tc.name, got, tc.want)
		}
	}
	for _, tc := range cases {
		if got := normalize(tc.in, tc.oid); got != tc.want {
			t.Errorf("%s: normalize(%v, %d) = %#v, want %#v", tc.name, tc.in, tc.oid, got, tc.want)
		}
	}
}
