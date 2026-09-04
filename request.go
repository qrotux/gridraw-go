package gridraw

import (
	"fmt"
	"math/big"
	"strings"
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

// Clause is a validated, typed predicate. Value2 is the upper bound of the
// range operators; UpperOpen marks it exclusive ([Value, Value2) instead of
// [Value, Value2]), which is how stepped time columns are expressed.
type Clause struct {
	Col       Column
	Op        Op
	Value     any
	Value2    any
	UpperOpen bool
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
		idc, ok := g.Column(g.IDColumn)
		if !ok {
			// Only a Grid.ForContext result can get here: registered grids
			// are validated. A server bug, not a client error.
			return nil, &ReqError{Status: 500, Msg: fmt.Sprintf("grid misconfigured: idColumn %q not found", g.IDColumn)}
		}
		q.Cols = append(q.Cols, idc)
		q.Keys = append(q.Keys, g.IDColumn)
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
	// Re-checked here, not only in validateGrid: a Grid.ForContext result is
	// never validated, and clauseExpr has no branch for a foreign op.
	if !opAllowed(col, fc.Op) {
		return Clause{}, badReq("op %q not allowed for field %q", fc.Op, fc.Field)
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
	if valueless(fc.Op) {
		return c, nil // value ignored
	}
	if col.Array {
		return buildArrayClause(c, fc)
	}
	var err *ReqError
	switch col.Type {
	case TypeString, TypeEnum:
		if fc.Op == OpIn || fc.Op == OpNotIn {
			c.Value, err = toStrings(fc)
		} else {
			c.Value, err = toString(fc)
		}
	case TypeUUID:
		if fc.Op == OpIn || fc.Op == OpNotIn {
			c.Value, err = toUUIDs(fc)
		} else {
			c.Value, err = toUUID(fc)
		}
	case TypeNumber:
		if isRange(fc.Op) {
			c.Value, c.Value2, err = toPair(fc, toFloat)
		} else {
			c.Value, err = toFloat(fc.Value, fc)
		}
	case TypeDecimal:
		if isRange(fc.Op) {
			c.Value, c.Value2, err = toPair(fc, toDecimal)
		} else {
			c.Value, err = toDecimal(fc.Value, fc)
		}
	case TypeDate:
		if isRange(fc.Op) {
			c.Value, c.Value2, err = toPair(fc, toDate)
		} else {
			c.Value, err = toDate(fc.Value, fc)
		}
	case TypeTime:
		if isRange(fc.Op) {
			c.Value, c.Value2, err = toPair(fc, toClock)
		} else {
			c.Value, err = toClock(fc.Value, fc)
		}
	case TypeDatetime:
		if isRange(fc.Op) {
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
	if isRange(fc.Op) {
		if f, ok := c.Value.(float64); ok && f > c.Value2.(float64) {
			return Clause{}, badReq("field %q: %s requires a <= b", fc.Field, fc.Op)
		}
		if col.Type == TypeDecimal && decimalGreater(c.Value.(string), c.Value2.(string)) {
			return Clause{}, badReq("field %q: %s requires a <= b", fc.Field, fc.Op)
		}
		if t, ok := c.Value.(time.Time); ok && t.After(c.Value2.(time.Time)) {
			return Clause{}, badReq("field %q: %s requires a <= b", fc.Field, fc.Op)
		}
	}
	if col.Type == TypeTime || col.Type == TypeDatetime {
		return applyStep(c, fc)
	}
	return c, nil
}

// buildArrayClause converts every element with the scalar converter of the
// element type; Value becomes a typed slice.
func buildArrayClause(c Clause, fc FilterClause) (Clause, *ReqError) {
	raw, ok := fc.Value.([]any)
	if !ok || len(raw) == 0 {
		return Clause{}, badReq("field %q: non-empty array expected", fc.Field)
	}
	conv, err := elemConverter(c.Col, fc)
	if err != nil {
		return Clause{}, err
	}
	vals := make([]any, len(raw))
	for i, v := range raw {
		vals[i], err = conv(v)
		if err != nil {
			return Clause{}, err
		}
	}
	c.Value = typedSlice(vals)
	return c, nil
}

func elemConverter(col Column, fc FilterClause) (func(any) (any, *ReqError), *ReqError) {
	single := func(f func(FilterClause) (any, *ReqError)) func(any) (any, *ReqError) {
		return func(v any) (any, *ReqError) { e := fc; e.Value = v; return f(e) }
	}
	withStep := func(f func(any, FilterClause) (any, *ReqError)) func(any) (any, *ReqError) {
		return func(v any) (any, *ReqError) {
			t, err := f(v, fc)
			if err != nil {
				return nil, err
			}
			if !aligned(t.(time.Time), col.step()) {
				return nil, badReq("field %q: value must be aligned to %v", fc.Field, col.step())
			}
			return t, nil
		}
	}
	switch col.Type {
	case TypeString, TypeEnum:
		return single(toString), nil
	case TypeUUID:
		return single(toUUID), nil
	case TypeNumber:
		return func(v any) (any, *ReqError) { return toFloat(v, fc) }, nil
	case TypeDecimal:
		return func(v any) (any, *ReqError) { return toDecimal(v, fc) }, nil
	case TypeBool:
		return func(v any) (any, *ReqError) {
			b, ok := v.(bool)
			if !ok {
				return nil, badReq("field %q: array of booleans expected", fc.Field)
			}
			return b, nil
		}, nil
	case TypeDate:
		return func(v any) (any, *ReqError) { return toDate(v, fc) }, nil
	case TypeTime:
		return withStep(toClock), nil
	case TypeDatetime:
		return withStep(toTime), nil
	}
	return nil, badReq("field %q: unsupported array element type", fc.Field)
}

// typedSlice turns the converted elements into []string, []float64, []bool
// or []time.Time so compilers get one concrete type per element type.
func typedSlice(vals []any) any {
	switch vals[0].(type) {
	case string:
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = v.(string)
		}
		return out
	case float64:
		out := make([]float64, len(vals))
		for i, v := range vals {
			out[i] = v.(float64)
		}
		return out
	case bool:
		out := make([]bool, len(vals))
		for i, v := range vals {
			out[i] = v.(bool)
		}
		return out
	case time.Time:
		out := make([]time.Time, len(vals))
		for i, v := range vals {
			out[i] = v.(time.Time)
		}
		return out
	}
	return vals
}

// applyStep validates alignment and, for steps above one second, widens the
// clause to whole buckets so that eq 09:15 on a 15-minute column matches
// 09:20. Bucket upper bounds are exclusive (UpperOpen); gte and lt are
// unchanged because their bound already sits on a bucket edge.
func applyStep(c Clause, fc FilterClause) (Clause, *ReqError) {
	step := c.Col.step()
	for _, v := range []any{c.Value, c.Value2} {
		if t, ok := v.(time.Time); ok && !aligned(t, step) {
			return Clause{}, badReq("field %q: value must be aligned to %v", fc.Field, step)
		}
	}
	if step == time.Second {
		return c, nil
	}
	v, _ := c.Value.(time.Time)
	switch c.Op {
	case OpEq:
		c.Op, c.Value2, c.UpperOpen = OpBetween, v.Add(step), true
	case OpNeq:
		c.Op, c.Value2, c.UpperOpen = OpNotBetween, v.Add(step), true
	case OpGt:
		c.Op, c.Value = OpGte, v.Add(step)
	case OpLte:
		c.Op, c.Value = OpLt, v.Add(step)
	case OpBetween, OpNotBetween:
		c.Value2, c.UpperOpen = c.Value2.(time.Time).Add(step), true
	}
	return c, nil
}

// aligned reports whether t sits on a step boundary counted from midnight
// in t's own zone, so an hourly datetime step follows the client's clock.
func aligned(t time.Time, step time.Duration) bool {
	sinceMidnight := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
	return sinceMidnight%step == 0
}

func isRange(op Op) bool { return op == OpBetween || op == OpNotBetween }

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

// toUUID accepts the canonical 8-4-4-4-12 form in any case and returns it
// lowercased. Anything else would reach Postgres as an invalid ::uuid cast
// and surface as a 500.
func toUUID(fc FilterClause) (any, *ReqError) {
	s, ok := fc.Value.(string)
	if !ok || !isUUID(s) {
		return nil, badReq("field %q: uuid expected", fc.Field)
	}
	return strings.ToLower(s), nil
}

func toUUIDs(fc FilterClause) (any, *ReqError) {
	v, err := toStrings(fc)
	if err != nil {
		return nil, err
	}
	out := v.([]string)
	for i, s := range out {
		if !isUUID(s) {
			return nil, badReq("field %q: array of uuids expected", fc.Field)
		}
		out[i] = strings.ToLower(s)
	}
	return out, nil
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
				return false
			}
		}
	}
	return true
}

// toDecimal accepts a decimal string only. A JSON number would already have
// gone through float64 on the client and lost exactly what this type is for.
func toDecimal(v any, fc FilterClause) (any, *ReqError) {
	s, ok := v.(string)
	if !ok || !isDecimal(s) {
		return nil, badReq("field %q: decimal string expected", fc.Field)
	}
	return s, nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	intPart, frac, hasDot := strings.Cut(s, ".")
	if intPart == "" || (hasDot && frac == "") {
		return false
	}
	for _, r := range intPart + frac {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// decimalGreater compares two validated decimal strings exactly.
func decimalGreater(a, b string) bool {
	ra, _ := new(big.Rat).SetString(a)
	rb, _ := new(big.Rat).SetString(b)
	return ra.Cmp(rb) > 0
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

// toDate accepts YYYY-MM-DD only; a value with a time part is a client bug,
// not something to silently truncate.
func toDate(v any, fc FilterClause) (any, *ReqError) {
	s, ok := v.(string)
	if !ok {
		return nil, badReq("field %q: YYYY-MM-DD string expected", fc.Field)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, badReq("field %q: invalid date: %v", fc.Field, err)
	}
	return t, nil
}

// toClock accepts HH:MM:SS or HH:MM. The result carries only the clock
// part (zero date, UTC); a between across midnight is not expressible with
// a single clause because a must be <= b.
func toClock(v any, fc FilterClause) (any, *ReqError) {
	s, ok := v.(string)
	if !ok {
		return nil, badReq("field %q: HH:MM:SS string expected", fc.Field)
	}
	layout := time.TimeOnly
	if len(s) == len("15:04") {
		layout = "15:04"
	}
	t, err := time.Parse(layout, s)
	if err != nil || len(s) != len(layout) { // Go's parser accepts "9:00" for "15:04"; the wire format does not
		return nil, badReq("field %q: invalid time %q, want HH:MM or HH:MM:SS", fc.Field, s)
	}
	return t, nil
}

func toPair(fc FilterClause, conv func(any, FilterClause) (any, *ReqError)) (any, any, *ReqError) {
	raw, ok := fc.Value.([]any)
	if !ok || len(raw) != 2 {
		return nil, nil, badReq("field %q: %s expects [a, b]", fc.Field, fc.Op)
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
