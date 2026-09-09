package gridraw

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestFetchRowsLegacyCompilerAndResolution(t *testing.T) {
	g := validTestGrid()
	calls := 0
	g.ForContext = func(context.Context) Grid {
		calls++
		resolved := g
		resolved.SkipTotal = true
		resolved.Columns = append([]Column(nil), g.Columns[:2]...)
		return resolved
	}
	reg, err := NewRegistry(nopCompiler{}, g)
	if err != nil {
		t.Fatal(err)
	}
	ex := &fakeExecutor{rows: []map[string]any{{"id": "a"}, {"id": "b"}, {"id": "c"}}}
	h := NewHandler(Options{Registry: reg, Compiler: nopCompiler{}, Executor: ex})
	page, err := h.FetchRows(context.Background(), "t", RowsRequest{Columns: []string{"email", "email"}, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(page.Rows) != 2 || !page.HasPrev || !page.HasNext || page.Total != nil || ex.counted || !reflect.DeepEqual(ex.keys, []string{"email", "id"}) {
		t.Fatalf("page=%+v keys=%v calls=%d", page, ex.keys, calls)
	}
	for _, tc := range []struct {
		name   string
		req    RowsRequest
		status int
	}{
		{"missing", RowsRequest{}, 404},
		{"t", RowsRequest{Columns: []string{"rating"}}, 400},
		{"t", RowsRequest{PageSize: 101}, 400},
	} {
		_, err := h.FetchRows(context.Background(), tc.name, tc.req)
		var re *ReqError
		if !errors.As(err, &re) || re.Status != tc.status {
			t.Fatalf("error: %v", err)
		}
	}
}

type contextTestCompiler struct {
	nopCompiler
	compile func(context.Context, *Query) (Statements, error)
}

func (c contextTestCompiler) CompileContext(ctx context.Context, q *Query) (Statements, error) {
	return c.compile(ctx, q)
}

type fetchContextKey struct{}

func TestFetchAndHTTPUseContextCompiler(t *testing.T) {
	g := validTestGrid()
	reg, err := NewRegistry(nopCompiler{}, g)
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("scope denied")
	compiler := contextTestCompiler{compile: func(ctx context.Context, q *Query) (Statements, error) {
		if ctx.Value(fetchContextKey{}) != "tenant-a" || !reflect.DeepEqual(q.Keys, []string{"email", "id"}) {
			t.Fatalf("lost context or projection: %+v", q)
		}
		return Statements{}, denied
	}}
	ex := &fakeExecutor{}
	h := NewHandler(Options{Registry: reg, Compiler: compiler, Executor: ex})
	ctx := context.WithValue(context.Background(), fetchContextKey{}, "tenant-a")
	if _, err := h.FetchRows(ctx, "t", RowsRequest{Columns: []string{"email"}}); !errors.Is(err, denied) {
		t.Fatalf("error: %v", err)
	}
	w := httptest.NewRecorder()
	h.Rows(w, httptest.NewRequest("POST", "/t/rows", strings.NewReader(`{"columns":["email"]}`)).WithContext(ctx), "t")
	if w.Code != 500 || w.Body.String() != "{\"error\":\"query failed\"}\n" {
		t.Fatalf("response: %d %s", w.Code, w.Body.String())
	}
	if ex.keys != nil || ex.counted {
		t.Fatal("executed after scope failure")
	}
}

func TestHTTPPreservesLookupBeforeJSON(t *testing.T) {
	reg, err := NewRegistry(nopCompiler{}, validTestGrid())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Options{Registry: reg})
	w := httptest.NewRecorder()
	h.Rows(w, httptest.NewRequest("POST", "/missing/rows", strings.NewReader(`{`)), "missing")
	if w.Code != 404 || w.Body.String() != "{\"error\":\"unknown grid\"}\n" {
		t.Fatal(w.Body.String())
	}
}
