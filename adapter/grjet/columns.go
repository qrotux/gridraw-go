package grjet

import (
	"github.com/go-jet/jet/v2/postgres"

	"github.com/qrotux/gridraw-go"
)

// Base wraps a table constructor as a Grid binding.
func Base(base func() postgres.ReadableTable) GridBinding { return GridBinding{Base: base} }

// StrCol is a sortable string column with every string filter.
func StrCol(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeString, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// UUIDCol is a sortable uuid column with exact eq/neq/in/notIn filters.
// go-jet generates uuid columns as ColumnString.
func UUIDCol(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeUUID, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// StrColNoFilter is a sortable string column without a filter (opaque, non-uuid ids).
func StrColNoFilter(key string, c postgres.ColumnString) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeString, Binding: Binding{Projection: c}, Sortable: true}
}

// BoolCol is a sortable boolean column with an eq filter.
func BoolCol(key string, c postgres.ColumnBool) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeBool, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// NumCol is a sortable number column over a float column.
func NumCol(key string, c postgres.ColumnFloat) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeNumber, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// DecimalCol is a sortable exact-number column. The projection is cast to
// text so the executor never sees a float: pgx cannot tell "money" from
// "count" by OID, and numeric::text keeps the stored scale ("4.10").
func DecimalCol(key string, c postgres.ColumnFloat) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeDecimal, Sortable: true,
		Binding: Binding{Projection: postgres.CAST(c).AS_TEXT().AS(key), Filter: c, Sort: c},
		Filter:  &gridraw.FilterSpec{}}
}

// IntCol is a sortable number column over an integer column; the wire type
// is the same as NumCol, only go-jet's static type differs.
func IntCol(key string, c postgres.ColumnInteger) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeNumber, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// TsCol is a sortable datetime column with eq and range filters.
func TsCol(key string, c postgres.ColumnTimestampz) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeDatetime, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// DateCol is a sortable date column with eq and range filters.
func DateCol(key string, c postgres.ColumnDate) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeDate, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// TimeCol is a sortable time-of-day column with eq and range filters.
func TimeCol(key string, c postgres.ColumnTime) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeTime, Binding: Binding{Projection: c}, Sortable: true,
		Filter: &gridraw.FilterSpec{}}
}

// EnumCol is a sortable enum column with in/notIn filters.
func EnumCol(key string, c postgres.ColumnString, values []string) gridraw.Column {
	return gridraw.Column{Key: key, Type: gridraw.TypeEnum, Binding: Binding{Projection: c}, Sortable: true, Enum: values,
		Filter: &gridraw.FilterSpec{}}
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
		Filter:  &gridraw.FilterSpec{},
	}
}

// ArrayCol is an array column over the go-jet array column or expression c,
// with elem as the element type. Element matching is exact. The parameter is
// bound as <elem sql type>[]; use PgType when the SQL element type differs
// (integer[] instead of float8[], a Postgres enum). A decimal array is
// projected as text[] for the same reason DecimalCol casts to text.
func ArrayCol(key string, c postgres.Expression, elem gridraw.ColType) gridraw.Column {
	b := Binding{Projection: c}
	if elem == gridraw.TypeDecimal {
		b = Binding{Projection: postgres.CAST(c).AS("text[]").AS(key), Filter: c}
	}
	return gridraw.Column{Key: key, Type: elem, Array: true, Binding: b,
		Filter: &gridraw.FilterSpec{}}
}

// EnumArrayCol is an array of enum values; wrap in PgType when the element
// is a Postgres enum type.
func EnumArrayCol(key string, c postgres.Expression, values []string) gridraw.Column {
	col := ArrayCol(key, c, gridraw.TypeEnum)
	col.Enum = values
	return col
}

// Bind replaces the go-jet binding of a column built by a constructor, so a
// derived expression (COALESCE, an aggregate, a cast) keeps the constructor's
// type and filters instead of being declared as a gridraw.Column literal. The
// column passed to the constructor is only a placeholder for its static type.
// An aliased Projection needs explicit Filter and Sort, and a DecimalCol
// binding must cast its projection to text itself; see Binding.
func Bind(c gridraw.Column, b Binding) gridraw.Column { c.Binding = b; return c }

// Vis marks a column visible by default.
//
// Deprecated: use gridraw.Column.Vis.
func Vis(c gridraw.Column) gridraw.Column { return c.Vis() }

// PgType names the SQL type of the column (or of the array element) so that
// parameters are cast to it: a Postgres enum, or an integer[] column whose
// number elements would otherwise bind as float8[]. See Binding.ParamType.
// An array column is also projected as text[], because pgx returns arrays
// of unknown element types as their literal ("{en,ru}") instead of a slice.
func PgType(name string, c gridraw.Column) gridraw.Column {
	b, _ := c.Binding.(Binding)
	b.ParamType = name
	if e, ok := exprOf(b.Projection); ok && c.Array {
		if b.Filter == nil {
			b.Filter = e
		}
		b.Projection = postgres.CAST(e).AS("text[]").AS(c.Key)
	}
	c.Binding = b
	return c
}

// Searchable adds a column to quick search.
//
// Deprecated: use gridraw.Column.WithSearch.
func Searchable(c gridraw.Column) gridraw.Column { return c.WithSearch() }
