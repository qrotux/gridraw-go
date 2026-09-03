package grstd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qrotux/gridraw-go"
)

type nopCompiler struct{}

func (nopCompiler) Validate(*gridraw.Grid) error { return nil }
func (nopCompiler) Compile(*gridraw.Query) (gridraw.Statements, error) {
	return gridraw.Statements{}, nil
}

func testGrid() gridraw.Grid {
	return gridraw.Grid{
		Name: "t", IDColumn: "id", PageSize: 25,
		DefaultSort: gridraw.SortSpec{Column: "id", Dir: "asc"},
		Columns:     []gridraw.Column{{Key: "id", Type: gridraw.TypeString, Sortable: true}},
	}
}

// passGuard proves Register wraps both routes: it only stamps a header.
func passGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Guard", "1")
		next.ServeHTTP(w, r)
	})
}

func newHandler(t *testing.T) *gridraw.Handler {
	t.Helper()
	reg, err := gridraw.NewRegistry(nopCompiler{}, testGrid())
	if err != nil {
		t.Fatal(err)
	}
	return gridraw.NewHandler(gridraw.Options{
		Registry:   reg,
		Translator: func(_, k string) string { return k },
		Locale:     func(*http.Request) string { return "en" },
		Compiler:   nopCompiler{},
	})
}

func TestRoutes(t *testing.T) {
	for _, guarded := range []bool{true, false} {
		var guard func(http.Handler) http.Handler
		if guarded {
			guard = passGuard
		}
		srv := mount(t, "/api/grids", guard, newHandler(t))
		wantGuard := ""
		if guarded {
			wantGuard = "1"
		}

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/grids/t", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("descriptor: status = %d; body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("X-Guard"); got != wantGuard {
			t.Errorf("descriptor: X-Guard = %q, want %q", got, wantGuard)
		}
		var d gridraw.Descriptor
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || d.Name != "t" {
			t.Errorf("descriptor body = %s (err %v)", rec.Body.String(), err)
		}

		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/grids/nope/rows", bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("rows unknown: status = %d; body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("X-Guard"); got != wantGuard {
			t.Errorf("rows: X-Guard = %q, want %q", got, wantGuard)
		}

		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/grids/t/rows", bytes.NewBufferString(`{"filters":[[]]}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("rows bad request: status = %d; body=%s", rec.Code, rec.Body.String())
		}
		if want := `{"error":"filter group 0 is empty"}` + "\n"; rec.Body.String() != want {
			t.Errorf("body = %q, want %q", rec.Body.String(), want)
		}
	}
}
