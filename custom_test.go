package gridraw

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// parseRaw is the identity CustomOps.Parse of the core tests.
func parseRaw(_ Op, raw any) (any, any, error) { return raw, nil, nil }

// A custom type is filterable only through its CustomOps and can still sort.
func TestCustomOpsOnUnknownType(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, Column{
		Key: "geo", Type: "point", Sortable: true, Filter: &FilterSpec{Widget: "map"},
		Custom: &CustomOps{
			Operators: []Op{"within"},
			Parse: func(op Op, raw any) (any, any, error) {
				m, ok := raw.(map[string]any)
				if !ok {
					return nil, nil, errors.New("object expected")
				}
				return m["km"], nil, nil
			},
		},
	})
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatal(err)
	}

	q, rerr := BuildQuery(&g, RowsRequest{
		Columns: []string{"geo"},
		Filters: [][]FilterClause{{{Field: "geo", Op: "within", Value: map[string]any{"km": 5.0}}}},
		Sort:    []SortSpec{{Column: "geo", Dir: "asc"}},
	})
	if rerr != nil {
		t.Fatal(rerr)
	}
	if c := q.Groups[0][0]; c.Op != "within" || c.Value != 5.0 || c.Value2 != nil {
		t.Errorf("clause = %+v, want within/5", c)
	}

	_, rerr = BuildQuery(&g, RowsRequest{
		Columns: []string{"geo"},
		Filters: [][]FilterClause{{{Field: "geo", Op: "within", Value: "x"}}},
	})
	if rerr == nil || rerr.Status != 400 || !strings.Contains(rerr.Msg, `field "geo": object expected`) {
		t.Errorf("parse error = %v, want 400 with the Parse text", rerr)
	}
	_, rerr = BuildQuery(&g, RowsRequest{
		Columns: []string{"geo"},
		Filters: [][]FilterClause{{{Field: "geo", Op: OpEq, Value: "x"}}},
	})
	if rerr == nil || !strings.Contains(rerr.Msg, "not allowed") {
		t.Errorf("built-in op on a custom type = %v, want not allowed", rerr)
	}

	desc := BuildDescriptor(&g, stubTranslator, "en")
	last := desc.Columns[len(desc.Columns)-1]
	if last.Type != "point" || last.Filter == nil || last.Filter.Widget != "map" ||
		len(last.Filter.Operators) != 1 || last.Filter.Operators[0].Op != "within" {
		t.Errorf("descriptor = %+v, want point column with the within operator", last)
	}
}

// A custom operator with a built-in name takes that operator over: eq on a
// string column then reaches Parse instead of the string conversion, and the
// other built-in operators stay.
func TestCustomOpsOverrideBuiltIn(t *testing.T) {
	g := validTestGrid()
	g.Columns[1].Filter = &FilterSpec{} // email: full default set
	g.Columns[1].Custom = &CustomOps{
		Operators: []Op{OpEq, OpIn},
		Parse: func(op Op, raw any) (any, any, error) {
			return "parsed:" + raw.(string), nil, nil
		},
	}
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatal(err)
	}

	q, rerr := BuildQuery(&g, RowsRequest{
		Columns: []string{"email"},
		Filters: [][]FilterClause{{
			{Field: "email", Op: OpEq, Value: "A"},
			{Field: "email", Op: OpIn, Value: "B"},
			{Field: "email", Op: OpContains, Value: "c"},
		}},
	})
	if rerr != nil {
		t.Fatal(rerr)
	}
	if got := q.Groups[0]; got[0].Value != "parsed:A" || got[1].Value != "parsed:B" || got[2].Value != "c" {
		t.Errorf("values = %v %v %v, want custom parse for eq and in only", got[0].Value, got[1].Value, got[2].Value)
	}

	var got []Op
	for _, op := range BuildDescriptor(&g, stubTranslator, "en").Columns[1].Filter.Operators {
		got = append(got, op.Op)
	}
	want := []Op{OpEq, OpNeq, OpContains, OpNotContains, OpStarts, OpEnds, OpIn}
	if !equalOps(got, want) {
		t.Errorf("operators = %v, want built-ins then the new custom ones, no duplicate eq", got)
	}

	if got := g.Columns[1].Nullable().operators(); !equalOps(got, append(want, OpIsNull, OpIsNotNull)) {
		t.Errorf("Nullable operators = %v, want custom ones kept", got)
	}
}

// The Custom flag marks only custom-parsed clauses; a built-in operator,
// including a stepped eq that BuildQuery widens to between, stays built-in so
// a compiler never routes it to a custom renderer. Parse may set Value2.
func TestClauseCustomFlag(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns, Column{
		Key: "span", Type: "range", Sortable: true, Filter: &FilterSpec{},
		Custom: &CustomOps{
			Operators: []Op{"within"},
			Parse:     func(_ Op, raw any) (any, any, error) { return "lo", "hi", nil },
		},
	})
	// opensAt is a time column offering built-in between; step it to 15m so
	// BuildQuery widens a built-in eq to between.
	g.Columns[4].Step = 15 * time.Minute
	g.Columns[4].Custom = &CustomOps{Operators: []Op{"near"},
		Parse: func(_ Op, raw any) (any, any, error) { return raw, nil, nil }}
	if _, err := NewRegistry(nopCompiler{}, g); err != nil {
		t.Fatal(err)
	}

	q, rerr := BuildQuery(&g, RowsRequest{
		Columns: []string{"span"},
		Filters: [][]FilterClause{{
			{Field: "span", Op: "within", Value: nil},
			{Field: "opensAt", Op: OpEq, Value: "09:15:00"}, // built-in, stepped → widened
		}},
	})
	if rerr != nil {
		t.Fatal(rerr)
	}
	within, stepped := q.Groups[0][0], q.Groups[0][1]
	if !within.Custom || within.Value != "lo" || within.Value2 != "hi" {
		t.Errorf("custom clause = %+v, want Custom with lo/hi", within)
	}
	if stepped.Op != OpBetween || stepped.Custom || !stepped.UpperOpen {
		t.Errorf("stepped eq = %+v, want built-in between, Custom=false", stepped)
	}
}
