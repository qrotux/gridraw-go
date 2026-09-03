// Package grjet compiles gridraw queries with go-jet's Postgres dialect.
// Bindings hold go-jet expressions; quick search and "contains"/"starts"
// filters use ILIKE, boolean filters use IS TRUE / IS NOT TRUE.
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

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func clauseExpr(c gridraw.Clause) postgres.BoolExpression {
	e, _ := filterExpr(c.Col) // checked by Validate
	switch c.Col.Type {
	case gridraw.TypeString, gridraw.TypeEnum:
		s := postgres.StringExp(e)
		switch c.Op {
		case gridraw.OpEq:
			return s.EQ(postgres.String(c.Value.(string)))
		case gridraw.OpContains:
			return ilike(s, "%"+escapeLike(c.Value.(string))+"%")
		case gridraw.OpStarts:
			return ilike(s, escapeLike(c.Value.(string))+"%")
		case gridraw.OpIn:
			vals := c.Value.([]string)
			exprs := make([]postgres.Expression, len(vals))
			for i, v := range vals {
				exprs[i] = postgres.String(v)
			}
			return s.IN(exprs...)
		}
	case gridraw.TypeNumber:
		f := postgres.FloatExp(e)
		switch c.Op {
		case gridraw.OpEq:
			return f.EQ(postgres.Float(c.Value.(float64)))
		case gridraw.OpGte:
			return f.GT_EQ(postgres.Float(c.Value.(float64)))
		case gridraw.OpLte:
			return f.LT_EQ(postgres.Float(c.Value.(float64)))
		case gridraw.OpBetween:
			return f.BETWEEN(postgres.Float(c.Value.(float64)), postgres.Float(c.Value2.(float64)))
		}
	case gridraw.TypeDatetime:
		ts := postgres.TimestampzExp(e)
		switch c.Op {
		case gridraw.OpGte:
			return ts.GT_EQ(postgres.TimestampzT(c.Value.(time.Time)))
		case gridraw.OpLte:
			return ts.LT_EQ(postgres.TimestampzT(c.Value.(time.Time)))
		case gridraw.OpBetween:
			return ts.BETWEEN(postgres.TimestampzT(c.Value.(time.Time)), postgres.TimestampzT(c.Value2.(time.Time)))
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
