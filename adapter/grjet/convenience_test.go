package grjet

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/qrotux/gridraw-go"
)

func TestExpressionColumns(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns,
		BoolExpr("active", postgres.COALESCE(colIsBanned, postgres.Bool(false))).Vis(),
		StrExpr("label", postgres.COALESCE(colEmail, postgres.String("unknown"))),
		DecimalExpr("amount", postgres.COALESCE(colPrice, postgres.Float(0))),
	)
	if _, err := gridraw.NewRegistry(Compiler{}, g); err != nil {
		t.Fatal(err)
	}
	st := compile(t, &g, gridraw.RowsRequest{Columns: []string{"active", "label", "amount"}, Filters: [][]gridraw.FilterClause{{{Field: "amount", Op: gridraw.OpGte, Value: "12.50"}}}})
	for _, fragment := range []string{"COALESCE", "::text", `AS "amount"`, ">="} {
		if !strings.Contains(st.RowsSQL, fragment) {
			t.Fatalf("missing %q: %s", fragment, st.RowsSQL)
		}
	}
}

func TestWithFilterExprPreservesBinding(t *testing.T) {
	c := PgType("numeric", DecimalCol("price", colPrice))
	g := validTestGrid()
	g.Columns[8] = WithFilterExpr(c, postgres.COALESCE(colPrice, postgres.Float(0)))
	st := compile(t, &g, gridraw.RowsRequest{Columns: []string{"price"}, Sort: []gridraw.SortSpec{{Column: "price", Dir: "asc"}}, Filters: [][]gridraw.FilterClause{{{Field: "price", Op: gridraw.OpGte, Value: "1.00"}}}})
	if !strings.Contains(st.RowsSQL, `users.price::text AS "price"`) || !strings.Contains(st.RowsSQL, "COALESCE(users.price") || !strings.Contains(st.RowsSQL, "users.price ASC") {
		t.Fatal(st.RowsSQL)
	}
	b := g.Columns[8].Binding.(Binding)
	if b.ParamType != "numeric" || c.Binding.(Binding).Filter != colPrice {
		t.Fatal("binding metadata changed")
	}
}

type scopeKey struct{}

func TestScopeAppliesToBothStatements(t *testing.T) {
	g := validTestGrid()
	calls := 0
	g.Binding = Base(func() postgres.ReadableTable { return users }).WithScope(func(ctx context.Context) (postgres.BoolExpression, error) {
		calls++
		return colRole.EQ(postgres.String(ctx.Value(scopeKey{}).(string))), nil
	})
	if _, err := gridraw.NewRegistry(Compiler{}, g); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("scope ran at registration")
	}
	q, err := gridraw.BuildQuery(&g, gridraw.RowsRequest{Search: "test", Filters: [][]gridraw.FilterClause{{{Field: "email", Op: gridraw.OpEq, Value: "a"}}, {{Field: "email", Op: gridraw.OpEq, Value: "b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		st, err := (Compiler{}).CompileContext(context.WithValue(context.Background(), scopeKey{}, tenant), q)
		if err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{st.RowsSQL, st.CountSQL} {
			where := "WHERE ( (users.role = $1::text) AND ( (users.email ILIKE $2::text) OR (array_to_string(users.tags, $3::text) ILIKE $4::text) ) AND ( (users.email ILIKE $5::text) OR (users.email ILIKE $6::text) ) )"
			if !strings.Contains(strings.Join(strings.Fields(sql), " "), where) {
				t.Fatalf("scope must enclose both OR groups: %s", sql)
			}
		}
		if len(st.RowsArgs) != len(st.CountArgs)+2 || !reflect.DeepEqual(st.RowsArgs[:len(st.CountArgs)], st.CountArgs) || st.RowsArgs[0] != tenant {
			t.Fatalf("scope args: %#v / %#v", st.RowsArgs, st.CountArgs)
		}
	}
	if calls != 2 {
		t.Fatalf("scope called %d times", calls)
	}
	if _, err := (Compiler{}).Compile(q); err == nil {
		t.Fatal("context-free compilation bypassed scope")
	}
}

func TestScopeFailsClosed(t *testing.T) {
	denied := errors.New("tenant missing")
	for _, tc := range []struct {
		name  string
		scope func(context.Context) (postgres.BoolExpression, error)
	}{
		{"error", func(context.Context) (postgres.BoolExpression, error) { return nil, denied }},
		{"nil predicate", func(context.Context) (postgres.BoolExpression, error) { return nil, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := validTestGrid()
			g.Binding = Base(func() postgres.ReadableTable { t.Fatal("base evaluated after scope failure"); return users }).WithScope(tc.scope)
			q, err := gridraw.BuildQuery(&g, gridraw.RowsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			st, compileErr := (Compiler{}).CompileContext(context.Background(), q)
			if compileErr == nil || st.RowsSQL != "" || st.CountSQL != "" {
				t.Fatalf("scope did not fail closed: %+v %v", st, compileErr)
			}
			if tc.name == "error" && !errors.Is(compileErr, denied) {
				t.Fatal(compileErr)
			}
		})
	}
	g := validTestGrid()
	g.Binding = Base(func() postgres.ReadableTable { return users }).WithScope(nil)
	if _, err := gridraw.NewRegistry(Compiler{}, g); err == nil {
		t.Fatal("nil scope accepted")
	}
}

func TestChainedScopesKeepBothRestrictions(t *testing.T) {
	g := validTestGrid()
	firstCalls, secondCalls := 0, 0
	first := func(context.Context) (postgres.BoolExpression, error) {
		firstCalls++
		return colRole.EQ(postgres.String("tenant-a")), nil
	}
	second := func(context.Context) (postgres.BoolExpression, error) {
		secondCalls++
		return colIsBanned.IS_FALSE(), nil
	}
	g.Binding = Base(func() postgres.ReadableTable { return users }).WithScope(first).WithScope(second)
	q, err := gridraw.BuildQuery(&g, gridraw.RowsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	st, compileErr := (Compiler{}).CompileContext(context.Background(), q)
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	for _, sql := range []string{st.RowsSQL, st.CountSQL} {
		if !strings.Contains(sql, "users.role = $1::text") || !strings.Contains(sql, "users.is_banned IS FALSE") || !strings.Contains(sql, "AND") {
			t.Fatal(sql)
		}
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("scope calls: %d / %d", firstCalls, secondCalls)
	}
	for _, binding := range []ScopedBinding{
		Base(func() postgres.ReadableTable { return users }).WithScope(nil).WithScope(second),
		Base(func() postgres.ReadableTable { return users }).WithScope(first).WithScope(nil),
	} {
		g.Binding = binding
		if _, err := gridraw.NewRegistry(Compiler{}, g); err == nil {
			t.Fatal("nil chained scope accepted")
		}
	}
}

func TestScopeContextCancellationAndRuntimeNil(t *testing.T) {
	g := validTestGrid()
	g.Binding = Base(func() postgres.ReadableTable { t.Fatal("base called"); return users }).WithScope(func(context.Context) (postgres.BoolExpression, error) {
		t.Fatal("scope called after cancellation")
		return nil, nil
	})
	q, err := gridraw.BuildQuery(&g, gridraw.RowsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Compiler{}).CompileContext(ctx, q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	g.Binding = Base(func() postgres.ReadableTable { t.Fatal("base called"); return users }).WithScope(nil)
	if _, err := (Compiler{}).CompileContext(context.Background(), q); err == nil {
		t.Fatal("runtime nil scope accepted")
	}
}

func TestWithFilterExprPreservesCustomRenderer(t *testing.T) {
	c := StrCol("email", colEmail)
	c.Custom = &gridraw.CustomOps{Operators: []gridraw.Op{gridraw.OpEq}, Parse: func(_ gridraw.Op, raw any) (any, any, error) {
		return raw, nil, nil
	}}
	b := c.Binding.(Binding)
	b.Compile = func(filter postgres.Expression, clause gridraw.Clause) (postgres.BoolExpression, bool) {
		return postgres.StringExp(filter).EQ(postgres.String(clause.Value.(string))), true
	}
	c = WithFilterExpr(Bind(c, b), postgres.LOWER(colEmail))
	g := validTestGrid()
	g.Columns[1] = c
	if _, err := gridraw.NewRegistry(Compiler{}, g); err != nil {
		t.Fatal(err)
	}
	st := compile(t, &g, gridraw.RowsRequest{Filters: [][]gridraw.FilterClause{{{Field: "email", Op: gridraw.OpEq, Value: "a"}}}})
	if !strings.Contains(st.RowsSQL, "LOWER(users.email) = $1::text") || strings.Contains(st.RowsSQL, "ILIKE") {
		t.Fatal(st.RowsSQL)
	}
}

func TestChainedScopeFailuresStopCompilation(t *testing.T) {
	denied := errors.New("denied")
	for _, failFirst := range []bool{true, false} {
		for _, scopeErr := range []error{nil, denied} {
			g := validTestGrid()
			calls := 0
			failure := func(context.Context) (postgres.BoolExpression, error) { return nil, scopeErr }
			success := func(context.Context) (postgres.BoolExpression, error) { calls++; return postgres.Bool(true), nil }
			binding := Base(func() postgres.ReadableTable { t.Fatal("base called on failed scope"); return users })
			if failFirst {
				g.Binding = binding.WithScope(failure).WithScope(success)
			} else {
				g.Binding = binding.WithScope(success).WithScope(failure)
			}
			q, err := gridraw.BuildQuery(&g, gridraw.RowsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			st, compileErr := (Compiler{}).CompileContext(context.Background(), q)
			if compileErr == nil || st.RowsSQL != "" || (scopeErr != nil && !errors.Is(compileErr, denied)) {
				t.Fatalf("scope failure: %+v %v", st, compileErr)
			}
			if (failFirst && calls != 0) || (!failFirst && calls != 1) {
				t.Fatalf("success calls=%d, failFirst=%v", calls, failFirst)
			}
		}
	}
}
