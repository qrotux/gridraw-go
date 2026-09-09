package gridraw

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestColumnConveniences(t *testing.T) {
	g := validTestGrid()
	original := g.Columns[2]
	ops := []Op{OpGte, OpLte}
	g.Columns[2] = original.FilterWidget(WidgetTags).WithOperators(ops...).WithoutSort()
	ops[0] = OpContains
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatal(err)
	}
	d := BuildDescriptor(&g, stubTranslator, "en").Columns[2]
	if d.Sortable || d.Filter.Widget != WidgetTags || len(d.Filter.Operators) != 2 || d.Filter.Operators[0].Op != OpGte {
		t.Fatalf("modified descriptor: %+v", d)
	}
	if len(original.Filter.Operators) != 5 || !original.Sortable {
		t.Fatal("modifier mutated original")
	}
	_, err := BuildQuery(&g, RowsRequest{Filters: [][]FilterClause{{{Field: "rating", Op: OpEq, Value: 1.0}}}})
	if err == nil {
		t.Fatal("restricted operator accepted")
	}
	g.Columns[2] = g.Columns[2].WithoutFilter()
	if BuildDescriptor(&g, nil, "").Columns[2].Filter != nil {
		t.Fatal("filter still advertised")
	}
	_, err = BuildQuery(&g, RowsRequest{Filters: [][]FilterClause{{{Field: "rating", Op: OpGte, Value: 1.0}}}})
	if err == nil {
		t.Fatal("disabled filter accepted")
	}
	g.Columns[2] = original.WithOperators()
	if len(BuildDescriptor(&g, nil, "").Columns[2].Filter.Operators) != 8 {
		t.Fatal("empty operators must mean all")
	}
}

func TestTitleFallbackTranslator(t *testing.T) {
	g := validTestGrid()
	for _, tc := range []struct {
		name      string
		tr        Translator
		email, id string
	}{
		{"no translator", nil, "Email", "grid.t.id"},
		{"legacy key echo", stubTranslator, "Email", "grid.t.id"},
		{"legacy empty", func(_, _ string) string { return "" }, "Email", ""},
		{"translation", func(_, key string) string { return "translated:" + key }, "translated:grid.t.email", "translated:grid.t.id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			titles := map[string]string{"grid.t.email": "Email"}
			tr := WithTitles(tc.tr, titles)
			titles["grid.t.email"] = "changed"
			d := BuildDescriptor(&g, tr, "")
			e := BuildGridEntry(&g, tr, "")
			if d.Columns[1].Title != tc.email || e.Columns[1].Title != tc.email || d.Columns[0].Title != tc.id || d.Search.Columns[0] != tc.email {
				t.Fatalf("titles: descriptor=%+v catalog=%+v", d, e)
			}
		})
	}
	if got := BuildGridInfo(&g, nil, ""); got.Name != "t" {
		t.Fatal(got)
	}
}

func TestHandlerOptionalLocalization(t *testing.T) {
	reg, err := NewRegistry(nopCompiler{}, validTestGrid())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Options{Registry: reg})
	w := httptest.NewRecorder()
	h.Descriptor(w, httptest.NewRequest("GET", "/t", nil), "t")
	var d Descriptor
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || d.Columns[1].Title != "email" || d.Columns[1].Filter.Operators[0].Label != "eq" {
		t.Fatalf("response: %s", w.Body.String())
	}
}
