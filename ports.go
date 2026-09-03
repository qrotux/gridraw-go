package gridraw

import "context"

// Statements is the SQL a Compiler produces for one Query.
type Statements struct {
	RowsSQL   string
	RowsArgs  []any
	CountSQL  string
	CountArgs []any
}

// Compiler turns grid definitions into SQL. Validate runs once per grid at
// registration and checks whatever the bindings require; Compile runs per request.
type Compiler interface {
	Validate(g *Grid) error
	Compile(q *Query) (Statements, error)
}

// Executor runs the compiled SQL. Rows returns one map per row keyed by keys
// in projection order.
type Executor interface {
	Rows(ctx context.Context, sql string, args []any, keys []string) ([]map[string]any, error)
	Count(ctx context.Context, sql string, args []any) (int64, error)
}
