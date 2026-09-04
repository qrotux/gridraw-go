package grjet

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	colBirthday  = postgres.DateColumn("birthday")
	colOpensAt   = postgres.TimeColumn("opens_at")
	colSlot      = postgres.TimeColumn("slot")
	colPrice     = postgres.FloatColumn("price")
	colLocale    = postgres.StringColumn("locale")
	colTags      = postgres.StringColumn("tags")
	colLocales   = postgres.StringColumn("locales")
	colDays      = postgres.DateColumn("days")
	colIDs       = postgres.StringColumn("ids")
	colScores    = postgres.FloatColumn("scores")
	colAmounts   = postgres.FloatColumn("amounts")
	colFlags     = postgres.BoolColumn("flags")
	colSeen      = postgres.TimestampzColumn("seen")
	colSlots     = postgres.TimeColumn("slots")
	users        = postgres.NewTable("public", "users", "", colID, colEmail, colRating, colCreatedAt, colIsBanned, colRole, colPrefs, colBirthday, colOpensAt, colSlot, colPrice, colLocale, colTags, colLocales, colDays, colIDs, colScores, colAmounts, colFlags, colSeen, colSlots)
)

func validTestGrid() gridraw.Grid {
	return gridraw.Grid{
		Name: "t", IDColumn: "id", PageSize: 25,
		DefaultSort: gridraw.SortSpec{Column: "email", Dir: "desc"},
		Binding:     Base(func() postgres.ReadableTable { return users }),
		Columns: []gridraw.Column{
			UUIDCol("id", colID),
			StrCol("email", colEmail).WithSearch(),
			NumCol("rating", colRating),
			TsCol("createdAt", colCreatedAt).Nullable(),
			BoolCol("isBanned", colIsBanned),
			DateCol("birthday", colBirthday),
			TimeCol("opensAt", colOpensAt),
			TimeCol("slot", colSlot).WithStep(15 * time.Minute),
			DecimalCol("price", colPrice),
			PgType("_locales", EnumCol("locale", colLocale, []string{"en", "ru"})),
			ArrayCol("tags", colTags, gridraw.TypeString).WithSearch(),
			PgType("_locales", EnumArrayCol("locales", colLocales, []string{"en", "ru"})),
			ArrayCol("days", colDays, gridraw.TypeDate),
			ArrayCol("ids", colIDs, gridraw.TypeUUID),
			PgType("int4", ArrayCol("scores", colScores, gridraw.TypeNumber)),
			ArrayCol("amounts", colAmounts, gridraw.TypeDecimal),
			ArrayCol("flags", colFlags, gridraw.TypeBool),
			ArrayCol("seen", colSeen, gridraw.TypeDatetime),
			ArrayCol("slots", colSlots, gridraw.TypeTime).WithStep(15 * time.Minute),
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
			g.Columns[0].Filter = nil
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
	// LIMIT and OFFSET are the last two args (no WHERE args here). The limit
	// is q.RowLimit(), one row over the page, which is how the handler answers
	// hasNext without a count.
	args := st.RowsArgs
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args (limit, offset), got %v", args)
	}
	limitArg, offsetArg := args[len(args)-2], args[len(args)-1]
	if n, ok := limitArg.(int64); !ok || n != 26 {
		t.Errorf("expected LIMIT arg int64(26), got %v (%T)", limitArg, limitArg)
	}
	if n, ok := offsetArg.(int64); !ok || n != 50 {
		t.Errorf("expected OFFSET arg int64(50), got %v (%T)", offsetArg, offsetArg)
	}
}

// Boolean filters must use IS TRUE / IS NOT TRUE, not "= $n": with "= $n"
// NULL rows would be invisible to both eq true and eq false.
func TestSQLStringOps(t *testing.T) {
	gr := testRegistry(t)
	for _, tc := range []struct {
		op   gridraw.Op
		arg  string // bound ILIKE pattern
		null bool   // NULL rows kept
	}{
		{gridraw.OpEq, `a\_b`, false},
		{gridraw.OpNeq, `a\_b`, true},
		{gridraw.OpContains, `%a\_b%`, false},
		{gridraw.OpNotContains, `%a\_b%`, true},
		{gridraw.OpStarts, `a\_b%`, false},
		{gridraw.OpEnds, `%a\_b`, false},
	} {
		t.Run(string(tc.op), func(t *testing.T) {
			st := compile(t, gr, gridraw.RowsRequest{
				Columns: []string{"email"},
				Filters: [][]gridraw.FilterClause{{{Field: "email", Op: tc.op, Value: "a_b"}}},
			})
			if !strings.Contains(st.RowsSQL, "ILIKE") || strings.Contains(st.RowsSQL, "email = $") {
				t.Errorf("string op must go through ILIKE:\n%s", st.RowsSQL)
			}
			if st.RowsArgs[0] != tc.arg {
				t.Errorf("pattern = %q, want %q", st.RowsArgs[0], tc.arg)
			}
			if got := strings.Contains(st.RowsSQL, "IS NULL"); got != tc.null {
				t.Errorf("IS NULL present = %v, want %v:\n%s", got, tc.null, st.RowsSQL)
			}
		})
	}
}

func TestSQLNullOps(t *testing.T) {
	gr := testRegistry(t)
	for _, tc := range []struct {
		op   gridraw.Op
		want string
	}{
		{gridraw.OpIsNull, "created_at IS NULL"},
		{gridraw.OpIsNotNull, "created_at IS NOT NULL"},
	} {
		st := compile(t, gr, gridraw.RowsRequest{
			Columns: []string{"email"},
			Filters: [][]gridraw.FilterClause{{{Field: "createdAt", Op: tc.op}}},
		})
		if !strings.Contains(st.RowsSQL, tc.want) {
			t.Errorf("%s: missing %q in:\n%s", tc.op, tc.want, st.RowsSQL)
		}
	}
}

func TestSQLDateAndStrictBounds(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{{
			{Field: "birthday", Op: gridraw.OpBetween, Value: []any{"2024-01-01", "2024-12-31"}},
			{Field: "rating", Op: gridraw.OpGt, Value: 3.0},
			{Field: "rating", Op: gridraw.OpNeq, Value: 5.0},
			{Field: "createdAt", Op: gridraw.OpLt, Value: "2025-01-01T00:00:00Z"},
			{Field: "birthday", Op: gridraw.OpNeq, Value: "2024-06-15"},
			{Field: "createdAt", Op: gridraw.OpNotBetween, Value: []any{"2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z"}},
			{Field: "rating", Op: gridraw.OpNotBetween, Value: []any{1.0, 2.0}},
			{Field: "birthday", Op: gridraw.OpNotBetween, Value: []any{"2020-01-01", "2020-12-31"}},
			{Field: "opensAt", Op: gridraw.OpGte, Value: "09:30"},
			{Field: "opensAt", Op: gridraw.OpNotBetween, Value: []any{"12:00", "13:00:30"}},
		}},
	})
	for _, frag := range []string{"birthday BETWEEN", "::date", "rating > $", "rating != $", "rating IS NULL", "created_at < $",
		"birthday != $", "birthday IS NULL", "created_at NOT BETWEEN", "created_at IS NULL",
		"rating NOT BETWEEN", "birthday NOT BETWEEN", "opens_at >= $", "::time", "opens_at NOT BETWEEN", "opens_at IS NULL"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
}

// A stepped column compiles to half-open buckets; the last bucket of the
// day ends at 24:00:00, which Postgres accepts for time.
func TestSQLSteppedTime(t *testing.T) {
	gr := testRegistry(t)
	st := compile(t, gr, gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{{
			{Field: "slot", Op: gridraw.OpEq, Value: "23:45"},
			{Field: "slot", Op: gridraw.OpNeq, Value: "09:00"},
		}},
	})
	for _, frag := range []string{"slot >= $", "slot < $", "NOT (", "slot IS NULL"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if strings.Contains(st.RowsSQL, "slot BETWEEN") || strings.Contains(st.RowsSQL, "slot <= $") {
		t.Errorf("stepped range must be half-open:\n%s", st.RowsSQL)
	}
	if args := fmt.Sprint(st.RowsArgs); !strings.Contains(args, "23:45:00 24:00:00") {
		t.Errorf("last bucket must end at 24:00:00: %v", st.RowsArgs)
	}
}

func TestSQLArrays(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email", "locales"},
		Search:  "go",
		Filters: [][]gridraw.FilterClause{{
			{Field: "tags", Op: gridraw.OpContainsAny, Value: []any{"go", "sql"}},
			{Field: "tags", Op: gridraw.OpNotContainsAny, Value: []any{"java"}},
			{Field: "locales", Op: gridraw.OpContainsAll, Value: []any{"en", "ru"}},
			{Field: "days", Op: gridraw.OpContainsAny, Value: []any{"2024-01-01"}},
			{Field: "tags", Op: gridraw.OpIsEmpty},
			{Field: "days", Op: gridraw.OpIsNotEmpty},
		}},
	})
	for _, frag := range []string{
		`users.locales::text[] AS "locales"`,
		"array_to_string(users.tags, $2::text) ILIKE $3::text",
		"users.tags && ($4::text[])",
		"NOT (users.tags && ($5::text[]))",
		"users.locales @> ($6::_locales[])",
		"users.days && ($7::date[])",
		"cardinality(users.tags) = $8",
		"cardinality(users.days) > $9",
	} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if n := strings.Count(st.RowsSQL, "users.tags IS NULL"); n != 2 {
		t.Errorf("notContainsAny and isEmpty must each keep NULL rows, got %d IS NULL checks:\n%s", n, st.RowsSQL)
	}
	if args := fmt.Sprint(st.RowsArgs); !strings.Contains(args, "[go sql]") || !strings.Contains(args, "[2024-01-01]") {
		t.Errorf("array args not bound as slices: %v", st.RowsArgs)
	}
}

// Every element type binds its own array type; PgType overrides it; a
// decimal array is projected as text[]; a stepped time array is not widened.
func TestSQLArrayElementTypes(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"amounts"},
		Filters: [][]gridraw.FilterClause{{
			{Field: "ids", Op: gridraw.OpContainsAny, Value: []any{"0F40E110-944F-46A3-8A9F-0A81221CBC41"}},
			{Field: "scores", Op: gridraw.OpContainsAll, Value: []any{1.0, 2.0}},
			{Field: "amounts", Op: gridraw.OpContainsAny, Value: []any{"4.10"}},
			{Field: "flags", Op: gridraw.OpContainsAny, Value: []any{true}},
			{Field: "seen", Op: gridraw.OpContainsAny, Value: []any{"2024-01-01T10:00:00Z"}},
			{Field: "slots", Op: gridraw.OpContainsAny, Value: []any{"09:15"}},
		}},
	})
	for _, frag := range []string{
		`users.amounts::text[] AS "amounts"`,
		"users.ids && ($1::uuid[])",
		"users.scores @> ($2::int4[])",
		"users.amounts && ($3::decimal[])",
		"users.flags && ($4::bool[])",
		"users.seen && ($5::timestamptz[])",
		"users.slots && ($6::time[])",
	} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if strings.Contains(st.RowsSQL, "slots >=") || strings.Contains(st.RowsSQL, "slots <") {
		t.Errorf("stepped array must not be widened:\n%s", st.RowsSQL)
	}
	args := fmt.Sprint(st.RowsArgs)
	for _, want := range []string{"[0f40e110-944f-46a3-8a9f-0a81221cbc41]", "[1 2]", "[4.10]", "[true]", "[09:15:00]"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %v missing %q", st.RowsArgs, want)
		}
	}
}

// decimal: the projection is text so the executor never floats it, the
// comparison binds ::decimal from the exact string.
func TestSQLDecimal(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"price"},
		Filters: [][]gridraw.FilterClause{{{Field: "price", Op: gridraw.OpGte, Value: "19.99"}}},
		Sort:    []gridraw.SortSpec{{Column: "price", Dir: "desc"}},
	})
	for _, frag := range []string{`price::text AS "price"`, "price >= $1::decimal", "ORDER BY users.price DESC"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if st.RowsArgs[0] != "19.99" {
		t.Errorf("arg = %#v, want the exact string", st.RowsArgs[0])
	}
}

// A Postgres enum column needs its parameters cast to the enum type.
func TestSQLPgEnumCast(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{{{Field: "locale", Op: gridraw.OpNotIn, Value: []any{"en", "ru"}}}},
	})
	if !strings.Contains(st.RowsSQL, "locale NOT IN ($1::text::_locales, $2::text::_locales)") {
		t.Errorf("enum params not cast:\n%s", st.RowsSQL)
	}
}

// uuid comparisons must bind ::uuid, never compare the column with text.
func TestSQLUUID(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{
			{{Field: "id", Op: gridraw.OpEq, Value: "0f40e110-944f-46a3-8a9f-0a81221cbc41"}},
			{{Field: "id", Op: gridraw.OpNotIn, Value: []any{"0f40e110-944f-46a3-8a9f-0a81221cbc41", "19958150-9528-4e73-9e6e-e3bf08c51d57"}}},
		},
	})
	for _, frag := range []string{"id = $1::uuid", "id IS NULL", "id NOT IN ($2::uuid, $3::uuid)"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if strings.Contains(st.RowsSQL, "ILIKE") || strings.Contains(st.RowsSQL, "::text") {
		t.Errorf("uuid must not go through text comparison:\n%s", st.RowsSQL)
	}
}

// notIn must keep NULL rows: plain NOT IN drops them because NULL NOT IN (...) is NULL.
func TestSQLNotInKeepsNull(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{{{Field: "role", Op: gridraw.OpNotIn, Value: []any{"admin", "user"}}}},
	})
	for _, frag := range []string{"IS NULL", "NOT IN"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	if !strings.Contains(fmt.Sprint(st.RowsArgs), "admin user") {
		t.Errorf("values not bound: %v", st.RowsArgs)
	}
}

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

// containsOnly is set equality: both containment directions on the same value.
func TestSQLContainsOnly(t *testing.T) {
	st := compile(t, testRegistry(t), gridraw.RowsRequest{
		Columns: []string{"email"},
		Filters: [][]gridraw.FilterClause{{
			{Field: "tags", Op: gridraw.OpContainsOnly, Value: []any{"go", "sql"}},
		}},
	})
	for _, frag := range []string{"users.tags @> ($1::text[])", "users.tags <@ ($2::text[])"} {
		if !strings.Contains(st.RowsSQL, frag) {
			t.Errorf("missing %q in:\n%s", frag, st.RowsSQL)
		}
	}
	// The value is bound once per direction: go-jet re-serializes the
	// expression instead of reusing the placeholder.
	if args := fmt.Sprint(st.RowsArgs); !strings.HasPrefix(args, "[[go sql] [go sql]") {
		t.Errorf("args = %v, want the value bound twice", st.RowsArgs)
	}
	if strings.Contains(st.RowsSQL, "users.tags IS NULL") {
		t.Errorf("containsOnly is a positive operator and must not match NULL:\n%s", st.RowsSQL)
	}
}
