package gridraw

import (
	"strings"
	"testing"
)

func TestBuildQuery(t *testing.T) {
	base := func() Grid { return validTestGrid() }

	cases := []struct {
		name    string
		req     RowsRequest
		wantErr string // substring of ReqError.Msg; "" means success expected
		check   func(*testing.T, *Query)
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
