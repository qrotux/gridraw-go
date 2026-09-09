// Package grjet compiles gridraw queries with go-jet's Postgres dialect.
// Bindings hold go-jet expressions; every string operator and the quick
// search use ILIKE, boolean filters use IS TRUE / IS NOT TRUE, negative
// operators keep NULL rows.
package grjet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-jet/jet/v2/postgres"

	"github.com/qrotux/gridraw-go"
)

// Binding is the go-jet side of a gridraw.Column. Projection is what the
// rows query selects; Filter and Sort default to Projection when it is an
// Expression and must be set explicitly when it is an alias (AS), because
// Postgres rejects aliases in WHERE and the sort should target the expression.
type Binding struct {
	Projection postgres.Projection
	Filter     postgres.Expression
	Sort       postgres.Expression
	// ParamType, when set, names the SQL type in/notIn and array parameters
	// are cast to. Required for a Postgres enum column ("enum = text" has no
	// operator) and for an array column whose SQL element type is not the
	// default of its grid type (integer[] for number, which binds float8[]).
	ParamType string
	// Compile, when set, renders the clauses of Column.Custom and may take
	// over a built-in operator of the same name; ok=false falls back to the
	// built-in rendering. It is called only for clauses that went through
	// CustomOps.Parse, so isNull/isNotNull and a stepped eq widened to
	// between never reach it. It receives Filter (or Projection) already
	// resolved. NULL handling is the hook's own: wrap a negative predicate
	// in OrNull to keep NULL rows like the built-in negative operators do.
	Compile func(filter postgres.Expression, c gridraw.Clause) (postgres.BoolExpression, bool)
}

// GridBinding is the go-jet side of a gridraw.Grid.
type GridBinding struct {
	Base func() postgres.ReadableTable
}

// ScopedBinding combines a base table with a mandatory per-request row predicate.
type ScopedBinding struct {
	GridBinding
	Scope func(context.Context) (postgres.BoolExpression, error)
}

// WithScope restricts rows and counts; a nil callback or nil predicate is an error.
func (b GridBinding) WithScope(scope func(context.Context) (postgres.BoolExpression, error)) ScopedBinding {
	return ScopedBinding{GridBinding: b, Scope: scope}
}

// WithScope adds another mandatory predicate by AND, preserving every preceding scope.
func (b ScopedBinding) WithScope(scope func(context.Context) (postgres.BoolExpression, error)) ScopedBinding {
	previous := b.Scope
	if previous == nil || scope == nil {
		b.Scope = nil
		return b
	}
	b.Scope = func(ctx context.Context) (postgres.BoolExpression, error) {
		first, err := previous(ctx)
		if err != nil {
			return nil, err
		}
		if first == nil {
			return nil, fmt.Errorf("scope returned nil predicate")
		}
		second, err := scope(ctx)
		if err != nil {
			return nil, err
		}
		if second == nil {
			return nil, fmt.Errorf("scope returned nil predicate")
		}
		return postgres.AND(first, second), nil
	}
	return b
}

// Compiler implements gridraw.Compiler.
type Compiler struct{}

var _ gridraw.Compiler = Compiler{}
var _ gridraw.ContextCompiler = Compiler{}

func exprOf(p postgres.Projection) (postgres.Expression, bool) {
	e, ok := p.(postgres.Expression)
	return e, ok
}

func bindingOf(c gridraw.Column) (Binding, bool) {
	b, ok := c.Binding.(Binding)
	return b, ok && b.Projection != nil
}

func sortExpr(c gridraw.Column) (postgres.Expression, bool) {
	b, ok := bindingOf(c)
	if !ok {
		return nil, false
	}
	if b.Sort != nil {
		return b.Sort, true
	}
	return exprOf(b.Projection)
}

func filterExpr(c gridraw.Column) (postgres.Expression, bool) {
	b, ok := bindingOf(c)
	if !ok {
		return nil, false
	}
	if b.Filter != nil {
		return b.Filter, true
	}
	return exprOf(b.Projection)
}

func baseOf(g *gridraw.Grid) (func() postgres.ReadableTable, bool) {
	if sb, ok := g.Binding.(ScopedBinding); ok {
		return sb.Base, sb.Base != nil
	}
	gb, ok := g.Binding.(GridBinding)
	return gb.Base, ok && gb.Base != nil
}

// Validate checks that every binding is a grjet type with the expressions its column needs.
func (Compiler) Validate(g *gridraw.Grid) error {
	if sb, ok := g.Binding.(ScopedBinding); ok && sb.Scope == nil {
		return fmt.Errorf("scoped binding requires Scope")
	}
	if _, ok := baseOf(g); !ok {
		return fmt.Errorf("grid binding must be grjet.GridBinding with Base")
	}
	for _, c := range g.Columns {
		if _, ok := bindingOf(c); !ok {
			return fmt.Errorf("column %q: binding must be grjet.Binding with Projection", c.Key)
		}
		if c.Sortable {
			if _, ok := sortExpr(c); !ok {
				return fmt.Errorf("column %q: sortable without sort expression", c.Key)
			}
		}
		if c.Filter != nil {
			if _, ok := filterExpr(c); !ok {
				return fmt.Errorf("column %q: filter without expression", c.Key)
			}
		}
		if c.Searchable {
			if _, ok := filterExpr(c); !ok {
				return fmt.Errorf("column %q: searchable without expression", c.Key)
			}
		}
		if c.Custom != nil {
			if b, _ := bindingOf(c); b.Compile == nil {
				return fmt.Errorf("column %q: custom operators without Compile", c.Key)
			}
		}
	}
	idc, _ := g.Column(g.IDColumn)
	if _, ok := sortExpr(idc); !ok {
		return fmt.Errorf("idColumn %q must be order-by-able (PK tiebreaker)", g.IDColumn)
	}
	return nil
}

// Compile renders the rows and count statements for q.
func (Compiler) Compile(q *gridraw.Query) (gridraw.Statements, error) {
	if _, ok := q.Grid.Binding.(ScopedBinding); ok {
		return gridraw.Statements{}, fmt.Errorf("scoped binding requires CompileContext")
	}
	return compileQuery(q, nil)
}

// CompileContext resolves the scope once and applies it to both rows and count statements.
func (Compiler) CompileContext(ctx context.Context, q *gridraw.Query) (gridraw.Statements, error) {
	if err := ctx.Err(); err != nil {
		return gridraw.Statements{}, err
	}
	var scope postgres.BoolExpression
	if sb, ok := q.Grid.Binding.(ScopedBinding); ok {
		if sb.Scope == nil {
			return gridraw.Statements{}, fmt.Errorf("scoped binding requires Scope")
		}
		var err error
		scope, err = sb.Scope(ctx)
		if err != nil {
			return gridraw.Statements{}, fmt.Errorf("grid %q scope: %w", q.Grid.Name, err)
		}
		if scope == nil {
			return gridraw.Statements{}, fmt.Errorf("grid %q scope returned nil predicate", q.Grid.Name)
		}
	}
	return compileQuery(q, scope)
}

func compileQuery(q *gridraw.Query, scope postgres.BoolExpression) (gridraw.Statements, error) {
	base, ok := baseOf(q.Grid)
	if !ok {
		return gridraw.Statements{}, fmt.Errorf("grid %q: missing base table", q.Grid.Name)
	}
	var st gridraw.Statements
	st.RowsSQL, st.RowsArgs = rowsSQL(q, base, scope)
	st.CountSQL, st.CountArgs = countSQL(q, base, scope)
	return st, nil
}

func ilike(lhs postgres.StringExpression, pattern string) postgres.BoolExpression {
	return postgres.BoolExp(postgres.CustomExpression(lhs, postgres.Token("ILIKE"), postgres.String(pattern)))
}

// clock renders a time-of-day literal. A stepped upper bound can land on
// the next day's midnight, which Postgres time spells as 24:00:00.
func clock(t time.Time) postgres.TimeExpression {
	if t.Day() != 1 {
		return postgres.Time(24, 0, 0)
	}
	return postgres.Time(t.Hour(), t.Minute(), t.Second())
}

// ordered is the go-jet comparison surface shared by time and timestamptz
// expressions, enough to spell a range with either bound convention.
type ordered[T postgres.Expression] interface {
	GT_EQ(T) postgres.BoolExpression
	LT(T) postgres.BoolExpression
	BETWEEN(T, T) postgres.BoolExpression
	NOT_BETWEEN(T, T) postgres.BoolExpression
}

// rangeExpr keeps BETWEEN for closed ranges and spells the half-open
// [lo, hi) of stepped columns as >= AND <.
func rangeExpr[T postgres.Expression](e postgres.Expression, col ordered[T], lo, hi T, upperOpen, negate bool) postgres.BoolExpression {
	var in postgres.BoolExpression
	if upperOpen {
		in = postgres.AND(col.GT_EQ(lo), col.LT(hi))
	} else {
		in = col.BETWEEN(lo, hi)
	}
	if !negate {
		return in
	}
	if upperOpen {
		return OrNull(e, postgres.NOT(in))
	}
	return OrNull(e, col.NOT_BETWEEN(lo, hi))
}

// arrayExpr binds the whole value array as one parameter cast to the element
// array type, so && and @> can use a GIN index. Dates and times are bound
// as text elements to stay clear of the session time zone.
func arrayExpr(e postgres.Expression, c gridraw.Clause) postgres.BoolExpression {
	switch c.Op {
	case gridraw.OpIsEmpty:
		return OrNull(e, cardinality(e).EQ(postgres.Int(0)))
	case gridraw.OpIsNotEmpty:
		return cardinality(e).GT(postgres.Int(0))
	}
	b, _ := bindingOf(c.Col)
	param := arrayParam(c.Col.Type, c.Value, b.ParamType)
	switch c.Op {
	case gridraw.OpContainsAny:
		return postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("&&"), param))
	case gridraw.OpContainsAll:
		return postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("@>"), param))
	case gridraw.OpContainsOnly:
		// Mutual containment is set equality: order and duplicates do not
		// matter, and the @> half still uses a GIN index.
		return postgres.AND(
			postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("@>"), param)),
			postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("<@"), param)),
		)
	case gridraw.OpNotContainsAny:
		return OrNull(e, postgres.NOT(postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("&&"), param))))
	}
	panic(fmt.Sprintf("grjet: no rendering for op %q on column %q (a custom operator needs Binding.Compile)", c.Op, c.Col.Key))
}

func cardinality(e postgres.Expression) postgres.IntegerExpression {
	return postgres.IntExp(postgres.Func("cardinality", e))
}

func arrayParam(elem gridraw.ColType, value any, paramType string) postgres.Expression {
	var vals any = value
	sqlType := "text"
	switch elem {
	case gridraw.TypeUUID:
		sqlType = "uuid"
	case gridraw.TypeNumber:
		sqlType = "float8"
	case gridraw.TypeDecimal:
		sqlType = "decimal"
	case gridraw.TypeBool:
		sqlType = "bool"
	case gridraw.TypeDate:
		sqlType, vals = "date", formatAll(value.([]time.Time), time.DateOnly)
	case gridraw.TypeTime:
		sqlType, vals = "time", formatAll(value.([]time.Time), time.TimeOnly)
	case gridraw.TypeDatetime:
		sqlType = "timestamptz"
	}
	if paramType != "" {
		sqlType = paramType
	}
	return postgres.Raw("#v::"+sqlType+"[]", postgres.RawArgs{"#v": vals})
}

func formatAll(ts []time.Time, layout string) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format(layout)
	}
	return out
}

func numberExpr(e postgres.Expression, op gridraw.Op, v, v2 postgres.FloatExpression) postgres.BoolExpression {
	f := postgres.FloatExp(e)
	switch op {
	case gridraw.OpEq:
		return f.EQ(v)
	case gridraw.OpNeq:
		return OrNull(e, f.NOT_EQ(v))
	case gridraw.OpGt:
		return f.GT(v)
	case gridraw.OpGte:
		return f.GT_EQ(v)
	case gridraw.OpLt:
		return f.LT(v)
	case gridraw.OpLte:
		return f.LT_EQ(v)
	case gridraw.OpBetween:
		return f.BETWEEN(v, v2)
	case gridraw.OpNotBetween:
		return OrNull(e, f.NOT_BETWEEN(v, v2))
	}
	panic("unreachable: op validated in BuildQuery")
}

type uuidString string

func (u uuidString) String() string { return string(u) }

func uuidLit(s string) postgres.StringExpression { return postgres.UUID(uuidString(s)) }

func uuidExprs(vals []string) []postgres.Expression {
	exprs := make([]postgres.Expression, len(vals))
	for i, v := range vals {
		exprs[i] = uuidLit(v)
	}
	return exprs
}

func stringExprs(vals []string, paramType string) []postgres.Expression {
	exprs := make([]postgres.Expression, len(vals))
	for i, v := range vals {
		if paramType != "" {
			exprs[i] = postgres.CAST(postgres.String(v)).AS(paramType)
		} else {
			exprs[i] = postgres.String(v)
		}
	}
	return exprs
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// OrNull widens a negative predicate to NULL rows: in SQL "NULL <> x" is
// NULL and the row would vanish from both the positive and the negative
// filter. Every built-in negative operator goes through it.
func OrNull(e postgres.Expression, cond postgres.BoolExpression) postgres.BoolExpression {
	return postgres.OR(e.IS_NULL(), cond)
}

func clauseExpr(c gridraw.Clause) postgres.BoolExpression {
	e, _ := filterExpr(c.Col) // checked by Validate
	if c.Custom {
		// Only a custom-parsed clause reaches Compile; a reserved null
		// operator or a built-in one widened by applyStep never does.
		if b, ok := bindingOf(c.Col); ok && b.Compile != nil {
			if expr, ok := b.Compile(e, c); ok {
				return expr
			}
		}
	}
	switch c.Op {
	case gridraw.OpIsNull:
		return e.IS_NULL()
	case gridraw.OpIsNotNull:
		return e.IS_NOT_NULL()
	}
	if c.Col.Array {
		return arrayExpr(e, c)
	}
	switch c.Col.Type {
	case gridraw.TypeString, gridraw.TypeEnum:
		s := postgres.StringExp(e)
		b, _ := bindingOf(c.Col)
		lit := func(vals []string) []postgres.Expression { return stringExprs(vals, b.ParamType) }
		switch c.Op {
		case gridraw.OpEq:
			// ILIKE on the escaped literal: case-insensitive like the other
			// string operators, without wildcards.
			return ilike(s, escapeLike(c.Value.(string)))
		case gridraw.OpNeq:
			return OrNull(e, postgres.NOT(ilike(s, escapeLike(c.Value.(string)))))
		case gridraw.OpContains:
			return ilike(s, "%"+escapeLike(c.Value.(string))+"%")
		case gridraw.OpNotContains:
			return OrNull(e, postgres.NOT(ilike(s, "%"+escapeLike(c.Value.(string))+"%")))
		case gridraw.OpStarts:
			return ilike(s, escapeLike(c.Value.(string))+"%")
		case gridraw.OpEnds:
			return ilike(s, "%"+escapeLike(c.Value.(string)))
		case gridraw.OpIn:
			return s.IN(lit(c.Value.([]string))...)
		case gridraw.OpNotIn:
			return OrNull(e, s.NOT_IN(lit(c.Value.([]string))...))
		}
	case gridraw.TypeUUID:
		// Bound as $n::uuid: comparing a uuid column with a text parameter
		// is a type error in Postgres.
		s := postgres.StringExp(e)
		switch c.Op {
		case gridraw.OpEq:
			return s.EQ(uuidLit(c.Value.(string)))
		case gridraw.OpNeq:
			return OrNull(e, s.NOT_EQ(uuidLit(c.Value.(string))))
		case gridraw.OpIn:
			return s.IN(uuidExprs(c.Value.([]string))...)
		case gridraw.OpNotIn:
			return OrNull(e, s.NOT_IN(uuidExprs(c.Value.([]string))...))
		}
	case gridraw.TypeNumber:
		v2, _ := c.Value2.(float64)
		return numberExpr(e, c.Op, postgres.Float(c.Value.(float64)), postgres.Float(v2))
	case gridraw.TypeDecimal:
		// Bound as $n::decimal from the exact string; never through float64.
		v2, _ := c.Value2.(string)
		return numberExpr(e, c.Op, postgres.Decimal(c.Value.(string)), postgres.Decimal(v2))
	case gridraw.TypeDate:
		d := postgres.DateExp(e)
		v := postgres.DateT(c.Value.(time.Time))
		switch c.Op {
		case gridraw.OpEq:
			return d.EQ(v)
		case gridraw.OpNeq:
			return OrNull(e, d.NOT_EQ(v))
		case gridraw.OpGt:
			return d.GT(v)
		case gridraw.OpGte:
			return d.GT_EQ(v)
		case gridraw.OpLt:
			return d.LT(v)
		case gridraw.OpLte:
			return d.LT_EQ(v)
		case gridraw.OpBetween:
			return d.BETWEEN(v, postgres.DateT(c.Value2.(time.Time)))
		case gridraw.OpNotBetween:
			return OrNull(e, d.NOT_BETWEEN(v, postgres.DateT(c.Value2.(time.Time))))
		}
	case gridraw.TypeTime:
		// Bound as a "HH:MM:SS" text literal cast to time, not as time.Time:
		// a timestamp parameter cast to time would pass through the session
		// time zone.
		tm := postgres.TimeExp(e)
		v := clock(c.Value.(time.Time))
		switch c.Op {
		case gridraw.OpEq:
			return tm.EQ(v)
		case gridraw.OpNeq:
			return OrNull(e, tm.NOT_EQ(v))
		case gridraw.OpGt:
			return tm.GT(v)
		case gridraw.OpGte:
			return tm.GT_EQ(v)
		case gridraw.OpLt:
			return tm.LT(v)
		case gridraw.OpLte:
			return tm.LT_EQ(v)
		case gridraw.OpBetween:
			return rangeExpr(e, tm, v, clock(c.Value2.(time.Time)), c.UpperOpen, false)
		case gridraw.OpNotBetween:
			return rangeExpr(e, tm, v, clock(c.Value2.(time.Time)), c.UpperOpen, true)
		}
	case gridraw.TypeDatetime:
		ts := postgres.TimestampzExp(e)
		v := postgres.TimestampzT(c.Value.(time.Time))
		switch c.Op {
		case gridraw.OpEq:
			return ts.EQ(v)
		case gridraw.OpNeq:
			return OrNull(e, ts.NOT_EQ(v))
		case gridraw.OpGt:
			return ts.GT(v)
		case gridraw.OpGte:
			return ts.GT_EQ(v)
		case gridraw.OpLt:
			return ts.LT(v)
		case gridraw.OpLte:
			return ts.LT_EQ(v)
		case gridraw.OpBetween:
			return rangeExpr(e, ts, v, postgres.TimestampzT(c.Value2.(time.Time)), c.UpperOpen, false)
		case gridraw.OpNotBetween:
			return rangeExpr(e, ts, v, postgres.TimestampzT(c.Value2.(time.Time)), c.UpperOpen, true)
		}
	case gridraw.TypeBool:
		// IS TRUE / IS NOT TRUE so that NULL lands in the "false" bucket
		// instead of vanishing from both.
		b := postgres.BoolExp(e)
		if c.Value.(bool) {
			return b.IS_TRUE()
		}
		return b.IS_NOT_TRUE()
	}
	panic(fmt.Sprintf("grjet: no rendering for op %q on column %q (a custom operator needs Binding.Compile)", c.Op, c.Col.Key))
}

func whereExpr(q *gridraw.Query, scope postgres.BoolExpression) postgres.BoolExpression {
	var parts []postgres.BoolExpression
	if scope != nil {
		parts = append(parts, scope)
	}

	if q.Search != "" {
		var ors []postgres.BoolExpression
		pattern := "%" + escapeLike(q.Search) + "%"
		for _, c := range q.Grid.Columns {
			if !c.Searchable {
				continue
			}
			e, ok := filterExpr(c)
			if !ok {
				continue
			}
			if c.Array {
				e = postgres.Func("array_to_string", e, postgres.String(" "))
			}
			ors = append(ors, ilike(postgres.StringExp(e), pattern))
		}
		if len(ors) > 0 {
			parts = append(parts, postgres.OR(ors...))
		}
	}

	if len(q.Groups) > 0 {
		var groups []postgres.BoolExpression
		for _, g := range q.Groups {
			var ands []postgres.BoolExpression
			for _, c := range g {
				ands = append(ands, clauseExpr(c))
			}
			groups = append(groups, postgres.AND(ands...))
		}
		parts = append(parts, postgres.OR(groups...))
	}

	if len(parts) == 0 {
		return nil
	}
	return postgres.AND(parts...)
}

func orderBy(q *gridraw.Query) []postgres.OrderByClause {
	var out []postgres.OrderByClause
	hasID := false
	for _, t := range q.Sorts {
		se, _ := sortExpr(t.Col)
		if t.Spec.Dir == "desc" {
			out = append(out, se.DESC().NULLS_LAST())
		} else {
			out = append(out, se.ASC().NULLS_LAST())
		}
		if t.Spec.Column == q.Grid.IDColumn {
			hasID = true
		}
	}
	if !hasID {
		idc, _ := q.Grid.Column(q.Grid.IDColumn)
		ide, _ := sortExpr(idc)
		out = append(out, ide.ASC())
	}
	return out
}

func rowsSQL(q *gridraw.Query, base func() postgres.ReadableTable, scope postgres.BoolExpression) (string, []any) {
	projections := make([]postgres.Projection, len(q.Cols))
	for i, c := range q.Cols {
		b, _ := bindingOf(c)
		projections[i] = b.Projection
	}
	stmt := postgres.SELECT(projections[0], projections[1:]...).FROM(base())
	if w := whereExpr(q, scope); w != nil {
		stmt = stmt.WHERE(w)
	}
	stmt = stmt.ORDER_BY(orderBy(q)...).
		LIMIT(int64(q.RowLimit())).
		OFFSET(int64((q.Page - 1) * q.PageSize))
	return stmt.Sql()
}

func countSQL(q *gridraw.Query, base func() postgres.ReadableTable, scope postgres.BoolExpression) (string, []any) {
	stmt := postgres.SELECT(postgres.COUNT(postgres.STAR)).FROM(base())
	if w := whereExpr(q, scope); w != nil {
		stmt = stmt.WHERE(w)
	}
	return stmt.Sql()
}
