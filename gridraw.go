// Package gridraw is a server-driven data grid protocol: a grid definition is
// validated once, published to the client as a descriptor, and turned into
// paginated, filtered, sorted row pages. The core knows nothing about SQL,
// routers or drivers; those plug in through Compiler, Executor and the router
// subpackages.
package gridraw

import (
	"context"
	"time"
)

// ColType is the wire type of a column.
type ColType string

const (
	TypeString ColType = "string"
	// TypeUUID is an identifier: exact matching on the canonical form, never searchable.
	TypeUUID   ColType = "uuid"
	TypeNumber ColType = "number"
	// TypeDecimal is an exact number (money, percentages): wire values are
	// decimal strings such as "19.99", compared as numeric in SQL.
	TypeDecimal ColType = "decimal"
	TypeBool    ColType = "boolean"
	TypeEnum    ColType = "enum"
	// TypeDate is a calendar day without time zone; wire values are YYYY-MM-DD.
	TypeDate ColType = "date"
	// TypeTime is a time of day without time zone; wire values are HH:MM:SS (HH:MM accepted).
	TypeTime     ColType = "time"
	TypeDatetime ColType = "datetime"
	// TypeJSON is display-only: no filter, no sort, no quick search.
	TypeJSON ColType = "json"
)

// Op is a filter operator.
type Op string

const (
	OpEq          Op = "eq"
	OpNeq         Op = "neq"
	OpContains    Op = "contains"
	OpNotContains Op = "notContains"
	OpStarts      Op = "starts"
	OpEnds        Op = "ends"
	OpGt          Op = "gt"
	OpGte         Op = "gte"
	OpLt          Op = "lt"
	OpLte         Op = "lte"
	OpBetween     Op = "between"
	OpNotBetween  Op = "notBetween"
	OpIn          Op = "in"
	OpNotIn       Op = "notIn"
	// OpIsNull and OpIsNotNull take no value and are allowed on every type.
	OpIsNull    Op = "isNull"
	OpIsNotNull Op = "isNotNull"
	// Array operators; the value is an array of element values. Element
	// matching is exact.
	OpContainsAny Op = "containsAny"
	OpContainsAll Op = "containsAll"
	// OpContainsOnly matches an array holding exactly the given set: every
	// element is in the value and every value is in the element. Order and
	// duplicates do not matter.
	OpContainsOnly   Op = "containsOnly"
	OpNotContainsAny Op = "notContainsAny"
	OpIsEmpty        Op = "isEmpty"
	OpIsNotEmpty     Op = "isNotEmpty"
)

var opsByType = map[ColType]map[Op]bool{
	TypeString:   {OpEq: true, OpNeq: true, OpContains: true, OpNotContains: true, OpStarts: true, OpEnds: true},
	TypeUUID:     {OpEq: true, OpNeq: true, OpIn: true, OpNotIn: true},
	TypeNumber:   {OpEq: true, OpNeq: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpBetween: true, OpNotBetween: true},
	TypeDecimal:  {OpEq: true, OpNeq: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpBetween: true, OpNotBetween: true},
	TypeDate:     {OpEq: true, OpNeq: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpBetween: true, OpNotBetween: true},
	TypeTime:     {OpEq: true, OpNeq: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpBetween: true, OpNotBetween: true},
	TypeDatetime: {OpEq: true, OpNeq: true, OpGt: true, OpGte: true, OpLt: true, OpLte: true, OpBetween: true, OpNotBetween: true},
	TypeEnum:     {OpIn: true, OpNotIn: true},
	TypeBool:     {OpEq: true},
	TypeJSON:     {},
}

var arrayOps = map[Op]bool{OpContainsAny: true, OpContainsAll: true, OpContainsOnly: true, OpNotContainsAny: true, OpIsEmpty: true, OpIsNotEmpty: true}

func opAllowed(c Column, op Op) bool {
	if op == OpIsNull || op == OpIsNotNull {
		return true
	}
	if c.Array {
		return arrayOps[op]
	}
	return opsByType[c.Type][op]
}

// valueless reports whether op carries no value.
func valueless(op Op) bool {
	return op == OpIsNull || op == OpIsNotNull || op == OpIsEmpty || op == OpIsNotEmpty
}

// SortSpec is one ORDER BY term; Dir is "asc" or "desc".
type SortSpec struct {
	Column string `json:"column"`
	Dir    string `json:"dir"`
}

// Translator resolves an i18n key for a locale (see the key convention in descriptor.go).
type Translator func(locale, key string) string

// FilterSpec lists the operators a column accepts; nil on a Column means not
// filterable. Widget is a hint for how the UI renders the filter input; the
// core never interprets it beyond publishing it in the descriptor.
type FilterSpec struct {
	Operators []Op
	Widget    string
}

// Filter widget hints. The set is open: a client may honour any value and
// falls back to its own default (empty). These name the common ones.
const (
	WidgetCheckboxes = "checkboxes" // enum / enum array as a checkbox list
	WidgetTags       = "tags"       // enum / enum array as a tag input
)

// Column is one grid column. Binding carries whatever the Compiler needs to
// project, filter and sort it (for grjet: the go-jet expressions); the core
// never inspects it.
type Column struct {
	Key  string
	Type ColType
	// Description is free-form documentation of the column, published in the
	// descriptor and the registry endpoint; a translation of
	// "grid.<grid>.<column>.description" overrides it.
	Description    string
	Filter         *FilterSpec
	Sortable       bool
	Searchable     bool // TypeString only; joins the quick-search OR
	DefaultVisible bool
	Enum           []string // TypeEnum values
	// Step is the resolution of a TypeTime or TypeDatetime column; zero means
	// one second. Filter values must be aligned to it and eq/neq/gt/lte and
	// the range operators act on whole [v, v+Step) buckets.
	Step time.Duration
	// Array makes the column an array of Type. Array columns use the array
	// operators, are never sortable, and are searchable only for string
	// elements. Step on a time array only validates alignment and informs the
	// UI; array matching stays exact.
	Array   bool
	Binding any
}

// Vis marks the column visible by default.
func (c Column) Vis() Column { c.DefaultVisible = true; return c }

// FilterWidget sets the UI hint for the column's filter input; see FilterSpec.
// A column with no filter is a declaration error, caught by NewRegistry.
func (c Column) FilterWidget(w string) Column {
	if c.Filter == nil {
		c.Filter = &FilterSpec{}
	} else {
		f := *c.Filter // copy so a shared FilterSpec is not mutated
		c.Filter = &f
	}
	c.Filter.Widget = w
	return c
}

// WithDescription documents the column; see Column.Description.
func (c Column) WithDescription(d string) Column { c.Description = d; return c }

// WithSearch adds the column to the quick search (TypeString only).
func (c Column) WithSearch() Column { c.Searchable = true; return c }

// Nullable adds isNull/isNotNull to the column's filters. Constructors leave
// them out because most columns are NOT NULL and the extra operators would
// only clutter the filter menu.
func (c Column) Nullable() Column {
	var ops []Op
	if c.Filter != nil {
		ops = append(ops, c.Filter.Operators...)
	}
	c.Filter = &FilterSpec{Operators: append(ops, OpIsNull, OpIsNotNull)}
	return c
}

// WithStep sets the resolution of a time or datetime column; see Step.
func (c Column) WithStep(d time.Duration) Column { c.Step = d; return c }

func (c Column) step() time.Duration {
	if c.Step == 0 {
		return time.Second
	}
	return c.Step
}

var defaultPageSizeOptions = []int{10, 25, 50, 100}

// Grid is a grid definition. Binding carries the Compiler-specific data
// source (for grjet: the base table); the core never inspects it.
type Grid struct {
	Name string
	// Description is free-form documentation of the grid, published in the
	// descriptor and the list and registry endpoints; a translation of
	// "grid.<grid>.description" overrides it.
	Description     string
	IDColumn        string
	PageSize        int
	PageSizeOptions []int
	DefaultSort     SortSpec
	// SkipTotal drops the count query for this grid: the rows response then
	// carries no total and the client paginates on hasPrev/hasNext alone.
	SkipTotal bool
	Columns   []Column
	Binding   any

	// ForContext, when set, replaces the definition per request. The result
	// is not re-validated: derive it from a registered grid.
	ForContext func(ctx context.Context) Grid
}

// Column returns the column with the given key.
func (g *Grid) Column(key string) (Column, bool) {
	for _, c := range g.Columns {
		if c.Key == key {
			return c, true
		}
	}
	return Column{}, false
}
