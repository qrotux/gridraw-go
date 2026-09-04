package gridraw

import (
	"fmt"
	"sort"
	"time"
)

// Registry holds validated grids by name.
type Registry struct{ grids map[string]*Grid }

// NewRegistry validates every grid, first structurally and then through the
// Compiler, and rejects duplicate names.
func NewRegistry(c Compiler, grids ...Grid) (*Registry, error) {
	if c == nil {
		return nil, fmt.Errorf("compiler required")
	}
	r := &Registry{grids: map[string]*Grid{}}
	for i := range grids {
		g := grids[i]
		if err := validateGrid(&g); err != nil {
			return nil, fmt.Errorf("grid %q: %w", g.Name, err)
		}
		if err := c.Validate(&g); err != nil {
			return nil, fmt.Errorf("grid %q: %w", g.Name, err)
		}
		if _, dup := r.grids[g.Name]; dup {
			return nil, fmt.Errorf("duplicate grid name %q", g.Name)
		}
		r.grids[g.Name] = &g
	}
	return r, nil
}

// Get returns a registered grid by name.
func (r *Registry) Get(name string) (*Grid, bool) { g, ok := r.grids[name]; return g, ok }

// Names lists registered grid names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.grids))
	for name := range r.grids {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateGrid(g *Grid) error {
	if g.Name == "" {
		return fmt.Errorf("name required")
	}
	if g.PageSize < 1 || g.PageSize > maxPageSize {
		return fmt.Errorf("pageSize %d out of 1..%d", g.PageSize, maxPageSize)
	}
	if g.PageSizeOptions == nil {
		g.PageSizeOptions = defaultPageSizeOptions
	}
	if len(g.PageSizeOptions) == 0 {
		return fmt.Errorf("pageSizeOptions must be non-empty")
	}
	inOptions := false
	for _, o := range g.PageSizeOptions {
		if o < 1 || o > maxPageSize {
			return fmt.Errorf("pageSizeOption %d out of 1..%d", o, maxPageSize)
		}
		if o == g.PageSize {
			inOptions = true
		}
	}
	if !inOptions {
		return fmt.Errorf("pageSize %d not in pageSizeOptions %v", g.PageSize, g.PageSizeOptions)
	}
	seen := map[string]bool{}
	for _, c := range g.Columns {
		if seen[c.Key] {
			return fmt.Errorf("duplicate column %q", c.Key)
		}
		seen[c.Key] = true
		if c.Searchable && c.Type != TypeString {
			return fmt.Errorf("column %q: searchable requires type string", c.Key)
		}
		if c.Type == TypeEnum && len(c.Enum) == 0 {
			return fmt.Errorf("column %q: enum type requires Enum values", c.Key)
		}
		if c.Type == TypeJSON && c.Sortable {
			return fmt.Errorf("column %q: json is not sortable", c.Key)
		}
		if c.Array {
			if c.Type == TypeJSON {
				return fmt.Errorf("column %q: json cannot be an array", c.Key)
			}
			if c.Sortable {
				return fmt.Errorf("column %q: array columns are not sortable", c.Key)
			}
		}
		if c.Step != 0 {
			if c.Type != TypeTime && c.Type != TypeDatetime {
				return fmt.Errorf("column %q: step is only valid for time and datetime", c.Key)
			}
			if c.Step < time.Second || c.Step%time.Second != 0 || (24*time.Hour)%c.Step != 0 {
				return fmt.Errorf("column %q: step %v must be whole seconds dividing a day", c.Key, c.Step)
			}
		}
		if c.Filter != nil {
			if len(c.operators()) == 0 {
				return fmt.Errorf("column %q: filter with no operators", c.Key)
			}
			for _, op := range c.Filter.Operators {
				if !opAllowed(c, op) {
					return fmt.Errorf("column %q: op %q not allowed for type %q", c.Key, op, c.Type)
				}
			}
		}
	}
	if _, ok := g.Column(g.IDColumn); !ok {
		return fmt.Errorf("idColumn %q not found", g.IDColumn)
	}
	ds, ok := g.Column(g.DefaultSort.Column)
	if !ok || !ds.Sortable || (g.DefaultSort.Dir != "asc" && g.DefaultSort.Dir != "desc") {
		return fmt.Errorf("defaultSort %+v invalid", g.DefaultSort)
	}
	return nil
}
