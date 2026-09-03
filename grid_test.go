package gridraw

import (
	"strings"
	"testing"
)

// nopCompiler accepts every binding; core tests pin the structural checks only.
type nopCompiler struct{}

func (nopCompiler) Validate(*Grid) error               { return nil }
func (nopCompiler) Compile(*Query) (Statements, error) { return Statements{}, nil }

func validTestGrid() Grid {
	return Grid{
		Name: "t", IDColumn: "id", PageSize: 25,
		DefaultSort: SortSpec{Column: "email", Dir: "desc"},
		Columns: []Column{
			{Key: "id", Type: TypeString, Sortable: true},
			{Key: "email", Type: TypeString, Sortable: true, Searchable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpContains}}},
			{Key: "rating", Type: TypeNumber, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpGte, OpLte, OpBetween}}},
			{Key: "createdAt", Type: TypeDatetime, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpGte, OpLte, OpBetween}}},
			{Key: "isBanned", Type: TypeBool, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq}}},
			{Key: "role", Type: TypeEnum, Enum: []string{"user", "admin"},
				Filter: &FilterSpec{Operators: []Op{OpIn}}},
		},
	}
}

func TestNewRegistryValid(t *testing.T) {
	r, err := NewRegistry(nopCompiler{}, validTestGrid())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("t"); !ok {
		t.Fatal("grid not registered")
	}
	if names := r.Names(); len(names) != 1 || names[0] != "t" {
		t.Errorf("Names = %v", names)
	}
}

func TestNewRegistryRequiresCompiler(t *testing.T) {
	if _, err := NewRegistry(nil, validTestGrid()); err == nil {
		t.Fatal("nil compiler accepted")
	}
}

type failCompiler struct{}

func (failCompiler) Validate(g *Grid) error             { return errFail }
func (failCompiler) Compile(*Query) (Statements, error) { return Statements{}, nil }

var errFail = &ReqError{Msg: "binding rejected"}

func TestNewRegistryRunsCompilerValidate(t *testing.T) {
	_, err := NewRegistry(failCompiler{}, validTestGrid())
	if err == nil || !strings.Contains(err.Error(), "binding rejected") {
		t.Fatalf("err=%v, want compiler validation error", err)
	}
}

func TestNewRegistryRejectsDuplicateName(t *testing.T) {
	_, err := NewRegistry(nopCompiler{}, validTestGrid(), validTestGrid())
	if err == nil || !strings.Contains(err.Error(), "duplicate grid name") {
		t.Fatalf("err=%v, want contains %q", err, "duplicate grid name")
	}
}

func TestNewRegistryRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Grid)
		want string
	}{
		{"empty name", func(g *Grid) { g.Name = "" }, "name required"},
		{"dup key", func(g *Grid) { g.Columns = append(g.Columns, g.Columns[1]) }, "duplicate"},
		{"bad id", func(g *Grid) { g.IDColumn = "nope" }, "idColumn"},
		{"searchable non-string", func(g *Grid) { g.Columns[1].Type = TypeNumber; g.Columns[1].Filter = nil }, "searchable"},
		{"enum empty", func(g *Grid) {
			g.Columns[1].Type = TypeEnum
			g.Columns[1].Searchable = false
			g.Columns[1].Filter = nil
		}, "enum"},
		{"bad op", func(g *Grid) { g.Columns[1].Filter.Operators = []Op{OpGte} }, "not allowed"},
		{"bad defaultSort", func(g *Grid) { g.DefaultSort.Column = "nope" }, "defaultSort"},
		{"non-sortable defaultSort", func(g *Grid) { g.Columns[1].Sortable = false }, "defaultSort"},
		{"bad pageSize", func(g *Grid) { g.PageSize = 0 }, "pageSize"},
		{"bad pageSize hi", func(g *Grid) { g.PageSize = 101 }, "pageSize"},
		{"pageSizeOption out of range", func(g *Grid) { g.PageSizeOptions = []int{10, 200} }, "pageSizeOption"},
		{"pageSize not in options", func(g *Grid) { g.PageSizeOptions = []int{10, 50} }, "not in pageSizeOptions"},
		{"empty pageSizeOptions", func(g *Grid) { g.PageSizeOptions = []int{} }, "non-empty"},
	}
	for _, tc := range cases {
		g := validTestGrid()
		tc.mut(&g)
		_, err := NewRegistry(nopCompiler{}, g)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want contains %q", tc.name, err, tc.want)
		}
	}
}

func TestTypeJSONRegisters(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, Column{Key: "payload", Type: TypeJSON})
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatalf("NewRegistry() error: %v", err)
	}
}

// Every operator is rejected on TypeJSON; the operator list must be
// non-empty because an empty list passes validation vacuously.
func TestTypeJSONRejectsFilter(t *testing.T) {
	for _, op := range []Op{OpEq, OpContains, OpIn, OpGte} {
		g := validTestGrid()
		g.Columns = append(g.Columns, Column{
			Key: "payload", Type: TypeJSON,
			Filter: &FilterSpec{Operators: []Op{op}},
		})
		_, err := NewRegistry(nopCompiler{}, g)
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("op %q: err=%v, want contains %q", op, err, "not allowed")
		}
	}
}

func TestTypeJSONRejectsSearchable(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, Column{Key: "payload", Type: TypeJSON, Searchable: true})
	_, err := NewRegistry(nopCompiler{}, g)
	if err == nil || !strings.Contains(err.Error(), "searchable") {
		t.Fatalf("err=%v, want contains %q", err, "searchable")
	}
}
