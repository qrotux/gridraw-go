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
	Description    string      `json:"description,omitempty"`
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
	Description     string       `json:"description,omitempty"`
	IDColumn        string       `json:"idColumn"`
	PageSize        int          `json:"pageSize"`
	PageSizeOptions []int        `json:"pageSizeOptions"`
	DefaultSort     SortSpec     `json:"defaultSort"`
	Search          *SearchInfo  `json:"search"`
	Columns         []ColumnDesc `json:"columns"`
}

// i18n key convention: column titles "grid.<grid>.<column>", operator labels
// "grid.operators.<op>", enum labels "grid.<grid>.<column>_values.<value>",
// descriptions "grid.<grid>.description" and "grid.<grid>.<column>.description".
func columnKey(gridName, key string) string  { return "grid." + gridName + "." + key }
func operatorKey(op Op) string               { return "grid.operators." + string(op) }
func enumKey(gridName, key, v string) string { return "grid." + gridName + "." + key + "_values." + v }
func gridDescKey(gridName string) string     { return "grid." + gridName + ".description" }
func columnDescKey(gridName, key string) string {
	return columnKey(gridName, key) + ".description"
}

// describe prefers the translation of key over the literal description written
// in the grid. A Translator that has no entry is expected to echo the key back,
// so a returned key counts as a miss.
func describe(tr Translator, locale, key, lit string) string {
	if t := tr(locale, key); t != "" && t != key {
		return t
	}
	return lit
}

// GridInfo is one entry of the list endpoint.
type GridInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ColumnInfo is one column of a registry entry.
type ColumnInfo struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Type        ColType `json:"type"`
	Description string  `json:"description,omitempty"`
}

// GridEntry is one entry of the registry endpoint: a grid with its columns.
type GridEntry struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Columns     []ColumnInfo `json:"columns"`
}

// BuildGridInfo renders the list entry of a grid, resolving its description through tr.
func BuildGridInfo(g *Grid, tr Translator, locale string) GridInfo {
	return GridInfo{Name: g.Name, Description: describe(tr, locale, gridDescKey(g.Name), g.Description)}
}

// BuildGridEntry renders the registry entry of a grid, resolving titles and
// descriptions through tr.
func BuildGridEntry(g *Grid, tr Translator, locale string) GridEntry {
	e := GridEntry{
		Name:        g.Name,
		Description: describe(tr, locale, gridDescKey(g.Name), g.Description),
		Columns:     make([]ColumnInfo, 0, len(g.Columns)),
	}
	for _, c := range g.Columns {
		e.Columns = append(e.Columns, ColumnInfo{
			Key: c.Key, Title: tr(locale, columnKey(g.Name, c.Key)), Type: c.Type,
			Description: describe(tr, locale, columnDescKey(g.Name, c.Key), c.Description),
		})
	}
	return e
}

// BuildDescriptor renders a grid for the client, resolving titles through tr.
func BuildDescriptor(g *Grid, tr Translator, locale string) Descriptor {
	opts := g.PageSizeOptions
	if opts == nil {
		opts = defaultPageSizeOptions // grids built outside NewRegistry are not normalized
	}
	d := Descriptor{
		Name: g.Name, Description: describe(tr, locale, gridDescKey(g.Name), g.Description),
		IDColumn: g.IDColumn, PageSize: g.PageSize,
		PageSizeOptions: opts, DefaultSort: g.DefaultSort,
	}
	var searchTitles []string
	for _, c := range g.Columns {
		title := tr(locale, columnKey(g.Name, c.Key))
		cd := ColumnDesc{
			Key: c.Key, Type: c.Type, Title: title,
			Description: describe(tr, locale, columnDescKey(g.Name, c.Key), c.Description),
			Sortable:    c.Sortable, DefaultVisible: c.DefaultVisible, Array: c.Array,
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
