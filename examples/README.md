# Examples

Each example is a standalone `main` package inside this module. `basic` and
`advanced` need a Postgres reachable through `DATABASE_URL`; on first start
they create their own `example_*` tables and seed a few rows, and they never
touch anything else. `translations` prints JSON descriptors without a database.

| Example | Router | Shows |
|---|---|---|
| [`basic`](basic/main.go) | chi | one grid over one table, the column constructors, a map-backed translator |
| [`advanced`](advanced/main.go) | `net/http.ServeMux` | a joined table, `WithFilterExpr` with COALESCE, `WithScope` restricting non-admins to active members, a per-request grid through `ForContext`, a guard middleware, locale negotiation, a JSON logger |
| [`translations`](translations/main.go) | — | English and Russian labels, missing translations, `WithTitles` fallbacks, descriptors without a translator |

```
DATABASE_URL=postgres://user:pass@localhost:5432/db go run ./examples/basic
DATABASE_URL=postgres://user:pass@localhost:5432/db go run ./examples/advanced
go run ./examples/translations
```

The HTTP examples' package comments carry `curl` calls for their endpoints.

The translations example deliberately omits the English email title:

| Output | Email title |
|---|---|
| `withoutTranslator` | `email` (column key) |
| `english` | `grid.users.email` (missing translation echoed by the translator) |
| `englishWithTitles` | `Email address` (literal fallback) |
| `russianWithTitles` | `Почта` (translation takes precedence) |

It also shows translated operator and enum labels and a literal grid description
used when no translation is available. To use the same setup in an HTTP application,
pass `withTitles` as `Options.Translator` and supply the locale through `Options.Locale`.
