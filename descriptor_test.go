package gridraw

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDescriptorFilterWidget(t *testing.T) {
	g := validTestGrid()
	*col(&g, "role") = col(&g, "role").FilterWidget(WidgetTags)
	byKey := map[string]ColumnDesc{}
	for _, c := range BuildDescriptor(&g, stubTranslator, "en").Columns {
		byKey[c.Key] = c
	}
	if byKey["role"].Filter.Widget != WidgetTags {
		t.Errorf("role widget = %q, want %q", byKey["role"].Filter.Widget, WidgetTags)
	}
	if raw, _ := json.Marshal(byKey["email"].Filter); strings.Contains(string(raw), `"widget"`) {
		t.Errorf("column without a widget must omit it: %s", raw)
	}
}

// A column with an empty operator list publishes the full set of its type in
// opsByType order, and an array column the array set.
func TestDescriptorEmptyOperatorsMeanAll(t *testing.T) {
	g := validTestGrid()
	g.Columns = append(g.Columns,
		Column{Key: "label", Type: TypeString, Filter: &FilterSpec{}},
		Column{Key: "ids", Type: TypeUUID, Array: true, Filter: &FilterSpec{}})
	byKey := map[string]ColumnDesc{}
	for _, c := range BuildDescriptor(&g, stubTranslator, "en").Columns {
		byKey[c.Key] = c
	}
	var got []Op
	for _, op := range byKey["label"].Filter.Operators {
		got = append(got, op.Op)
	}
	want := []Op{OpEq, OpNeq, OpContains, OpNotContains, OpStarts, OpEnds}
	if !equalOps(got, want) {
		t.Errorf("label operators = %v, want %v", got, want)
	}
	got = nil
	for _, op := range byKey["ids"].Filter.Operators {
		got = append(got, op.Op)
	}
	if !equalOps(got, arrayOps) {
		t.Errorf("ids operators = %v, want %v", got, arrayOps)
	}
}

func TestDescriptorArray(t *testing.T) {
	g := validTestGrid()
	byKey := map[string]ColumnDesc{}
	for _, c := range BuildDescriptor(&g, stubTranslator, "en").Columns {
		byKey[c.Key] = c
	}
	if c := byKey["tags"]; !c.Array || c.Type != TypeString || c.Sortable {
		t.Errorf("tags = %+v, want array of string, not sortable", c)
	}
	if c := byKey["slots"]; !c.Array || c.Step != 900 {
		t.Errorf("slots = %+v, want array with step 900", c)
	}
	if raw, _ := json.Marshal(byKey["email"]); strings.Contains(string(raw), `"array"`) {
		t.Errorf("scalar column must not carry array: %s", raw)
	}
}

func TestDescriptorStep(t *testing.T) {
	g := validTestGrid()
	g.Columns[4].Step = 15 * time.Minute // opensAt
	d := BuildDescriptor(&g, stubTranslator, "en")
	byKey := map[string]ColumnDesc{}
	for _, c := range d.Columns {
		byKey[c.Key] = c
	}
	if byKey["opensAt"].Step != 900 {
		t.Errorf("opensAt.step = %d, want 900", byKey["opensAt"].Step)
	}
	if byKey["createdAt"].Step != 1 {
		t.Errorf("createdAt.step = %d, want 1 (default)", byKey["createdAt"].Step)
	}
	raw, _ := json.Marshal(byKey["birthday"])
	if strings.Contains(string(raw), `"step"`) {
		t.Errorf("date column must not carry step: %s", raw)
	}
}

func TestBuildDescriptor(t *testing.T) {
	g := validTestGrid()
	tr := func(_, key string) string { return "T:" + key }

	d := BuildDescriptor(&g, tr, "ru")

	var emailCol, idCol, roleCol *ColumnDesc
	for i := range d.Columns {
		switch d.Columns[i].Key {
		case "email":
			emailCol = &d.Columns[i]
		case "id":
			idCol = &d.Columns[i]
		case "role":
			roleCol = &d.Columns[i]
		}
	}
	if emailCol == nil || idCol == nil || roleCol == nil {
		t.Fatalf("missing expected columns: email=%v id=%v role=%v", emailCol, idCol, roleCol)
	}

	if emailCol.Title != "T:grid.t.email" {
		t.Errorf("email title = %q, want %q", emailCol.Title, "T:grid.t.email")
	}

	if roleCol.Filter == nil {
		t.Fatal("role column: Filter is nil, want non-nil")
	}
	found := false
	for _, ev := range roleCol.Filter.EnumValues {
		if ev.Value == "admin" {
			found = true
			if want := "T:grid.t.role_values.admin"; ev.Label != want {
				t.Errorf("role admin enum label = %q, want %q", ev.Label, want)
			}
		}
	}
	if !found {
		t.Fatal("role column: enumValues missing \"admin\"")
	}

	containsFound := false
	for _, op := range emailCol.Filter.Operators {
		if op.Op == OpContains {
			containsFound = true
			if want := "T:grid.operators.contains"; op.Label != want {
				t.Errorf("contains operator label = %q, want %q", op.Label, want)
			}
		}
	}
	if !containsFound {
		t.Fatal("email column: operators missing \"contains\"")
	}

	if len(d.PageSizeOptions) != 4 || d.PageSizeOptions[0] != 10 || d.PageSizeOptions[3] != 100 {
		t.Errorf("PageSizeOptions = %v, want default [10 25 50 100]", d.PageSizeOptions)
	}

	if d.Search == nil {
		t.Fatal("Search is nil, want non-nil (grid has a Searchable column)")
	}
	searchHasEmail := false
	for _, title := range d.Search.Columns {
		if title == emailCol.Title {
			searchHasEmail = true
		}
	}
	if !searchHasEmail {
		t.Errorf("Search.Columns = %v, want to contain %q", d.Search.Columns, emailCol.Title)
	}

	if idCol.Filter == nil || len(idCol.Filter.Operators) != 4 {
		t.Errorf("id column: Filter = %+v, want the four uuid operators", idCol.Filter)
	}
	g.Columns = append(g.Columns, Column{Key: "note", Type: TypeString})
	var noteCol ColumnDesc
	for _, c := range BuildDescriptor(&g, tr, "ru").Columns {
		if c.Key == "note" {
			noteCol = c
		}
	}
	noteJSON, err := json.Marshal(noteCol)
	if err != nil {
		t.Fatalf("marshal note column: %v", err)
	}
	if strings.Contains(string(noteJSON), `"filter"`) {
		t.Errorf("non-filterable column JSON contains \"filter\" key, want omitted: %s", noteJSON)
	}
}

func TestBuildDescriptorSearchNilWithoutSearchable(t *testing.T) {
	g := validTestGrid()
	for i := range g.Columns {
		g.Columns[i].Searchable = false
	}
	tr := func(_, key string) string { return "T:" + key }

	d := BuildDescriptor(&g, tr, "ru")
	if d.Search != nil {
		t.Errorf("Search = %+v, want nil when no Searchable columns", d.Search)
	}
}

// Descriptions come from the grid definition, and a translation of the
// description key wins over the literal.
func TestDescriptorDescription(t *testing.T) {
	g := validTestGrid()
	g.Description = "Test grid"
	*col(&g, "email") = col(&g, "email").WithDescription("Login email")

	d := BuildDescriptor(&g, stubTranslator, "en")
	byKey := map[string]ColumnDesc{}
	for _, c := range d.Columns {
		byKey[c.Key] = c
	}
	if d.Description != "Test grid" {
		t.Errorf("grid description = %q, want %q", d.Description, "Test grid")
	}
	if byKey["email"].Description != "Login email" {
		t.Errorf("email description = %q, want %q", byKey["email"].Description, "Login email")
	}
	if raw, _ := json.Marshal(byKey["rating"]); strings.Contains(string(raw), `"description"`) {
		t.Errorf("column without a description must omit it: %s", raw)
	}

	tr := func(_, key string) string {
		switch key {
		case "grid.t.description":
			return "Тест"
		case "grid.t.email.description":
			return "Почта"
		}
		return key // the house convention: a miss echoes the key back
	}
	d = BuildDescriptor(&g, tr, "ru")
	if d.Description != "Тест" {
		t.Errorf("translated grid description = %q, want %q", d.Description, "Тест")
	}
	for _, c := range d.Columns {
		if c.Key == "email" && c.Description != "Почта" {
			t.Errorf("translated email description = %q, want %q", c.Description, "Почта")
		}
		if c.Key == "rating" && c.Description != "" {
			t.Errorf("rating description = %q, want empty (key echo is a miss)", c.Description)
		}
	}
}

func TestBuildGridEntry(t *testing.T) {
	g := validTestGrid()
	g.Description = "Test grid"
	*col(&g, "email") = col(&g, "email").WithDescription("Login email")

	e := BuildGridEntry(&g, stubTranslator, "en")
	if e.Name != "t" || e.Description != "Test grid" || len(e.Columns) != len(g.Columns) {
		t.Fatalf("entry = %+v", e)
	}
	if c := e.Columns[1]; c.Key != "email" || c.Type != TypeString || c.Title != "grid.t.email" || c.Description != "Login email" {
		t.Errorf("email column = %+v", c)
	}
	if raw, _ := json.Marshal(e.Columns[2]); strings.Contains(string(raw), `"description"`) {
		t.Errorf("column without a description must omit it: %s", raw)
	}
}

// skipTotal reaches the client so it can drop the page numbers; a counting
// grid must not carry the key at all.
func TestDescriptorSkipTotal(t *testing.T) {
	g := validTestGrid()
	if raw, _ := json.Marshal(BuildDescriptor(&g, stubTranslator, "en")); strings.Contains(string(raw), `"skipTotal"`) {
		t.Errorf("a counting grid must omit skipTotal: %s", raw)
	}
	g.SkipTotal = true
	if d := BuildDescriptor(&g, stubTranslator, "en"); !d.SkipTotal {
		t.Errorf("descriptor.SkipTotal = false, want true")
	}
}
