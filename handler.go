package gridraw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// RowsResponse is the body of the rows endpoint. Total is absent when the
// request asked to skip the count; HasPrev and HasNext are always present.
type RowsResponse struct {
	Rows    []map[string]any `json:"rows"`
	Total   *int64           `json:"total,omitempty"`
	HasPrev bool             `json:"hasPrev"`
	HasNext bool             `json:"hasNext"`
}

// Options wires a Handler. Locale picks the descriptor locale per request.
type Options struct {
	Registry   *Registry
	Translator Translator
	Locale     func(*http.Request) string
	Compiler   Compiler
	Executor   Executor
	Log        *slog.Logger
}

// Handler serves the descriptor and rows endpoints; the router subpackages
// mount it and extract the grid name from the path.
type Handler struct {
	opts Options
}

// NewHandler builds a Handler; Log defaults to slog.Default.
func NewHandler(opts Options) *Handler {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Handler{opts: opts}
}

func (h *Handler) grid(w http.ResponseWriter, name string) (*Grid, bool) {
	g, ok := h.opts.Registry.Get(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown grid"})
		return nil, false
	}
	return g, true
}

// Resolve applies ForContext for a request context.
func (g *Grid) Resolve(ctx context.Context) *Grid {
	if g.ForContext == nil {
		return g
	}
	r := g.ForContext(ctx)
	return &r
}

// Descriptor answers GET <base>/{name}.
func (h *Handler) Descriptor(w http.ResponseWriter, r *http.Request, name string) {
	g, ok := h.grid(w, name)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, BuildDescriptor(g.Resolve(r.Context()), h.opts.Translator, h.opts.Locale(r)))
}

// List answers GET <base>/-/list: every registered grid by name and description.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	names := h.opts.Registry.Names()
	out := make([]GridInfo, 0, len(names))
	for _, name := range names {
		g, _ := h.opts.Registry.Get(name)
		out = append(out, BuildGridInfo(g.Resolve(r.Context()), h.opts.Translator, h.opts.Locale(r)))
	}
	writeJSON(w, http.StatusOK, out)
}

// Catalog answers GET <base>/-/registry: every registered grid with its columns.
func (h *Handler) Catalog(w http.ResponseWriter, r *http.Request) {
	names := h.opts.Registry.Names()
	out := make([]GridEntry, 0, len(names))
	for _, name := range names {
		g, _ := h.opts.Registry.Get(name)
		out = append(out, BuildGridEntry(g.Resolve(r.Context()), h.opts.Translator, h.opts.Locale(r)))
	}
	writeJSON(w, http.StatusOK, out)
}

// Rows answers POST <base>/{name}/rows.
func (h *Handler) Rows(w http.ResponseWriter, r *http.Request, name string) {
	g, ok := h.grid(w, name)
	if !ok {
		return
	}
	g = g.Resolve(r.Context())
	var req RowsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	q, rerr := BuildQuery(g, req)
	if rerr != nil {
		writeJSON(w, rerr.Status, map[string]string{"error": rerr.Msg})
		return
	}
	resp, err := h.execute(r.Context(), q)
	if err != nil {
		h.opts.Log.Error("grid rows", "grid", g.Name, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) execute(ctx context.Context, q *Query) (*RowsResponse, error) {
	if h.opts.Executor == nil {
		return nil, errors.New("no executor")
	}
	st, err := h.opts.Compiler.Compile(q)
	if err != nil {
		return nil, err
	}
	rows, err := h.opts.Executor.Rows(ctx, st.RowsSQL, st.RowsArgs, q.Keys)
	if err != nil {
		return nil, err
	}
	resp := &RowsResponse{Rows: rows, HasPrev: q.Page > 1}
	// The compiler asked for one row more than the page; its presence is the
	// answer to hasNext. A compiler that ignores RowLimit only loses hasNext.
	if len(rows) > q.PageSize {
		resp.Rows, resp.HasNext = rows[:q.PageSize], true
	}
	if q.WithTotal {
		total, err := h.opts.Executor.Count(ctx, st.CountSQL, st.CountArgs)
		if err != nil {
			return nil, err
		}
		resp.Total = &total
	}
	return resp, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
