package gridraw

import (
	"encoding/json"
	"strings"
	"testing"
)

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

	if idCol.Filter != nil {
		t.Fatal("id column: Filter is non-nil, want nil (id has no FilterSpec)")
	}
	idJSON, err := json.Marshal(idCol)
	if err != nil {
		t.Fatalf("marshal id column: %v", err)
	}
	if strings.Contains(string(idJSON), `"filter"`) {
		t.Errorf("id column JSON contains \"filter\" key, want omitted: %s", idJSON)
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
