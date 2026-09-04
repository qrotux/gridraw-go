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
    Description: "Application users", // optional; published in the descriptor and the list endpoints
    IDColumn:    "id",
    PageSize:    25,
    DefaultSort: gridraw.SortSpec{Column: "createdAt", Dir: "desc"},
    Binding:     grjet.Base(func() postgres.ReadableTable { return table.Users }),
    Columns: []gridraw.Column{
        grjet.UUIDCol("id", table.Users.ID),
        grjet.StrCol("email", table.Users.Email).WithSearch().Vis().WithDescription("Login email"),
        grjet.TsCol("createdAt", table.Users.CreatedAt).Vis(),
        grjet.TsCol("lastSeenAt", table.Users.LastSeenAt).Nullable(), // adds isNull / isNotNull; modifiers chain on gridraw.Column
        grjet.DateCol("birthday", table.Users.Birthday),
        grjet.TimeCol("opensAt", table.Users.OpensAt),
        grjet.BoolCol("isBanned", table.Users.IsBanned),
        grjet.EnumCol("role", table.Users.Role, []string{"user", "admin"}).FilterWidget(gridraw.WidgetTags),
        grjet.NumCol("rating", table.Users.Rating),
        grjet.DecimalCol("balance", table.Users.Balance),          // money: exact, string on the wire
        grjet.PgType("user_status", grjet.EnumCol("status", table.Users.Status, statuses)), // Postgres enum column
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

Set `SkipTotal: true` on a grid whose row count is too expensive to compute per request; see the rows response below.

Runnable versions of this and of the advanced seams (join, custom binding, per-request grid, guard, locale) live in [`examples/`](examples/README.md).

### Per-request grids

`Grid.ForContext func(ctx context.Context) Grid`, when set, replaces the definition for each request (for example to pick locale-dependent columns). The result is not re-validated, so derive it from the registered grid.

### Array columns

`Column.Array` makes a column an array of its `Type` (any type but `json`). The descriptor carries `"array": true` next to the element type; rows return JSON arrays of element values. Array columns are never sortable, are searchable only with string elements (the quick search runs over `array_to_string`), and use their own operators instead of the scalar ones:

| operator | SQL | value |
|---|---|---|
| `containsAny` | `col && $1` | non-empty array of element values |
| `containsAll` | `col @> $1` | non-empty array of element values |
| `containsOnly` | `col @> $1 AND col <@ $2` | non-empty array of element values |
| `notContainsAny` | `col IS NULL OR NOT (col && $1)` | non-empty array of element values |
| `isEmpty` | `col IS NULL OR cardinality(col) = 0` | none |
| `isNotEmpty` | `cardinality(col) > 0` | none |

`containsOnly` is set equality: the row matches when the column holds exactly the given values, no more and no less, ignoring order and duplicates (`{go,go}` matches `["go"]`). It binds the value twice, once per direction. An empty column value is matched by `isEmpty`, not by `containsOnly`.

Element matching is exact, including case. The parameter is bound as `<element type>[]` (`text[]`, `uuid[]`, `float8[]`, `decimal[]`, `bool[]`, `date[]`, `time[]`, `timestamptz[]`); when the column's SQL element type differs, name it with `grjet.PgType` (an `integer[]` column, a Postgres enum). `Step` on a `time`/`datetime` array validates alignment and informs the UI but does not widen matching.

```go
grjet.ArrayCol("tags", table.Posts.Tags, gridraw.TypeString),
grjet.PgType("int4", grjet.ArrayCol("scores", table.Posts.Scores, gridraw.TypeNumber)), // integer[] column
grjet.PgType("_locales", grjet.EnumArrayCol("locales", table.Trips.Locales, []string{"en", "ru"})),
```

### Time resolution

`Column.Step` (a `time.Duration`, default one second; set it with `.WithStep(d)`) sets the resolution of a `time` or `datetime` column: whole seconds, dividing a day. The descriptor publishes it as `step` in seconds so the UI can drop the seconds field, offer 15-minute slots or hours. Filter values must be aligned to the step, otherwise 400. With a step above one second the operators act on whole buckets: `eq 09:15` on a 15-minute column matches `09:15:00` through `09:29:59`, `gt 09:15` starts at `09:30`, `between [09:00, 10:00]` ends before `10:15`. Rows are still returned with seconds; the client formats them by `step`.

```go
grjet.TimeCol("slot", table.Bookings.Slot).WithStep(15*time.Minute)
grjet.TsCol("startsAt", table.Shifts.StartsAt).WithStep(time.Hour)
```

### Postgres enum columns

A column whose SQL type is a Postgres `enum` cannot be compared with a text parameter (`operator does not exist: my_enum = text`). Wrap it in `grjet.PgType("my_enum", …)` and the `in`/`notIn` parameters are cast to that type. On an array column `PgType` names the SQL element type for the array parameter (any element type, e.g. `int4` for an `integer[]` column) and projects the value as `text[]`, since pgx cannot decode arrays of a custom element type. `Binding.ParamType` is the underlying field for hand-written bindings.

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
  "description": "Application users",
  "idColumn": "id",
  "pageSize": 25,
  "pageSizeOptions": [10, 25, 50, 100],
  "defaultSort": {"column": "createdAt", "dir": "desc"},
  "search": {"columns": ["Email"]},
  "columns": [
    {
      "key": "email", "type": "string", "title": "Email", "description": "Login email",
      "sortable": true, "defaultVisible": true,
      "filter": {"operators": [{"op": "eq", "label": "equals"}, {"op": "contains", "label": "contains"}]}
    },
    {
      "key": "slot", "type": "time", "title": "Slot", "sortable": true, "defaultVisible": true, "step": 900,
      "filter": {"operators": [{"op": "eq", "label": "equals"}, {"op": "between", "label": "between"}]}
    },
    {
      "key": "role", "type": "enum", "title": "Role", "sortable": true, "defaultVisible": false,
      "filter": {"operators": [{"op": "in", "label": "is one of"}, {"op": "notIn", "label": "is not one of"}],
                 "enumValues": [{"value": "user", "label": "User"}, {"value": "admin", "label": "Admin"}],
                 "widget": "tags"}
    }
  ]
}
```

`skipTotal` is `true` only on a grid whose rows response carries no `total`, and is omitted otherwise. `description` is omitted, on the grid and on a column, when neither a literal `Description` nor a translation is set. `search` is `null` when no column is searchable. `filter` is omitted for non-filterable columns. `array` is `true` on array columns, whose `type` is then the element type. `step` (seconds) is present on every `time` and `datetime` column, `1` by default. `filter.widget` is a UI hint for the filter input (`checkboxes`, `tags`, or any client-defined value), omitted when unset.

### `GET <base>/-/list` → grid list

```json
[{"name": "orders", "description": "Customer orders"}, {"name": "users", "description": "Application users"}]
```

Every registered grid, sorted by name. `description` is omitted when unset.

### `GET <base>/-/registry` → grid list with columns

```json
[
  {
    "name": "users",
    "description": "Application users",
    "columns": [
      {"key": "id", "title": "ID", "type": "uuid"},
      {"key": "email", "title": "Email", "type": "string", "description": "Login email"}
    ]
  }
]
```

The same list plus the columns of each grid, with localized titles. Columns are listed in declaration order; array columns carry the element type. Both endpoints run `ForContext` per grid and sit behind the same guard as the other routes, so a guard is still the only thing that restricts who sees which grids.

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
{"rows": [{"id": "…", "email": "…", "role": "admin"}], "total": 128, "hasPrev": false, "hasNext": true}
```

- `filters` is a disjunctive normal form: the outer array is OR, each inner array is AND. Every group must be non-empty.
- `search` is combined with the filters by AND, and matches any searchable column case-insensitively (`ILIKE %term%`); `%`, `_` and `\` in the term are escaped.
- `sort` priority is array order. Empty falls back to the grid default. The id column is appended as a tiebreaker unless already present. Nulls sort last.
- `idColumn` is always included in every row, even when not requested.
- `page` defaults to 1, `pageSize` to the grid default.
- `hasPrev` and `hasNext` are always present. They cost nothing: `hasPrev` is `page > 1` and the rows statement fetches one row over the page, whose presence is `hasNext`. The extra row never reaches the client.
- `total` is present unless the grid sets `SkipTotal`, which drops the `COUNT` query — usually the expensive half on a filtered set. The decision is the server's alone: a request cannot ask for or refuse the count. A grid that counts still sends `"total": 0` for an empty result, and a grid that does not omits the key entirely; the descriptor carries `"skipTotal": true` so the client knows to paginate on `hasPrev`/`hasNext` instead of page numbers.

Column types and their operators:

| type | operators | value |
|---|---|---|
| `string` | `eq`, `neq`, `contains`, `notContains`, `starts`, `ends` | string; all case-insensitive |
| `uuid` | `eq`, `neq`, `in`, `notIn` | canonical uuid string(s), any case; exact match |
| `number` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `between`, `notBetween` | number, or `[a, b]` for the range operators |
| `decimal` | same as `number` | decimal string (`"19.99"`), or `[a, b]`; JSON numbers are rejected |
| `date` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `between`, `notBetween` | `YYYY-MM-DD` string, or `[a, b]` |
| `time` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `between`, `notBetween` | `HH:MM:SS` or `HH:MM` string, or `[a, b]` |
| `datetime` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `between`, `notBetween` | RFC 3339 string, or `[a, b]` |
| `boolean` | `eq` | boolean (`eq false` also matches NULL) |
| `enum` | `in`, `notIn` | non-empty array of strings |
| `json` | none | display only, not sortable |

Array columns use the array operators instead (see Array columns). Two operators apply to every type and take no value: `isNull` and `isNotNull`. The constructors leave them out; call `.Nullable()` on a column to offer them.

Negative operators (`neq`, `notContains`, `notIn`, `notBetween`) also match NULL: a row with no value is "not equal" to anything.

`time` and `datetime` values must be aligned to the column `step` (see Time resolution); fractional seconds are rejected. A `time` range across midnight (`22:00` to `06:00`) is two OR groups (`gte 22:00`, `lte 06:00`), because a range requires `a <= b`. Use Postgres `time`, not `timetz`.

String operators compile to `ILIKE`, including `eq`, so `eq` matches regardless of case (a change from v0.1.0, where it was exact) and a plain btree index on the column does not serve them; index `lower(col)` or use `pg_trgm` for large tables.

Limits: 10 filter groups, 20 clauses per group, 16 sort columns, page size 1..100.

Errors are `{"error": "<message>"}` with status 404 (unknown grid), 400 (invalid request) or 500 (query failed; details go to the logger).

Row values: arrays arrive as JSON arrays of the element format below; `uuid` arrives as a lowercase string, `numeric` as a float for `number` columns and as a string with its stored scale for `decimal` columns (`"4.10"`), `date` as `YYYY-MM-DD`, `time` as `HH:MM:SS`, `timestamptz` as an RFC 3339 UTC string, `jsonb` as decoded JSON, NULL as `null`.

## i18n

The descriptor resolves every label through `Translator(locale, key)` with these keys:

| what | key |
|---|---|
| column title | `grid.<grid>.<column>` |
| operator label | `grid.operators.<op>` |
| enum value label | `grid.<grid>.<column>_values.<value>` |
| grid description | `grid.<grid>.description` |
| column description | `grid.<grid>.<column>.description` |

Descriptions are the one label with a literal fallback: `Grid.Description` and `Column.Description` are used as written, and a translation of the description key overrides them. A `Translator` that echoes an unknown key back (the convention the examples follow) therefore counts as a miss, not as a description.

## Writing your own adapters

- **Compiler**: implement `Validate(*Grid) error` (check that `Grid.Binding` and every `Column.Binding` carry what you need; runs once at `NewRegistry`) and `Compile(*Query) (Statements, error)`. `Query` is fully validated: typed clause values (`string`, `[]string`, lowercased uuid strings, decimal strings, `float64`, and for array columns a `[]string`/`[]float64`/`[]bool`/`[]time.Time` of elements, `time.Time` for `date`, `time` and `datetime`, `bool`, nothing for `isNull`/`isNotNull`), resolved sort terms, page and page size. Stepped time columns arrive already widened to buckets: `between`/`notBetween` clauses with `Clause.UpperOpen` set mean `[a, b)`, and a `time` upper bound may be the next midnight (`t.Day() != 1`), which Postgres spells `24:00:00`. Append the id tiebreaker yourself, and `LIMIT` to `q.RowLimit()` (one row over the page) so the handler can answer `hasNext`; a compiler that limits to `q.PageSize` instead only ever reports `hasNext: false`. Skip the count statement when `q.WithTotal` is false if building it is not free.
- **Executor**: implement `Rows(ctx, sql, args, keys)` returning one `map[string]any` per row keyed positionally by `keys`, and `Count(ctx, sql, args)`.
- **Router**: call `Handler.Descriptor(w, r, name)`, `Handler.Rows(w, r, name)`, `Handler.List(w, r)` and `Handler.Catalog(w, r)` from any mux; see `router/grstd` for the shortest example.

## Tests

```
go test ./...
GRIDRAW_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/db go test ./adapter/grpgx/
```

The pgx test creates a temporary table only and skips when the variable is unset.

## License

MIT
