// Package grjet compiles gridraw queries with go-jet's Postgres dialect.
// Bindings hold go-jet expressions; every string operator and the quick
// search use ILIKE, boolean filters use IS TRUE / IS NOT TRUE, negative
// operators keep NULL rows.
package grjet

import (
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
}

// GridBinding is the go-jet side of a gridraw.Grid.
type GridBinding struct {
	Base func() postgres.ReadableTable
}

// Compiler implements gridraw.Compiler.
type Compiler struct{}

var _ gridraw.Compiler = Compiler{}

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
	gb, ok := g.Binding.(GridBinding)
	return gb.Base, ok && gb.Base != nil
}

// Validate checks that every binding is a grjet type with the expressions its column needs.
func (Compiler) Validate(g *gridraw.Grid) error {
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
	}
	idc, _ := g.Column(g.IDColumn)
	if _, ok := sortExpr(idc); !ok {
		return fmt.Errorf("idColumn %q must be order-by-able (PK tiebreaker)", g.IDColumn)
	}
	return nil
}

// Compile renders the rows and count statements for q.
func (Compiler) Compile(q *gridraw.Query) (gridraw.Statements, error) {
	base, ok := baseOf(q.Grid)
	if !ok {
		return gridraw.Statements{}, fmt.Errorf("grid %q: missing base table", q.Grid.Name)
	}
	var st gridraw.Statements
	st.RowsSQL, st.RowsArgs = rowsSQL(q, base)
	st.CountSQL, st.CountArgs = countSQL(q, base)
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
		return orNull(e, postgres.NOT(in))
	}
	return orNull(e, col.NOT_BETWEEN(lo, hi))
}

// arrayExpr binds the whole value array as one parameter cast to the element
// array type, so && and @> can use a GIN index. Dates and times are bound
// as text elements to stay clear of the session time zone.
func arrayExpr(e postgres.Expression, c gridraw.Clause) postgres.BoolExpression {
	switch c.Op {
	case gridraw.OpIsEmpty:
		return orNull(e, cardinality(e).EQ(postgres.Int(0)))
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
	case gridraw.OpNotContainsAny:
		return orNull(e, postgres.NOT(postgres.BoolExp(postgres.CustomExpression(e, postgres.Token("&&"), param))))
	}
	panic("unreachable: op validated in BuildQuery")
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
		return orNull(e, f.NOT_EQ(v))
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
		return orNull(e, f.NOT_BETWEEN(v, v2))
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

// Negative operators keep NULL rows: in SQL "NULL <> x" is NULL and the
// row would vanish from both the positive and the negative filter.
func orNull(e postgres.Expression, cond postgres.BoolExpression) postgres.BoolExpression {
	return postgres.OR(e.IS_NULL(), cond)
}

func clauseExpr(c gridraw.Clause) postgres.BoolExpression {
	e, _ := filterExpr(c.Col) // checked by Validate
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
			return orNull(e, postgres.NOT(ilike(s, escapeLike(c.Value.(string)))))
		case gridraw.OpContains:
			return ilike(s, "%"+escapeLike(c.Value.(string))+"%")
		case gridraw.OpNotContains:
			return orNull(e, postgres.NOT(ilike(s, "%"+escapeLike(c.Value.(string))+"%")))
		case gridraw.OpStarts:
			return ilike(s, escapeLike(c.Value.(string))+"%")
		case gridraw.OpEnds:
			return ilike(s, "%"+escapeLike(c.Value.(string)))
		case gridraw.OpIn:
			return s.IN(lit(c.Value.([]string))...)
		case gridraw.OpNotIn:
			return orNull(e, s.NOT_IN(lit(c.Value.([]string))...))
		}
	case gridraw.TypeUUID:
		// Bound as $n::uuid: comparing a uuid column with a text parameter
		// is a type error in Postgres.
		s := postgres.StringExp(e)
		switch c.Op {
		case gridraw.OpEq:
			return s.EQ(uuidLit(c.Value.(string)))
		case gridraw.OpNeq:
			return orNull(e, s.NOT_EQ(uuidLit(c.Value.(string))))
		case gridraw.OpIn:
			return s.IN(uuidExprs(c.Value.([]string))...)
		case gridraw.OpNotIn:
			return orNull(e, s.NOT_IN(uuidExprs(c.Value.([]string))...))
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
			return orNull(e, d.NOT_EQ(v))
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
			return orNull(e, d.NOT_BETWEEN(v, postgres.DateT(c.Value2.(time.Time))))
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
			return orNull(e, tm.NOT_EQ(v))
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
			return orNull(e, ts.NOT_EQ(v))
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
	panic("unreachable: op validated in BuildQuery")
}

func whereExpr(q *gridraw.Query) postgres.BoolExpression {
	var parts []postgres.BoolExpression

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

func rowsSQL(q *gridraw.Query, base func() postgres.ReadableTable) (string, []any) {
	projections := make([]postgres.Projection, len(q.Cols))
	for i, c := range q.Cols {
		b, _ := bindingOf(c)
		projections[i] = b.Projection
	}
	stmt := postgres.SELECT(projections[0], projections[1:]...).FROM(base())
	if w := whereExpr(q); w != nil {
		stmt = stmt.WHERE(w)
	}
	stmt = stmt.ORDER_BY(orderBy(q)...).
		LIMIT(int64(q.PageSize)).
		OFFSET(int64((q.Page - 1) * q.PageSize))
	return stmt.Sql()
}

func countSQL(q *gridraw.Query, base func() postgres.ReadableTable) (string, []any) {
	stmt := postgres.SELECT(postgres.COUNT(postgres.STAR)).FROM(base())
	if w := whereExpr(q); w != nil {
		stmt = stmt.WHERE(w)
	}
	return stmt.Sql()
}
