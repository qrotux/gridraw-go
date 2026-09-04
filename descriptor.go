package gridraw

import "time"

// OpDesc is a filter operator with its localized label.
type OpDesc struct {
	Op    Op     `json:"op"`
	Label string `json:"label"`
}

// EnumValue is an enum member with its localized label.
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FilterDesc describes the filter UI of one column.
type FilterDesc struct {
	Operators  []OpDesc    `json:"operators"`
	EnumValues []EnumValue `json:"enumValues,omitempty"`
	Widget     string      `json:"widget,omitempty"`
}

// ColumnDesc is the wire form of a Column.
type ColumnDesc struct {
	Key            string      `json:"key"`
	Type           ColType     `json:"type"`
	Title          string      `json:"title"`
	Sortable       bool        `json:"sortable"`
	DefaultVisible bool        `json:"defaultVisible"`
	Step           int         `json:"step,omitempty"`  // seconds; time and datetime only
	Array          bool        `json:"array,omitempty"` // Type is the element type
	Filter         *FilterDesc `json:"filter,omitempty"`
}

// SearchInfo lists the titles of quick-search columns (for a placeholder).
type SearchInfo struct {
	Columns []string `json:"columns"`
}

// Descriptor is the wire form of a Grid.
type Descriptor struct {
	Name            string       `json:"name"`
	IDColumn        string       `json:"idColumn"`
	PageSize        int          `json:"pageSize"`
	PageSizeOptions []int        `json:"pageSizeOptions"`
	DefaultSort     SortSpec     `json:"defaultSort"`
	Search          *SearchInfo  `json:"search"`
	Columns         []ColumnDesc `json:"columns"`
}

// i18n key convention: column titles "grid.<grid>.<column>", operator labels
// "grid.operators.<op>", enum labels "grid.<grid>.<column>_values.<value>".
func columnKey(gridName, key string) string  { return "grid." + gridName + "." + key }
func operatorKey(op Op) string               { return "grid.operators." + string(op) }
func enumKey(gridName, key, v string) string { return "grid." + gridName + "." + key + "_values." + v }

// BuildDescriptor renders a grid for the client, resolving titles through tr.
func BuildDescriptor(g *Grid, tr Translator, locale string) Descriptor {
	opts := g.PageSizeOptions
	if opts == nil {
		opts = defaultPageSizeOptions // grids built outside NewRegistry are not normalized
	}
	d := Descriptor{
		Name: g.Name, IDColumn: g.IDColumn, PageSize: g.PageSize,
		PageSizeOptions: opts, DefaultSort: g.DefaultSort,
	}
	var searchTitles []string
	for _, c := range g.Columns {
		title := tr(locale, columnKey(g.Name, c.Key))
		cd := ColumnDesc{
			Key: c.Key, Type: c.Type, Title: title,
			Sortable: c.Sortable, DefaultVisible: c.DefaultVisible, Array: c.Array,
		}
		if c.Type == TypeTime || c.Type == TypeDatetime {
			cd.Step = int(c.step() / time.Second)
		}
		if c.Filter != nil {
			fd := &FilterDesc{}
			for _, op := range c.Filter.Operators {
				fd.Operators = append(fd.Operators, OpDesc{Op: op, Label: tr(locale, operatorKey(op))})
			}
			for _, v := range c.Enum {
				fd.EnumValues = append(fd.EnumValues, EnumValue{Value: v, Label: tr(locale, enumKey(g.Name, c.Key, v))})
			}
			fd.Widget = c.Filter.Widget
			cd.Filter = fd
		}
		if c.Searchable {
			searchTitles = append(searchTitles, title)
		}
		d.Columns = append(d.Columns, cd)
	}
	if len(searchTitles) > 0 {
		d.Search = &SearchInfo{Columns: searchTitles}
	}
	return d
}
