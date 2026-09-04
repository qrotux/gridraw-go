package gridraw

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func stubTranslator(_, key string) string { return key }

func enLocale(*http.Request) string { return "en" }

func newTestHandler(t *testing.T, grids ...Grid) *Handler {
	t.Helper()
	if len(grids) == 0 {
		grids = []Grid{validTestGrid()}
	}
	reg, err := NewRegistry(nopCompiler{}, grids...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return NewHandler(Options{Registry: reg, Translator: stubTranslator, Locale: enLocale, Compiler: nopCompiler{}})
}

func TestHandlerDescriptor200(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/grids/t", nil)
	rec := httptest.NewRecorder()
	h.Descriptor(rec, req, "t")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var d Descriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal descriptor: %v; body=%s", err, rec.Body.String())
	}
	if d.Name != "t" {
		t.Errorf("descriptor.Name = %q, want %q", d.Name, "t")
	}
}

func TestHandlerUnknownGrid404(t *testing.T) {
	h := newTestHandler(t)
	for _, call := range []struct {
		name string
		do   func(rec *httptest.ResponseRecorder)
	}{
		{"descriptor", func(rec *httptest.ResponseRecorder) {
			h.Descriptor(rec, httptest.NewRequest(http.MethodGet, "/grids/nope", nil), "nope")
		}},
		{"rows", func(rec *httptest.ResponseRecorder) {
			h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/nope/rows", bytes.NewBufferString(`{}`)), "nope")
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			call.do(rec)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if body["error"] != "unknown grid" {
				t.Errorf("error = %q, want %q", body["error"], "unknown grid")
			}
		})
	}
}

func TestHandlerRowsBadRequest400(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/t/rows", bytes.NewBufferString(`{"filters":[[]]}`)), "t")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	want := `{"error":"filter group 0 is empty"}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestHandlerRowsBadJSON400(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/t/rows", bytes.NewBufferString(`{not json`)), "t")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("error message empty, want non-empty invalid-JSON message")
	}
}

func TestHandlerRowsNoExecutor500(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/t/rows", bytes.NewBufferString(`{"columns":["email"]}`)), "t")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if want := `{"error":"query failed"}` + "\n"; rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

type fakeExecutor struct {
	rows  []map[string]any
	total int64
	keys  []string
}

func (f *fakeExecutor) Rows(_ context.Context, _ string, _ []any, keys []string) ([]map[string]any, error) {
	f.keys = keys
	return f.rows, nil
}
func (f *fakeExecutor) Count(context.Context, string, []any) (int64, error) { return f.total, nil }

func TestHandlerRows200(t *testing.T) {
	reg, err := NewRegistry(nopCompiler{}, validTestGrid())
	if err != nil {
		t.Fatal(err)
	}
	ex := &fakeExecutor{rows: []map[string]any{{"email": "a@b", "id": "1"}}, total: 7}
	h := NewHandler(Options{Registry: reg, Translator: stubTranslator, Locale: enLocale, Compiler: nopCompiler{}, Executor: ex})
	rec := httptest.NewRecorder()
	h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/t/rows", bytes.NewBufferString(`{"columns":["email"]}`)), "t")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var resp RowsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 7 || len(resp.Rows) != 1 || resp.Rows[0]["email"] != "a@b" {
		t.Errorf("resp = %+v", resp)
	}
	if len(ex.keys) != 2 || ex.keys[0] != "email" || ex.keys[1] != "id" {
		t.Errorf("executor keys = %v, want [email id]", ex.keys)
	}
}

func TestForContextResolvesPerRequest(t *testing.T) {
	type ctxKey struct{}
	base := validTestGrid()
	base.ForContext = func(ctx context.Context) Grid {
		g := base
		if v, _ := ctx.Value(ctxKey{}).(string); v == "swap" {
			g.DefaultSort = SortSpec{Column: "id", Dir: "asc"}
		}
		return g
	}
	reg, err := NewRegistry(nopCompiler{}, base)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := reg.Get("t")

	if got := BuildDescriptor(g.Resolve(context.Background()), stubTranslator, "en").DefaultSort.Dir; got != "desc" {
		t.Errorf("plain ctx: dir=%q, want desc", got)
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "swap")
	if got := BuildDescriptor(g.Resolve(ctx), stubTranslator, "en").DefaultSort.Dir; got != "asc" {
		t.Errorf("swapped ctx: dir=%q, want asc", got)
	}
}

// Both endpoints must resolve ForContext: the descriptor shows the resolved
// DefaultSort, and rows answers 400 for a column the resolved grid lacks
// (the static grid has it, so a regression would reach the executor and 500).
func TestForContextResolvesPerRequestHTTP(t *testing.T) {
	base := validTestGrid()
	var resolvedColumns []Column
	for _, c := range base.Columns {
		if c.Key == "rating" {
			continue
		}
		resolvedColumns = append(resolvedColumns, c)
	}
	base.ForContext = func(ctx context.Context) Grid {
		g := base
		g.DefaultSort = SortSpec{Column: "id", Dir: "asc"}
		g.Columns = resolvedColumns
		return g
	}
	h := newTestHandler(t, base)

	t.Run("descriptor sees resolved DefaultSort", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Descriptor(rec, httptest.NewRequest(http.MethodGet, "/grids/t", nil), "t")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var d Descriptor
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatalf("unmarshal descriptor: %v; body=%s", err, rec.Body.String())
		}
		if d.DefaultSort.Dir != "asc" {
			t.Errorf("defaultSort.dir = %q, want %q (resolved grid not applied)", d.DefaultSort.Dir, "asc")
		}
	})

	t.Run("rows sees resolved column set", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.Rows(rec, httptest.NewRequest(http.MethodPost, "/grids/t/rows", bytes.NewBufferString(`{"columns":["rating"]}`)), "t")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d (resolved grid not applied); body=%s",
				rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal error body: %v", err)
		}
		if body["error"] != `unknown column "rating"` {
			t.Errorf("error = %q, want %q", body["error"], `unknown column "rating"`)
		}
	})
}

func TestHandlerList(t *testing.T) {
	other := validTestGrid()
	other.Name = "a"
	other.Description = "First"
	h := newTestHandler(t, validTestGrid(), other)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/grids/-/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got []GridInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	want := []GridInfo{{Name: "a", Description: "First"}, {Name: "t"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("list = %+v, want %+v", got, want)
	}
	if strings.Contains(rec.Body.String(), `{"name":"t","description"`) {
		t.Errorf("a grid without a description must omit it: %s", rec.Body.String())
	}
}

func TestHandlerCatalog(t *testing.T) {
	g := validTestGrid()
	g.Description = "Test grid"
	h := newTestHandler(t, g)

	rec := httptest.NewRecorder()
	h.Catalog(rec, httptest.NewRequest(http.MethodGet, "/grids/-/registry", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got []GridEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(got) != 1 || got[0].Name != "t" || got[0].Description != "Test grid" {
		t.Fatalf("registry = %+v", got)
	}
	if len(got[0].Columns) != len(g.Columns) {
		t.Fatalf("columns = %d, want %d", len(got[0].Columns), len(g.Columns))
	}
	if c := got[0].Columns[0]; c.Key != "id" || c.Type != TypeUUID || c.Title != "grid.t.id" {
		t.Errorf("id column = %+v", c)
	}
}
