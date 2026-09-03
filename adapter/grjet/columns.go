package grjet

import (
	"github.com/go-jet/jet/v2/postgres"

	"github.com/qrotux/gridraw-go"
)

var (
	stringOps = []gridraw.Op{gridraw.OpEq, gridraw.OpContains, gridraw.OpStarts}
	numberOps = []gridraw.Op{gridraw.OpEq, gridraw.OpGte, gridraw.OpLte, gridraw.OpBetween}
	timeOps   = []gridraw.Op{gridraw.OpGte, gridraw.OpLte, gridraw.OpBetween}
)

// Base wraps a table constructor as a Grid binding.
func Base(base func() postgres.ReadableTable) GridBinding { return GridBinding{Base: base} }

// StrCol is a sortable string column with eq/contains/starts filters.
func StrCol(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeString, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{Operators: stringOps}}
}

// StrColNoFilter is a sortable string column without a filter (opaque ids).
func StrColNoFilter(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeString, Binding: Binding{Projection: c}, Sortable: true}
}

// BoolCol is a sortable boolean column with an eq filter.
func BoolCol(key string, c postgres.ColumnBool) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeBool, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpEq}}}
}

// NumCol is a sortable number column over a float column.
func NumCol(key string, c postgres.ColumnFloat) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeNumber, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{Operators: numberOps}}
}

// IntCol is a sortable number column over an integer column; the wire type
// is the same as NumCol, only go-jet's static type differs.
func IntCol(key string, c postgres.ColumnInteger) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeNumber, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{Operators: numberOps}}
}

// TsCol is a sortable datetime column with gte/lte/between filters.
func TsCol(key string, c postgres.ColumnTimestampz) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeDatetime, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{Operators: timeOps}}
}

// EnumCol is a sortable enum column with an in filter.
func EnumCol(key string, c postgres.ColumnString, values []string) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeEnum, Binding: Binding{Projection: c}, Sortable: true, Enum: values,
		Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpIn}}}
}

// JSONCol is a display-only jsonb column.
func JSONCol(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeJSON, Binding: Binding{Projection: c}}
}

// JoinStrCol is a string column of a joined table: the projection is
// aliased to key so the positional scan lands on it, while filter and sort
// use the bare column because Postgres rejects aliases in WHERE.
func JoinStrCol(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{
		Key: key, Type: gridraw.TypeString, Sortable: true,
		Binding: Binding{Projection: c.AS(key), Filter: c, Sort: c},
		Filter:  &gridraw.FilterSpec{Operators: stringOps},
	}
}

// Vis marks a column visible by default.
func Vis(c gridraw.Column) gridraw.Column { c.DefaultVisible = true; return c }

// Searchable adds a column to quick search.
func Searchable(c gridraw.Column) gridraw.Column { c.Searchable = true; return c }
