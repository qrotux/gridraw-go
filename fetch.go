package gridraw

import (
	"context"
	"errors"
)

// FetchRows returns one validated page; the caller supplies authorization normally performed by HTTP middleware.
func (h *Handler) FetchRows(ctx context.Context, name string, req RowsRequest) (*RowsResponse, error) {
	g, err := h.resolveGrid(ctx, name)
	if err != nil {
		return nil, err
	}
	q, rerr := BuildQuery(g, req)
	if rerr != nil {
		return nil, rerr
	}
	return h.execute(ctx, q)
}

func (h *Handler) resolveGrid(ctx context.Context, name string) (*Grid, *ReqError) {
	g, ok := h.opts.Registry.Get(name)
	if !ok {
		return nil, &ReqError{Status: 404, Msg: "unknown grid"}
	}
	return g.Resolve(ctx), nil
}

func (h *Handler) execute(ctx context.Context, q *Query) (*RowsResponse, error) {
	if h.opts.Executor == nil {
		return nil, errors.New("no executor")
	}
	var st Statements
	var err error
	if compiler, ok := h.opts.Compiler.(ContextCompiler); ok {
		st, err = compiler.CompileContext(ctx, q)
	} else {
		st, err = h.opts.Compiler.Compile(q)
	}
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
