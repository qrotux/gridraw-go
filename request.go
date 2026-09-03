package gridraw

import (
	"fmt"
	"time"
)

const (
	maxGroups      = 10
	maxClauses     = 20
	maxPageSize    = 100
	maxSortColumns = 16
)

// FilterClause is one predicate of a rows request.
type FilterClause struct {
	Field string `json:"field"`
	Op    Op     `json:"op"`
	Value any    `json:"value"`
}

// RowsRequest is the body of the rows endpoint. Filters is a DNF: OR of AND
// groups. Sort priority is slice order; empty falls back to the grid default.
type RowsRequest struct {
	Columns  []string         `json:"columns"`
	Filters  [][]FilterClause `json:"filters"`
	Search   string           `json:"search"`
	Sort     []SortSpec       `json:"sort"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// ReqError is a client error with the HTTP status to answer with.
type ReqError struct {
	Status int
	Msg    string
}

func (e *ReqError) Error() string { return e.Msg }

func badReq(format string, a ...any) *ReqError {
	return &ReqError{Status: 400, Msg: fmt.Sprintf(format, a...)}
}

// Clause is a validated, typed predicate. Value2 is the upper bound of between.
type Clause struct {
	Col    Column
	Op     Op
	Value  any
	Value2 any
}

// SortTerm is a validated sort term.
type SortTerm struct {
	Spec SortSpec
	Col  Column
}

// Query is a validated rows request ready for a Compiler. Cols is the
// response order and contains IDColumn exactly once; Keys mirrors it. Sorts
// carry no PK tiebreaker: the Compiler appends one.
type Query struct {
	Grid     *Grid
	Cols     []Column
	Keys     []string
	Groups   [][]Clause
	Search   string
	Sorts    []SortTerm
	Page     int
	PageSize int
}

// BuildQuery validates a request against a grid.
func BuildQuery(g *Grid, req RowsRequest) (*Query, *ReqError) {
	q := &Query{Grid: g, Search: req.Search}

	seen := map[string]bool{}
	for _, key := range req.Columns {
		c, ok := g.Column(key)
		if !ok {
			return nil, badReq("unknown column %q", key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		q.Cols = append(q.Cols, c)
		q.Keys = append(q.Keys, key)
	}
	if !seen[g.IDColumn] {
		idc, _ := g.Column(g.IDColumn)
		q.Cols = append(q.Cols, idc)
		q.Keys = append(q.Keys, g.IDColumn)
	}
	if len(q.Cols) == 0 {
		return nil, badReq("columns required")
	}

	if len(req.Filters) > maxGroups {
		return nil, badReq("too many filter groups (max %d)", maxGroups)
	}
	for gi, group := range req.Filters {
		if len(group) == 0 {
			return nil, badReq("filter group %d is empty", gi)
		}
		if len(group) > maxClauses {
			return nil, badReq("too many clauses in group %d (max %d)", gi, maxClauses)
		}
		var clauses []Clause
		for _, fc := range group {
			c, err := buildClause(g, fc)
			if err != nil {
				return nil, err
			}
			clauses = append(clauses, c)
		}
		q.Groups = append(q.Groups, clauses)
	}

	sorts := req.Sort
	if len(sorts) == 0 {
		sorts = []SortSpec{g.DefaultSort}
	}
	if len(sorts) > maxSortColumns {
		return nil, badReq("too many sort columns (max %d)", maxSortColumns)
	}
	seenSort := map[string]bool{}
	for _, s := range sorts {
		sc, ok := g.Column(s.Column)
		if !ok || !sc.Sortable || (s.Dir != "asc" && s.Dir != "desc") {
			return nil, badReq("invalid sort %+v", s)
		}
		if seenSort[s.Column] {
			return nil, badReq("duplicate sort column %q", s.Column)
		}
		seenSort[s.Column] = true
		q.Sorts = append(q.Sorts, SortTerm{Spec: s, Col: sc})
	}

	q.Page = req.Page
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Page < 1 {
		return nil, badReq("page must be >= 1")
	}
	q.PageSize = req.PageSize
	if q.PageSize == 0 {
		q.PageSize = g.PageSize
	}
	if q.PageSize < 1 || q.PageSize > maxPageSize {
		return nil, badReq("pageSize must be 1..%d", maxPageSize)
	}
	return q, nil
}

func buildClause(g *Grid, fc FilterClause) (Clause, *ReqError) {
	col, ok := g.Column(fc.Field)
	if !ok || col.Filter == nil {
		return Clause{}, badReq("unknown or non-filterable field %q", fc.Field)
	}
	allowed := false
	for _, op := range col.Filter.Operators {
		if op == fc.Op {
			allowed = true
		}
	}
	if !allowed {
		return Clause{}, badReq("op %q not allowed for field %q", fc.Op, fc.Field)
	}
	c := Clause{Col: col, Op: fc.Op}
	var err *ReqError
	switch col.Type {
	case TypeString, TypeEnum:
		if fc.Op == OpIn {
			c.Value, err = toStrings(fc)
		} else {
			c.Value, err = toString(fc)
		}
	case TypeNumber:
		if fc.Op == OpBetween {
			c.Value, c.Value2, err = toPair(fc, toFloat)
		} else {
			c.Value, err = toFloat(fc.Value, fc)
		}
	case TypeDatetime:
		if fc.Op == OpBetween {
			c.Value, c.Value2, err = toPair(fc, toTime)
		} else {
			c.Value, err = toTime(fc.Value, fc)
		}
	case TypeBool:
		b, ok := fc.Value.(bool)
		if !ok {
			return Clause{}, badReq("field %q: boolean expected", fc.Field)
		}
		c.Value = b
	default:
		return Clause{}, badReq("field %q: unsupported column type", fc.Field)
	}
	if err != nil {
		return Clause{}, err
	}
	if fc.Op == OpBetween {
		if f, ok := c.Value.(float64); ok && f > c.Value2.(float64) {
			return Clause{}, badReq("field %q: between requires a <= b", fc.Field)
		}
		if t, ok := c.Value.(time.Time); ok && t.After(c.Value2.(time.Time)) {
			return Clause{}, badReq("field %q: between requires a <= b", fc.Field)
		}
	}
	return c, nil
}

func toString(fc FilterClause) (any, *ReqError) {
	s, ok := fc.Value.(string)
	if !ok {
		return nil, badReq("field %q: string expected", fc.Field)
	}
	return s, nil
}

func toStrings(fc FilterClause) (any, *ReqError) {
	raw, ok := fc.Value.([]any)
	if !ok || len(raw) == 0 {
		return nil, badReq("field %q: non-empty array expected", fc.Field)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, badReq("field %q: array of strings expected", fc.Field)
		}
		out[i] = s
	}
	return out, nil
}

func toFloat(v any, fc FilterClause) (any, *ReqError) {
	f, ok := v.(float64) // encoding/json decodes numbers as float64
	if !ok {
		return nil, badReq("field %q: number expected", fc.Field)
	}
	return f, nil
}

func toTime(v any, fc FilterClause) (any, *ReqError) {
	s, ok := v.(string)
	if !ok {
		return nil, badReq("field %q: RFC3339 string expected", fc.Field)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, badReq("field %q: invalid RFC3339: %v", fc.Field, err)
	}
	return t, nil
}

func toPair(fc FilterClause, conv func(any, FilterClause) (any, *ReqError)) (any, any, *ReqError) {
	raw, ok := fc.Value.([]any)
	if !ok || len(raw) != 2 {
		return nil, nil, badReq("field %q: between expects [a, b]", fc.Field)
	}
	a, err := conv(raw[0], fc)
	if err != nil {
		return nil, nil, err
	}
	b, err := conv(raw[1], fc)
	if err != nil {
		return nil, nil, err
	}
	return a, b, nil
}
