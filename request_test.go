package gridraw

import (
	"strings"
	"testing"
	"time"
)

// steppedGrid gives opensAt a 15-minute and createdAt an hourly step.
func steppedGrid() Grid {
	g := validTestGrid()
	g.Columns[4].Step = 15 * time.Minute
	g.Columns[3].Step = time.Hour
	g.Columns[4].Filter.Operators = []Op{OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpBetween, OpNotBetween}
	g.Columns[3].Filter.Operators = g.Columns[4].Filter.Operators
	return g
}

// A ForContext result that drops the id column is a server bug: 500, not a panic.
func TestBuildQueryMissingIDColumnIs500(t *testing.T) {
	g := validTestGrid()
	g.Columns = g.Columns[1:] // drop "id"
	_, err := BuildQuery(&g, RowsRequest{Columns: []string{"email"}})
	if err == nil || err.Status != 500 || !strings.Contains(err.Msg, "idColumn") {
		t.Fatalf("err = %+v, want 500 about idColumn", err)
	}
}

func clockOf(v any) string { return v.(time.Time).Format(time.TimeOnly) }

func TestBuildQuery(t *testing.T) {
	base := func() Grid { return validTestGrid() }

	cases := []struct {
		name    string
		req     RowsRequest
		wantErr string // substring of ReqError.Msg; "" means success expected
		check   func(*testing.T, *Query)
		grid    func() Grid // defaults to base
	}{
		{
			name: "happy path: id auto-included, defaults applied",
			req:  RowsRequest{Columns: []string{"email"}},
			check: func(t *testing.T, q *Query) {
				if len(q.Keys) != 2 || q.Keys[0] != "email" || q.Keys[1] != "id" {
					t.Errorf("Keys = %v, want [email id]", q.Keys)
				}
				if len(q.Sorts) != 1 || q.Sorts[0].Spec != (SortSpec{Column: "email", Dir: "desc"}) {
					t.Errorf("Sorts = %+v, want single default", q.Sorts)
				}
				if q.Page != 1 {
					t.Errorf("Page = %d, want 1", q.Page)
				}
				if q.PageSize != 25 {
					t.Errorf("PageSize = %d, want 25 (grid default)", q.PageSize)
				}
			},
		},
		{
			name: "id explicitly requested is not duplicated",
			req:  RowsRequest{Columns: []string{"id", "email"}},
			check: func(t *testing.T, q *Query) {
				if len(q.Keys) != 2 {
					t.Errorf("Keys = %v, want len 2 (no duplicate id)", q.Keys)
				}
			},
		},
		{
			name: "datetime filter parses strict RFC3339",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{
					{Field: "createdAt", Op: OpGte, Value: "2024-01-01T00:00:00Z"},
				}},
			},
			check: func(t *testing.T, q *Query) {
				if len(q.Groups) != 1 || len(q.Groups[0]) != 1 {
					t.Fatalf("Groups = %+v, want 1 group of 1 clause", q.Groups)
				}
			},
		},
		{
			name: "datetime rejects non-RFC3339 string (date-only)",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{
					{Field: "createdAt", Op: OpGte, Value: "2024-01-01"},
				}},
			},
			wantErr: "invalid RFC3339",
		},
		{
			name: "datetime rejects non-string value",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{
					{Field: "createdAt", Op: OpGte, Value: 1234.0},
				}},
			},
			wantErr: "RFC3339 string expected",
		},
		{
			name:    "unknown column",
			req:     RowsRequest{Columns: []string{"bogus"}},
			wantErr: "unknown column",
		},
		{
			name: "unknown filter field",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "bogus", Op: OpEq, Value: "x"}}},
			},
			wantErr: "unknown or non-filterable",
		},
		{
			name: "disallowed operator for field",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "email", Op: OpGte, Value: "x"}}},
			},
			wantErr: "not allowed for field",
		},
		{
			name: "wrong value type",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "rating", Op: OpEq, Value: "not-a-number"}}},
			},
			wantErr: "number expected",
		},
		{
			name: "empty filter group",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{}},
			},
			wantErr: "is empty",
		},
		{
			name: "op outside the type matrix is rejected even when the column lists it",
			grid: func() Grid {
				g := validTestGrid()
				g.Columns[3].Filter.Operators = []Op{OpContains} // createdAt: contains is not a datetime op
				return g
			},
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "createdAt", Op: OpContains, Value: "2024"}}},
			},
			wantErr: "not allowed",
		},
		{
			name: "isNull ignores the value",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "email", Op: OpIsNull, Value: 42}}},
			},
			check: func(t *testing.T, q *Query) {
				if c := q.Groups[0][0]; c.Op != OpIsNull || c.Value != nil {
					t.Errorf("clause = %+v, want isNull with nil value", c)
				}
			},
		},
		{
			name: "date parses YYYY-MM-DD",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "birthday", Op: OpEq, Value: "2024-02-29"}}},
			},
			check: func(t *testing.T, q *Query) {
				d, ok := q.Groups[0][0].Value.(time.Time)
				if !ok || d.Format(time.DateOnly) != "2024-02-29" {
					t.Errorf("Value = %#v, want 2024-02-29 as time.Time", q.Groups[0][0].Value)
				}
			},
		},
		{
			name: "date rejects a value with a time part",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "birthday", Op: OpGt, Value: "2024-02-29T00:00:00Z"}}},
			},
			wantErr: "invalid date",
		},
		{
			name: "date between a>b",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "birthday", Op: OpBetween, Value: []any{"2024-03-01", "2024-02-01"}}}},
			},
			wantErr: "between requires a <= b",
		},
		{
			name: "notBetween takes a pair and checks order",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "rating", Op: OpNotBetween, Value: []any{5.0, 1.0}}}},
			},
			wantErr: "notBetween requires a <= b",
		},
		{
			name: "notBetween converts both bounds",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "birthday", Op: OpNotBetween, Value: []any{"2024-01-01", "2024-12-31"}}}},
			},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				a, ok1 := c.Value.(time.Time)
				b, ok2 := c.Value2.(time.Time)
				if !ok1 || !ok2 || a.Format(time.DateOnly) != "2024-01-01" || b.Format(time.DateOnly) != "2024-12-31" {
					t.Errorf("clause = %+v, want two dates", c)
				}
			},
		},
		{
			name: "time accepts HH:MM:SS and HH:MM",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "opensAt", Op: OpBetween, Value: []any{"09:30", "17:45:10"}}}},
			},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				a, _ := c.Value.(time.Time)
				b, _ := c.Value2.(time.Time)
				if a.Format(time.TimeOnly) != "09:30:00" || b.Format(time.TimeOnly) != "17:45:10" {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name:    "time rejects a single-digit hour",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "opensAt", Op: OpEq, Value: "9:00"}}}},
			wantErr: "invalid time",
		},
		{
			name: "time rejects a date",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "opensAt", Op: OpEq, Value: "2024-01-01"}}},
			},
			wantErr: "invalid time",
		},
		{
			name: "time range across midnight is rejected",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "opensAt", Op: OpNotBetween, Value: []any{"22:00", "06:00"}}}},
			},
			wantErr: "notBetween requires a <= b",
		},
		{
			name: "step: eq widens to a half-open bucket",
			grid: steppedGrid,
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "opensAt", Op: OpEq, Value: "09:15"}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				if c.Op != OpBetween || !c.UpperOpen || clockOf(c.Value) != "09:15:00" || clockOf(c.Value2) != "09:30:00" {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name: "step: neq widens to a negated half-open bucket",
			grid: steppedGrid,
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "opensAt", Op: OpNeq, Value: "09:15"}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				if c.Op != OpNotBetween || !c.UpperOpen || clockOf(c.Value2) != "09:30:00" {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name: "step: gt and lte move to the next bucket edge, gte and lt stay",
			grid: steppedGrid,
			req: RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{
				{Field: "opensAt", Op: OpGt, Value: "09:15"},
				{Field: "opensAt", Op: OpLte, Value: "09:15"},
				{Field: "opensAt", Op: OpGte, Value: "09:15"},
				{Field: "opensAt", Op: OpLt, Value: "09:15"},
			}}},
			check: func(t *testing.T, q *Query) {
				g := q.Groups[0]
				want := []struct {
					op Op
					v  string
				}{{OpGte, "09:30:00"}, {OpLt, "09:30:00"}, {OpGte, "09:15:00"}, {OpLt, "09:15:00"}}
				for i, w := range want {
					if g[i].Op != w.op || clockOf(g[i].Value) != w.v {
						t.Errorf("clause %d = %+v, want %s %s", i, g[i], w.op, w.v)
					}
				}
			},
		},
		{
			name: "step: between widens the upper bound and the last bucket ends at midnight",
			grid: steppedGrid,
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "opensAt", Op: OpBetween, Value: []any{"09:00", "23:45"}}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				hi := c.Value2.(time.Time)
				if !c.UpperOpen || hi.Day() != 2 || clockOf(hi) != "00:00:00" {
					t.Errorf("clause = %+v (Value2 day=%d)", c, hi.Day())
				}
			},
		},
		{
			name:    "step: misaligned value is rejected",
			grid:    steppedGrid,
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "opensAt", Op: OpEq, Value: "09:20"}}}},
			wantErr: "aligned to 15m0s",
		},
		{
			name: "step: hourly datetime aligns in the value's own zone",
			grid: steppedGrid,
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "createdAt", Op: OpGt, Value: "2024-06-01T09:00:00+05:30"}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				want := time.Date(2024, 6, 1, 10, 0, 0, 0, time.FixedZone("", 5*3600+1800))
				if c.Op != OpGte || !c.Value.(time.Time).Equal(want) {
					t.Errorf("clause = %+v, want gte %v", c, want)
				}
			},
		},
		{
			name: "step: hourly datetime eq widens to the hour",
			grid: steppedGrid,
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "createdAt", Op: OpEq, Value: "2024-06-01T09:00:00Z"}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				if c.Op != OpBetween || !c.UpperOpen || !c.Value2.(time.Time).Equal(c.Value.(time.Time).Add(time.Hour)) {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name:    "default step rejects fractional seconds on datetime",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "createdAt", Op: OpGte, Value: "2024-06-01T09:00:00.5Z"}}}},
			wantErr: "aligned to 1s",
		},
		{
			name: "uuid is validated and lowercased",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "id", Op: OpEq, Value: "0F40E110-944F-46A3-8A9F-0A81221CBC41"}}}},
			check: func(t *testing.T, q *Query) {
				if q.Groups[0][0].Value != "0f40e110-944f-46a3-8a9f-0a81221cbc41" {
					t.Errorf("Value = %#v", q.Groups[0][0].Value)
				}
			},
		},
		{
			name:    "uuid rejects a non-uuid string before it reaches SQL",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "id", Op: OpEq, Value: "not-a-uuid"}}}},
			wantErr: "uuid expected",
		},
		{
			name: "uuid in validates every element",
			req: RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "id", Op: OpIn,
				Value: []any{"0f40e110-944f-46a3-8a9f-0a81221cbc41", "zz"}}}}},
			wantErr: "array of uuids",
		},
		{
			name: "decimal keeps the exact string",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "price", Op: OpEq, Value: "19.990"}}}},
			check: func(t *testing.T, q *Query) {
				if q.Groups[0][0].Value != "19.990" {
					t.Errorf("Value = %#v", q.Groups[0][0].Value)
				}
			},
		},
		{
			name:    "decimal rejects a JSON number",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "price", Op: OpEq, Value: 19.99}}}},
			wantErr: "decimal string expected",
		},
		{
			name:    "decimal rejects malformed strings",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "price", Op: OpGt, Value: "1e5"}}}},
			wantErr: "decimal string expected",
		},
		{
			name:    "decimal range order is numeric, not lexical",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "price", Op: OpBetween, Value: []any{"10.5", "9.75"}}}}},
			wantErr: "between requires a <= b",
		},
		{
			name: "array: elements converted with the scalar converter",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "tags", Op: OpContainsAny, Value: []any{"go", "sql"}}}}},
			check: func(t *testing.T, q *Query) {
				v, ok := q.Groups[0][0].Value.([]string)
				if !ok || len(v) != 2 || v[1] != "sql" {
					t.Errorf("Value = %#v, want []string{go sql}", q.Groups[0][0].Value)
				}
			},
		},
		{
			name: "count is on by default, limit is one row over the page",
			req:  RowsRequest{Columns: []string{"email"}, PageSize: 25},
			check: func(t *testing.T, q *Query) {
				if !q.WithTotal || q.RowLimit() != 26 {
					t.Errorf("WithTotal=%v RowLimit=%d, want true and 26", q.WithTotal, q.RowLimit())
				}
			},
		},
		{
			name: "array: containsOnly carries the whole set",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "tags", Op: OpContainsOnly, Value: []any{"go", "sql"}}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				if v, ok := c.Value.([]string); c.Op != OpContainsOnly || !ok || len(v) != 2 {
					t.Errorf("clause = %+v, want containsOnly over two strings", c)
				}
			},
		},
		{
			name:    "array: containsOnly rejected on a scalar column",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "email", Op: OpContainsOnly, Value: []any{"go"}}}}},
			wantErr: "op",
		},
		{
			name:    "array: empty value rejected",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "tags", Op: OpContainsAll, Value: []any{}}}}},
			wantErr: "non-empty array",
		},
		{
			name:    "array: wrong element type rejected",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "tags", Op: OpContainsAny, Value: []any{"go", 1}}}}},
			wantErr: "string expected",
		},
		{
			name: "array: isEmpty ignores the value",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "tags", Op: OpIsEmpty, Value: "x"}}}},
			check: func(t *testing.T, q *Query) {
				if c := q.Groups[0][0]; c.Op != OpIsEmpty || c.Value != nil {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name: "array: time elements parse and must align to step, no widening",
			req:  RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "slots", Op: OpContainsAny, Value: []any{"09:15", "23:45:00"}}}}},
			check: func(t *testing.T, q *Query) {
				c := q.Groups[0][0]
				v, ok := c.Value.([]time.Time)
				if !ok || len(v) != 2 || clockOf(v[0]) != "09:15:00" || c.Op != OpContainsAny || c.UpperOpen {
					t.Errorf("clause = %+v", c)
				}
			},
		},
		{
			name:    "array: misaligned time element rejected",
			req:     RowsRequest{Columns: []string{"email"}, Filters: [][]FilterClause{{{Field: "slots", Op: OpContainsAny, Value: []any{"09:20"}}}}},
			wantErr: "aligned to 15m0s",
		},
		{
			name: "neq string converts",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "email", Op: OpNeq, Value: "a@b"}}},
			},
			check: func(t *testing.T, q *Query) {
				if q.Groups[0][0].Value != "a@b" {
					t.Errorf("Value = %#v", q.Groups[0][0].Value)
				}
			},
		},
		{
			name: "notIn converts to []string",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "role", Op: OpNotIn, Value: []any{"admin"}}}},
			},
			check: func(t *testing.T, q *Query) {
				vals, ok := q.Groups[0][0].Value.([]string)
				if !ok || len(vals) != 1 || vals[0] != "admin" {
					t.Errorf("Value = %#v, want []string{\"admin\"}", q.Groups[0][0].Value)
				}
			},
		},
		{
			name: "empty notIn array",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "role", Op: OpNotIn, Value: []any{}}}},
			},
			wantErr: "non-empty array",
		},
		{
			name: "empty in array",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "role", Op: OpIn, Value: []any{}}}},
			},
			wantErr: "non-empty array",
		},
		{
			name: "between a>b",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{Field: "rating", Op: OpBetween, Value: []any{10.0, 5.0}}}},
			},
			wantErr: "between requires a <= b",
		},
		{
			name: "between a>b (datetime)",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: [][]FilterClause{{{
					Field: "createdAt", Op: OpBetween,
					Value: []any{"2024-06-01T00:00:00Z", "2024-01-01T00:00:00Z"},
				}}},
			},
			wantErr: "between requires a <= b",
		},
		{
			name: "more than 10 filter groups",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: manyGroups(maxGroups+1, 1),
			},
			wantErr: "too many filter groups",
		},
		{
			name: "more than 20 clauses in a group",
			req: RowsRequest{
				Columns: []string{"email"},
				Filters: manyGroups(1, maxClauses+1),
			},
			wantErr: "too many clauses",
		},
		{
			name:    "page < 1",
			req:     RowsRequest{Columns: []string{"email"}, Page: -1},
			wantErr: "page must be >= 1",
		},
		{
			name:    "pageSize out of range (too high)",
			req:     RowsRequest{Columns: []string{"email"}, PageSize: maxPageSize + 1},
			wantErr: "pageSize must be 1..",
		},
		{
			name:    "pageSize out of range (negative)",
			req:     RowsRequest{Columns: []string{"email"}, PageSize: -1},
			wantErr: "pageSize must be 1..",
		},
		{
			name: "sort on non-sortable column",
			req: RowsRequest{
				Columns: []string{"email"},
				Sort:    []SortSpec{{Column: "role", Dir: "asc"}},
			},
			wantErr: "invalid sort",
		},
		{
			name: "sort on unknown column",
			req: RowsRequest{
				Columns: []string{"email"},
				Sort:    []SortSpec{{Column: "bogus", Dir: "asc"}},
			},
			wantErr: "invalid sort",
		},
		{
			name: "multi-column sort keeps request order",
			req: RowsRequest{
				Columns: []string{"email"},
				Sort:    []SortSpec{{Column: "rating", Dir: "desc"}, {Column: "email", Dir: "asc"}},
			},
			check: func(t *testing.T, q *Query) {
				if len(q.Sorts) != 2 || q.Sorts[0].Spec.Column != "rating" || q.Sorts[1].Spec.Column != "email" {
					t.Errorf("Sorts = %+v, want [rating email] in order", q.Sorts)
				}
			},
		},
		{
			name: "duplicate sort column is rejected",
			req: RowsRequest{
				Columns: []string{"email"},
				Sort:    []SortSpec{{Column: "email", Dir: "asc"}, {Column: "email", Dir: "desc"}},
			},
			wantErr: "duplicate sort column",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := base()
			if tc.grid != nil {
				g = tc.grid()
			}
			q, err := BuildQuery(&g, tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildQuery() error = %v, want success", err)
				}
				if tc.check != nil {
					tc.check(t, q)
				}
				return
			}
			if err == nil {
				t.Fatalf("BuildQuery() succeeded, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Msg, tc.wantErr) {
				t.Errorf("err.Msg = %q, want substring %q", err.Msg, tc.wantErr)
			}
			if err.Status != 400 {
				t.Errorf("err.Status = %d, want 400", err.Status)
			}
		})
	}
}

// manyGroups builds nGroups filter groups each with clausesPerGroup identical
// valid clauses, for exercising the DNF ceiling checks (maxGroups/maxClauses)
// without tripping other validation first.
func manyGroups(nGroups, clausesPerGroup int) [][]FilterClause {
	groups := make([][]FilterClause, nGroups)
	for i := range groups {
		clauses := make([]FilterClause, clausesPerGroup)
		for j := range clauses {
			clauses[j] = FilterClause{Field: "email", Op: OpEq, Value: "x"}
		}
		groups[i] = clauses
	}
	return groups
}
