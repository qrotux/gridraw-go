# Examples

Each example is a standalone `main` package inside this module. Both need a
Postgres reachable through `DATABASE_URL`; on first start they create their
own `example_*` tables and seed a few rows, and they never touch anything else.

| Example | Router | Shows |
|---|---|---|
| [`basic`](basic/main.go) | chi | one grid over one table, the column constructors, a map-backed translator |
| [`advanced`](advanced/main.go) | `net/http.ServeMux` | a joined table, a hand-written `grjet.Binding` with COALESCE, a per-request grid through `ForContext`, a guard middleware, locale negotiation, a JSON logger |

```
DATABASE_URL=postgres://user:pass@localhost:5432/db go run ./examples/basic
DATABASE_URL=postgres://user:pass@localhost:5432/db go run ./examples/advanced
```

Each file's package comment carries `curl` calls for its endpoints.
