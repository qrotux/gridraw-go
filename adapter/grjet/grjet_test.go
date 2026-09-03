package grjet

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-jet/jet/v2/postgres"

	"github.com/qrotux/gridraw-go"
)

var (
	colID        = postgres.StringColumn("id")
	colEmail     = postgres.StringColumn("email")
	colRating    = postgres.FloatColumn("rating")
	colCreatedAt = postgres.TimestampzColumn("created_at")
	colIsBanned  = postgres.BoolColumn("is_banned")
	colRole      = postgres.StringColumn("role")
	colPrefs     = postgres.StringColumn("prefs")
	users        = postgres.NewTable("public", "users", "", colID, colEmail, colRating, colCreatedAt, colIsBanned, colRole, colPrefs)
)

func validTestGrid() gridraw.Grid {
	return gridraw.Grid{
		Name: "t", IDColumn: "id", PageSize: 25,
		DefaultSort: gridraw.SortSpec{Column: "email", Dir: "desc"},
		Binding:     Base(func() postgres.ReadableTable { return users }),
		Columns: []gridraw.Column{
			StrColNoFilter("id", colID),
			Searchable(StrCol("email", colEmail)),
			NumCol("rating", colRating),
			TsCol("createdAt", colCreatedAt),
			BoolCol("isBanned", colIsBanned),
			EnumCol("role", colRole, []string{"user", "admin"}),
		},
	}
}

func testRegistry(t *testing.T) *gridraw.Grid {
	t.Helper()
	reg, err := gridraw.NewRegistry(Compiler{}, validTestGrid())
	if err != nil {
		t.Fatal(err)
	}
	g, ok := reg.Get("t")
	if !ok {
		t.Fatal("grid not registered")
	}
	return g
}

func compile(t *testing.T, g *gridraw.Grid, req gridraw.RowsRequest) gridraw.Statements {
	t.Helper()
	q, rerr := gridraw.BuildQuery(g, req)
	if rerr != nil {
		t.Fatal(rerr)
	}
	st, err := Compiler{}.Compile(q)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*gridraw.Grid)
		want string
	}{
		{"missing base", func(g *gridraw.Grid) { g.Binding = nil }, "GridBinding"},
		{"nil binding", func(g *gridraw.Grid) { g.Columns[1].Binding = nil }, "grjet.Binding"},
		{"wrong binding type", func(g *gridraw.Grid) { g.Columns[1].Binding = "x" }, "grjet.Binding"},
		// AS() yields a Projection that is not an Expression, so nothing to sort by.
		{"sortable without sort expr", func(g *gridraw.Grid) {
			g.Columns[1].Binding = Binding{Projection: colEmail.AS("x")}
		}, "sortable without sort expression"},
		{"filter without expr", func(g *gridraw.Grid) {
			g.Columns[1].Binding = Binding{Projection: colEmail.AS("x")}
			g.Columns[1].Sortable = false
			g.DefaultSort = gridraw.SortSpec{Column: "id", Dir: "asc"}
		}, "filter without expression"},
		{"id not order-by-able", func(g *gridraw.Grid) {
			g.Columns[0].Binding = Binding{Projection: colEmail.AS("x")}
			g.Columns[0].Sortable = false
		}, "order-by-able"},
	}
	for _, tc := range cases {
		g := validTestGrid()
		tc.mut(&g)
		_, err := gridraw.NewRegistry(Compiler{}, g)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want contains %q", tc.name, err, tc.want)
		}
	}
}

func TestJSONColRegisters(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, JSONCol("prefs", colPrefs))
	if _, err := gridraw.NewRegistry(Compiler{}, g); err != nil {
		t.Fatal(err)
	}
}

func TestJoinStrColRegistersAndCompiles(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, JoinStrCol("roleAlias", colRole))
	reg, err := gridraw.NewRegistry(Compiler{}, g)
	if err != nil {
		t.Fatal(err)
	}
	gr, _ := reg.Get("t")
	st := compile(t, gr, gridraw.RowsRequest{
		Columns: []string{"roleAlias"},
		Filters: [][]gridraw.FilterClause{{{Field: "roleAlias", Op: gridraw.OpEq, Value: "x"}}},
		Sort:    []gridraw.SortSpec{{Column: "roleAlias", Dir: "asc"}},
	})
	if !strings.Contains(st.RowsSQL, `AS "roleAlias"`) {
		t.Errorf("projection not aliased:\n%s", st.RowsSQL)
	}
	if strings.Contains(st.CountSQL, "roleAlias") {
		t.Errorf("alias leaked into WHERE:\n%s", st.CountSQL)
	}
}

func TestSQLSearchAndFilters(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Search:  "iv_an%",
		Filters: [][]gridraw.FilterClause{
			{
				{Field: "rating", Op: gridraw.OpGte, Value: 4.0},
				{Field: "isBanned", Op: gridraw.OpEq, Value: true},
			},
			{
				{Field: "role", Op: gridraw.OpIn, Value: []any{"admin"}},
			},
		},
	})
	for _, frag := range []string{"ILIKE", " OR ", " AND ", "ORDER BY", "NULLS LAST", "LIMIT"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	// "_" and "%" from user input must be escaped or they act as wildcards.
	if !strings.Contains(fmt.Sprint(st.RowsArgs), `%iv\_an\%%`) {
		t.Errorf("search pattern not escaped: %v", st.RowsArgs)
	}
	if strings.Contains(st.CountSQL, "ORDER BY") || strings.Contains(st.CountSQL, "LIMIT") {
		t.Errorf("count must not sort/limit:\n%s", st.CountSQL)
	}
	if len(st.CountArgs) == 0 {
		t.Error("count args empty")
	}
}

// Search alone must produce the WHERE: the combined test's OR token comes
// from the filter DNF, not the search branch.
func TestSQLSearchOnly(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{Columns: []string{"email"}, Search: "iv_an%"})
	if !strings.Contains(st.RowsSQL, "WHERE") || !strings.Contains(st.RowsSQL, "ILIKE") {
		t.Errorf("expected WHERE ... ILIKE from search in:\n%s", st.RowsSQL)
	}
	if !strings.Contains(fmt.Sprint(st.RowsArgs), `%iv\_an\%%`) {
		t.Errorf("search pattern not escaped: %v", st.RowsArgs)
	}
	if !strings.Contains(st.CountSQL, "WHERE") || !strings.Contains(st.CountSQL, "ILIKE") {
		t.Errorf("count must carry the same search predicate:\n%s", st.CountSQL)
	}
	if !strings.Contains(fmt.Sprint(st.CountArgs), `%iv\_an\%%`) {
		t.Errorf("count args missing escaped search pattern: %v", st.CountArgs)
	}
}

func TestSQLNoWhere(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{Columns: []string{"email"}})
	if strings.Contains(st.RowsSQL, "WHERE") {
		t.Errorf("expected no WHERE for empty filters+search:\n%s", st.RowsSQL)
	}
	if strings.Contains(st.CountSQL, "WHERE") {
		t.Errorf("expected no WHERE in count:\n%s", st.CountSQL)
	}
}

func TestSQLTiebreaker(t *testing.T) {
	gr := testRegistry(t)
	build := func(sort []gridraw.SortSpec) *gridraw.Query {
		q, rerr := gridraw.BuildQuery(gr, gridraw.RowsRequest{Columns: []string{"email"}, Sort: sort})
		if rerr != nil {
			t.Fatal(rerr)
		}
		return q
	}
	// default sort (email) != id → tiebreaker appended
	if n := len(orderBy(build(nil))); n != 2 {
		t.Fatalf("expected 2 order-by clauses (sort + tiebreaker), got %d", n)
	}
	// sort by id itself → no tiebreaker
	if n := len(orderBy(build([]gridraw.SortSpec{{Column: "id", Dir: "asc"}}))); n != 1 {
		t.Fatalf("expected 1 order-by clause when sort==id, got %d", n)
	}
	// two sorts without id → 2 + tiebreaker
	if n := len(orderBy(build([]gridraw.SortSpec{{Column: "rating", Dir: "desc"}, {Column: "email", Dir: "asc"}}))); n != 3 {
		t.Fatalf("expected 3 order-by clauses, got %d", n)
	}
}

func TestSQLBetweenDatetime(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{
			{{Field: "createdAt", Op: gridraw.OpBetween, Value: []any{"2026-01-01T00:00:00Z", "2026-06-01T00:00:00Z"}}},
		},
	})
	if !strings.Contains(st.RowsSQL, "BETWEEN") {
		t.Errorf("missing BETWEEN in:\n%s", st.RowsSQL)
	}
	if len(st.RowsArgs) < 2 {
		t.Fatalf("expected at least 2 args, got %v", st.RowsArgs)
	}
}

func TestSQLPagination(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{Columns: []string{"email"}, Page: 3, PageSize: 25})
	if !strings.Contains(st.RowsSQL, "OFFSET") {
		t.Fatalf("expected OFFSET in:\n%s", st.RowsSQL)
	}
	// LIMIT and OFFSET are the last two args (no WHERE args here).
	args := st.RowsArgs
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args (limit, offset), got %v", args)
	}
	limitArg, offsetArg := args[len(args)-2], args[len(args)-1]
	if n, ok := limitArg.(int64); !ok || n != 25 {
		t.Errorf("expected LIMIT arg int64(25), got %v (%T)", limitArg, limitArg)
	}
	if n, ok := offsetArg.(int64); !ok || n != 50 {
		t.Errorf("expected OFFSET arg int64(50), got %v (%T)", offsetArg, offsetArg)
	}
}

// Boolean filters must use IS TRUE / IS NOT TRUE, not "= $n": with "= $n"
// NULL rows would be invisible to both eq true and eq false.
func TestSQLBoolFilterUsesIsTrueNotEq(t *testing.T) {
	gr := testRegistry(t)
	for _, tc := range []struct {
		name  string
		value bool
		want  string
	}{
		{"eq true", true, "IS TRUE"},
		{"eq false", false, "IS NOT TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := compile(t, gr, gridraw.RowsRequest{
				Columns: []string{"email"},
				Filters: [][]gridraw.FilterClause{{{Field: "isBanned", Op: gridraw.OpEq, Value: tc.value}}},
			})
			if !strings.Contains(st.RowsSQL, tc.want) {
				t.Errorf("expected %q in WHERE, got:\n%s", tc.want, st.RowsSQL)
			}
			if strings.Contains(st.RowsSQL, "is_banned = $") {
				t.Errorf("bool filter uses `= $n`:\n%s", st.RowsSQL)
			}
		})
	}
}

// A consumer-defined binding (filter on COALESCE, projection on the raw
// column) must validate and compile through the exported Binding fields.
func TestCustomBinding(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, gridraw.Column{
		Key: "flag", Type: gridraw.TypeBool, Sortable: true,
		Binding: Binding{Projection: colIsBanned, Filter: postgres.COALESCE(colIsBanned, postgres.Bool(true))},
		Filter:  &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpEq}},
	})
	reg, err := gridraw.NewRegistry(Compiler{}, g)
	if err != nil {
		t.Fatal(err)
	}
	gr, _ := reg.Get("t")
	st := compile(t, gr, gridraw.RowsRequest{
		Columns: []string{"flag"},
		Filters: [][]gridraw.FilterClause{{{Field: "flag", Op: gridraw.OpEq, Value: false}}},
	})
	if !strings.Contains(st.RowsSQL, "COALESCE") {
		t.Errorf("custom filter expression not used:\n%s", st.RowsSQL)
	}
}
