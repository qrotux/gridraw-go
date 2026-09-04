package gridraw

import (
	"strings"
	"testing"
	"time"
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
			{Key: "id", Type: TypeUUID, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpNeq, OpIn, OpNotIn}}},
			{Key: "email", Type: TypeString, Sortable: true, Searchable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpNeq, OpContains, OpNotContains, OpEnds, OpIsNull, OpIsNotNull}}},
			{Key: "rating", Type: TypeNumber, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpGte, OpLte, OpBetween, OpNotBetween}}},
			{Key: "createdAt", Type: TypeDatetime, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpBetween}}},
			{Key: "opensAt", Type: TypeTime, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpLt, OpBetween, OpNotBetween}}},
			{Key: "birthday", Type: TypeDate, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpNeq, OpGt, OpLt, OpBetween, OpNotBetween, OpIsNull}}},
			{Key: "isBanned", Type: TypeBool, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq}}},
			{Key: "role", Type: TypeEnum, Enum: []string{"user", "admin"},
				Filter: &FilterSpec{Operators: []Op{OpIn, OpNotIn}}},
			{Key: "tags", Type: TypeString, Array: true, Searchable: true,
				Filter: &FilterSpec{Operators: []Op{OpContainsAny, OpContainsAll, OpContainsOnly, OpNotContainsAny, OpIsEmpty, OpIsNotEmpty, OpIsNull}}},
			{Key: "slots", Type: TypeTime, Array: true, Step: 15 * time.Minute,
				Filter: &FilterSpec{Operators: []Op{OpContainsAny}}},
			{Key: "price", Type: TypeDecimal, Sortable: true,
				Filter: &FilterSpec{Operators: []Op{OpEq, OpGt, OpBetween, OpNotBetween}}},
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

func TestStepAccepted(t *testing.T) {
	for _, d := range []time.Duration{time.Second, 30 * time.Second, time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour} {
		g := validTestGrid()
		g.Columns[4].Step = d // opensAt
		g.Columns[3].Step = d // createdAt
		if _, err := NewRegistry(nopCompiler{}, g); err != nil {
			t.Errorf("step %v: %v", d, err)
		}
	}
}

// col returns a pointer to the column with the given key for in-place mutation.
func col(g *Grid, key string) *Column {
	for i := range g.Columns {
		if g.Columns[i].Key == key {
			return &g.Columns[i]
		}
	}
	panic("no column " + key)
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
		{"notIn on string", func(g *Grid) { g.Columns[1].Filter.Operators = []Op{OpNotIn} }, "not allowed"},
		{"contains on uuid", func(g *Grid) { g.Columns[0].Filter.Operators = []Op{OpContains} }, "not allowed"},
		{"searchable uuid", func(g *Grid) { g.Columns[0].Searchable = true }, "searchable requires type string"},
		{"contains on datetime", func(g *Grid) { g.Columns[3].Filter.Operators = []Op{OpContains} }, "not allowed"},
		{"contains on date", func(g *Grid) { g.Columns[5].Filter.Operators = []Op{OpContains} }, "not allowed"},
		{"starts on time", func(g *Grid) { g.Columns[4].Filter.Operators = []Op{OpStarts} }, "not allowed"},
		{"scalar op on array", func(g *Grid) { col(g, "tags").Filter.Operators = []Op{OpEq} }, "not allowed"},
		{"array op on scalar", func(g *Grid) { g.Columns[1].Filter.Operators = []Op{OpContainsAny} }, "not allowed"},
		{"sortable array", func(g *Grid) { col(g, "tags").Sortable = true }, "not sortable"},
		{"json array", func(g *Grid) {
			c := col(g, "tags")
			c.Type, c.Searchable, c.Filter = TypeJSON, false, nil
		}, "json cannot be an array"},
		{"searchable non-string array", func(g *Grid) { c := col(g, "tags"); c.Type, c.Filter = TypeNumber, nil }, "searchable"},
		{"widget without operators", func(g *Grid) { c := col(g, "tags"); c.Filter = &FilterSpec{Widget: WidgetTags} }, "widget set without operators"},
		{"sortable json", func(g *Grid) { g.Columns = append(g.Columns, Column{Key: "j", Type: TypeJSON, Sortable: true}) }, "json is not sortable"},
		{"contains on decimal", func(g *Grid) { g.Columns[len(g.Columns)-1].Filter.Operators = []Op{OpContains} }, "not allowed"},
		{"notBetween on string", func(g *Grid) { g.Columns[1].Filter.Operators = []Op{OpNotBetween} }, "not allowed"},
		{"step on date", func(g *Grid) { g.Columns[5].Step = time.Minute }, "step is only valid"},
		{"step not whole seconds", func(g *Grid) { g.Columns[4].Step = 1500 * time.Millisecond }, "whole seconds"},
		{"step not dividing a day", func(g *Grid) { g.Columns[4].Step = 7 * time.Minute }, "dividing a day"},
		{"step longer than a day", func(g *Grid) { g.Columns[4].Step = 25 * time.Hour }, "dividing a day"},
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

// isNull/isNotNull are the only operators a json column accepts.
func TestTypeJSONAcceptsNullOps(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, Column{Key: "prefs", Type: TypeJSON,
		Filter: &FilterSpec{Operators: []Op{OpIsNull, OpIsNotNull}}})
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatal(err)
	}
}

// Every other operator is rejected on TypeJSON; the operator list must be
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
