// Package gridraw is a server-driven data grid protocol: a grid definition is
// validated once, published to the client as a descriptor, and turned into
// paginated, filtered, sorted row pages. The core knows nothing about SQL,
// routers or drivers; those plug in through Compiler, Executor and the router
// subpackages.
package gridraw

import "context"

// ColType is the wire type of a column.
type ColType string

const (
	TypeString   ColType = "string"
	TypeNumber   ColType = "number"
	TypeBool     ColType = "boolean"
	TypeEnum     ColType = "enum"
	TypeDatetime ColType = "datetime"
	// TypeJSON is display-only: no filter, no sort, no quick search.
	TypeJSON ColType = "json"
)

// Op is a filter operator.
type Op string

const (
	OpEq       Op = "eq"
	OpContains Op = "contains"
	OpStarts   Op = "starts"
	OpGte      Op = "gte"
	OpLte      Op = "lte"
	OpBetween  Op = "between"
	OpIn       Op = "in"
)

var opsByType = map[ColType]map[Op]bool{
	TypeString:   {OpEq: true, OpContains: true, OpStarts: true},
	TypeNumber:   {OpEq: true, OpGte: true, OpLte: true, OpBetween: true},
	TypeDatetime: {OpGte: true, OpLte: true, OpBetween: true},
	TypeEnum:     {OpIn: true},
	TypeBool:     {OpEq: true},
	TypeJSON:     {},
}

// SortSpec is one ORDER BY term; Dir is "asc" or "desc".
type SortSpec struct {
	Column string `json:"column"`
	Dir    string `json:"dir"`
}

// Translator resolves an i18n key for a locale (see the key convention in descriptor.go).
type Translator func(locale, key string) string

// FilterSpec lists the operators a column accepts; nil on a Column means not filterable.
type FilterSpec struct {
	Operators []Op
}

// Column is one grid column. Binding carries whatever the Compiler needs to
// project, filter and sort it (for grjet: the go-jet expressions); the core
// never inspects it.
type Column struct {
	Key            string
	Type           ColType
	Filter         *FilterSpec
	Sortable       bool
	Searchable     bool // TypeString only; joins the quick-search OR
	DefaultVisible bool
	Enum           []string // TypeEnum values
	Binding        any
}

var defaultPageSizeOptions = []int{10, 25, 50, 100}

// Grid is a grid definition. Binding carries the Compiler-specific data
// source (for grjet: the base table); the core never inspects it.
type Grid struct {
	Name            string
	IDColumn        string
	PageSize        int
	PageSizeOptions []int
	DefaultSort     SortSpec
	Columns         []Column
	Binding         any

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
