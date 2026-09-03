# gridraw-go

Server side of the gridraw data grid: a grid is declared once in Go, validated at startup, published to the client as a JSON descriptor, and served as filtered, sorted, paginated row pages. The matching React UI is `@qrotux/gridraw-shadcn-react`.

The core package `gridraw` knows nothing about SQL, drivers or routers. Those are pluggable:

| Seam | Interface | Shipped adapter |
|---|---|---|
| SQL generation | `gridraw.Compiler` | `adapter/grjet` (go-jet, Postgres dialect) |
| SQL execution | `gridraw.Executor` | `adapter/grpgx` (pgx v5) |
| HTTP routing | `gridraw.Handler` methods | `router/grchi` (chi v5), `router/grstd` (net/http ServeMux) |

## Install

```
go get github.com/qrotux/gridraw-go
```

## Wiring

```go
import (
    "github.com/go-chi/chi/v5"
    "github.com/go-jet/jet/v2/postgres"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/qrotux/gridraw-go"
    "github.com/qrotux/gridraw-go/adapter/grjet"
    "github.com/qrotux/gridraw-go/adapter/grpgx"
    "github.com/qrotux/gridraw-go/router/grchi"
)

users := gridraw.Grid{
    Name:        "users",
    IDColumn:    "id",
    PageSize:    25,
    DefaultSort: gridraw.SortSpec{Column: "createdAt", Dir: "desc"},
    Binding:     grjet.Base(func() postgres.ReadableTable { return table.Users }),
    Columns: []gridraw.Column{
        grjet.StrColNoFilter("id", table.Users.ID),
        grjet.Vis(grjet.Searchable(grjet.StrCol("email", table.Users.Email))),
        grjet.Vis(grjet.TsCol("createdAt", table.Users.CreatedAt)),
        grjet.BoolCol("isBanned", table.Users.IsBanned),
        grjet.EnumCol("role", table.Users.Role, []string{"user", "admin"}),
        grjet.NumCol("rating", table.Users.Rating),
        grjet.JSONCol("prefs", table.Users.Prefs),
    },
}

compiler := grjet.Compiler{}
reg, err := gridraw.NewRegistry(compiler, users)
if err != nil {
    log.Fatal(err) // invalid grids fail at startup, not per request
}

h := gridraw.NewHandler(gridraw.Options{
    Registry:   reg,
    Translator: translate,                                   // func(locale, key string) string
    Locale:     func(r *http.Request) string { return "en" },
    Compiler:   compiler,
    Executor:   grpgx.New(pool),                             // *pgxpool.Pool, *pgx.Conn or pgx.Tx
})

r := chi.NewRouter()
grchi.Register(r, "/api/admin/grids", authGuard, h) // guard may be nil
```

`grstd.Register(mux, "/api/admin/grids", authGuard, h)` does the same on an `*http.ServeMux`.

Runnable versions of this and of the advanced seams (join, custom binding, per-request grid, guard, locale) live in [`examples/`](examples/README.md).

### Per-request grids

`Grid.ForContext func(ctx context.Context) Grid`, when set, replaces the definition for each request (for example to pick locale-dependent columns). The result is not re-validated, so derive it from the registered grid.

### Custom column bindings

`grjet.Binding{Projection, Filter, Sort}` is exported. `Filter` and `Sort` default to `Projection` when it is a plain expression; set them explicitly when the projection is aliased (`.AS(key)`) or when the filter should run on a different expression:

```go
gridraw.Column{
    Key: "active", Type: gridraw.TypeBool, Sortable: true,
    Binding: grjet.Binding{
        Projection: table.Users.Active,
        Filter:     postgres.COALESCE(table.Users.Active, postgres.Bool(true)),
    },
    Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpEq}},
}
```

## Protocol

### `GET <base>/{name}` → descriptor

```json
{
  "name": "users",
  "idColumn": "id",
  "pageSize": 25,
  "pageSizeOptions": [10, 25, 50, 100],
  "defaultSort": {"column": "createdAt", "dir": "desc"},
  "search": {"columns": ["Email"]},
  "columns": [
    {
      "key": "email", "type": "string", "title": "Email",
      "sortable": true, "defaultVisible": true,
      "filter": {"operators": [{"op": "eq", "label": "equals"}, {"op": "contains", "label": "contains"}]}
    },
    {
      "key": "role", "type": "enum", "title": "Role", "sortable": true, "defaultVisible": false,
      "filter": {"operators": [{"op": "in", "label": "is one of"}],
                 "enumValues": [{"value": "user", "label": "User"}, {"value": "admin", "label": "Admin"}]}
    }
  ]
}
```

`search` is `null` when no column is searchable. `filter` is omitted for non-filterable columns.

### `POST <base>/{name}/rows` → rows

Request:

```json
{
  "columns": ["email", "role"],
  "filters": [
    [{"field": "rating", "op": "gte", "value": 4}, {"field": "isBanned", "op": "eq", "value": true}],
    [{"field": "role", "op": "in", "value": ["admin"]}]
  ],
  "search": "ivan",
  "sort": [{"column": "rating", "dir": "desc"}, {"column": "email", "dir": "asc"}],
  "page": 1,
  "pageSize": 25
}
```

Response:

```json
{"rows": [{"id": "…", "email": "…", "role": "admin"}], "total": 128}
```

- `filters` is a disjunctive normal form: the outer array is OR, each inner array is AND. Every group must be non-empty.
- `search` is combined with the filters by AND, and matches any searchable column case-insensitively (`ILIKE %term%`); `%`, `_` and `\` in the term are escaped.
- `sort` priority is array order. Empty falls back to the grid default. The id column is appended as a tiebreaker unless already present. Nulls sort last.
- `idColumn` is always included in every row, even when not requested.
- `page` defaults to 1, `pageSize` to the grid default.

Column types and their operators:

| type | operators | value |
|---|---|---|
| `string` | `eq`, `contains`, `starts` | string |
| `number` | `eq`, `gte`, `lte`, `between` | number, or `[a, b]` for between |
| `datetime` | `gte`, `lte`, `between` | RFC 3339 string, or `[a, b]` |
| `boolean` | `eq` | boolean (`eq false` also matches NULL) |
| `enum` | `in` | non-empty array of strings |
| `json` | none | display only, not sortable |

Limits: 10 filter groups, 20 clauses per group, 16 sort columns, page size 1..100.

Errors are `{"error": "<message>"}` with status 404 (unknown grid), 400 (invalid request) or 500 (query failed; details go to the logger).

Row values: `uuid` arrives as a string, `numeric` as a float, `timestamptz` as an RFC 3339 UTC string, `jsonb` as decoded JSON, NULL as `null`.

## i18n

The descriptor resolves every label through `Translator(locale, key)` with these keys:

| what | key |
|---|---|
| column title | `grid.<grid>.<column>` |
| operator label | `grid.operators.<op>` |
| enum value label | `grid.<grid>.<column>_values.<value>` |

## Writing your own adapters

- **Compiler**: implement `Validate(*Grid) error` (check that `Grid.Binding` and every `Column.Binding` carry what you need; runs once at `NewRegistry`) and `Compile(*Query) (Statements, error)`. `Query` is fully validated: typed clause values (`string`, `[]string`, `float64`, `time.Time`, `bool`), resolved sort terms, page and page size. Append the id tiebreaker yourself.
- **Executor**: implement `Rows(ctx, sql, args, keys)` returning one `map[string]any` per row keyed positionally by `keys`, and `Count(ctx, sql, args)`.
- **Router**: call `Handler.Descriptor(w, r, name)` and `Handler.Rows(w, r, name)` from any mux; see `router/grstd` for the shortest example.

## Tests

```
go test ./...
GRIDRAW_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/db go test ./adapter/grpgx/
```

The pgx test creates a temporary table only and skips when the variable is unset.

## License

MIT
