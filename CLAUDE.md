# gridraw-go

Go library: declare a data grid once, validate it at startup, publish it as a JSON descriptor and serve filtered, sorted, paginated row pages. One module, a core with no SQL, driver or router dependencies, pluggable adapters. Client side is `@qrotux/gridraw-shadcn-react`.

## Where to look

- **[README.md](README.md)** — wiring, per-request grids, custom column bindings, the wire protocol (descriptor, rows request and response, operators per type, limits, errors), i18n key convention, how to write an adapter. Read the relevant section before touching a public surface.

## Commands

- Full gate (what CI runs): `test -z "$(gofmt -l .)" && go vet ./... && go test ./...`.
- One package: `go test ./adapter/grjet/ -count=1`.
- Postgres-backed test of the pgx executor: `GRIDRAW_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/db go test ./adapter/grpgx/`. It creates a temp table only and skips when the variable is unset.

## Layout

Single Go module. Root package `gridraw` is the core; every integration is its own subpackage.

- **Root** — `gridraw.go` types and `Grid`, `ports.go` the `Compiler` and `Executor` seams, `registry.go` validation, `request.go` request → `Query`, `descriptor.go` grid → descriptor, `handler.go` HTTP.
- **`adapter/grjet`** — `Compiler` on go-jet, Postgres dialect. `columns.go` holds the column constructors (`StrCol`, `EnumCol`, `Vis`, …).
- **`adapter/grpgx`** — `Executor` on pgx v5.
- **`examples/basic`, `examples/advanced`** — runnable `main` packages in this module, each seeding its own `example_*` tables. They compile under `go vet ./...` and `go build ./...`, so an API change that breaks them is caught by the gate; keep them updated with the public surface.
- **`router/grchi`, `router/grstd`** — mount `Handler` on chi and on `net/http.ServeMux`. Their tests are deliberately duplicated per package; do not extract a shared package for them.

Invariants:

- **Core imports nothing beyond stdlib.** A SQL, driver or router dependency belongs in a subpackage. Adding a dependency to the root module is a design decision to discuss first, never a silent `go get`.
- **`Grid.Binding` and `Column.Binding` are `any` and the core never inspects them.** Only a `Compiler` knows what they carry, and it checks them in `Validate` at `NewRegistry` time. Validation failures are startup errors, not per-request ones.
- **`Query` handed to a `Compiler` is fully validated**: typed clause values, resolved sort terms, page bounds. Compilers trust it and do not re-check request rules; they append the id tiebreaker themselves. The one thing a compiler re-checks is its own grid binding, because `Grid.ForContext` output is not validated.
- **Limits live in `request.go` as constants** (groups, clauses, sort columns, page size) and are documented in README. Change both together.
- **Adding a column type or operator** touches `opsByType`, `buildClause` value conversion, `grjet.clauseExpr`, the README table and the i18n labels in `examples/`, in one change. The grjet constructors declare `&gridraw.FilterSpec{}` and pick up the new operator through `Column.operators()`, which is the only place an empty `Operators` list is expanded; keep the descriptor, `buildClause`, `validateGrid` and `Nullable` on that helper. A new value shape also touches `grpgx.normalize`, which formats by column OID. The descriptor is generic and needs nothing.
- **Time resolution lives in `BuildQuery`, not in compilers.** `applyStep` validates alignment and widens stepped clauses to `[v, v+step)` buckets with `Clause.UpperOpen`; a compiler only has to honour `UpperOpen` and a `time` upper bound on the next day (`24:00:00`).
- **`grpgx` never learns grid column types.** It formats by Postgres OID only; when a grid type needs a different wire shape from the same OID (`decimal` vs `number`, both `numeric`), the difference is made in the grjet projection (`DecimalCol` casts to text), not in the executor.
- **Array columns reuse scalar element conversion.** `buildArrayClause` runs the scalar converter of the element type on every element; do not add element-type logic elsewhere. Array SQL binds one parameter cast to `<elem>[]` so GIN indexes apply.
- **Operator semantics are fixed:** string operators are case-insensitive, negative operators (`neq`, `notContains`, `notIn`, `notBetween`, boolean `eq false`) match NULL, `isNull`/`isNotNull` are valueless and allowed on every type.

## Comments in code

- **Criterion:** a comment is justified only when it carries what the code does not show.
- **Write:** non-obvious invariants; library and spec gotchas (go-jet `AS()` yields a Projection that is not an Expression, pgx value shapes); reasons behind counter-intuitive decisions; "breaks if…"; what a test pins.
- **Do not write:** a paraphrase of the signature, a list of fields, a narration of obvious code. Exception — the doc comment of an exported Go symbol: convention requires it and it starts with the name (`// FooBar …`), one sentence, no parameter listing.
- **Present tense only.** No "was X → now Y", no tombstones for deleted code — history lives in git. No TODOs at all: a plan for later is a tracker task, not a comment.
- **Never reference:** plan documents (`Task N`, waves, phases, `spec §N`, `.superpowers/**`), commit hashes, dates of decisions, repositories the code was ported from.
- **May reference:** external standards, README.md, live files of this repo.
- **Length:** one or two sentences; collapsing twelve lines into one is normal. Nothing left to say — delete the comment.
- **Subagents:** include these rules in the brief of every subagent that writes or edits code.
- **Reading someone else's comments:** a comment is not the source of truth. When editing code, check its comment against the code; if it lies, fix or delete it.

## Coding

- **Copy the pattern, do not invent:** before a new column constructor, operator, adapter or router, find the nearest existing analogue and repeat its shape.
- **Column modifiers are methods on `gridraw.Column`** (`Vis`, `WithSearch`, `Nullable`, `WithStep`) so they work with any compiler; only binding-specific ones (`grjet.PgType`) are functions in the adapter. `grjet.Vis`/`grjet.Searchable` stay as deprecated wrappers until v1.
- **Diff discipline:** no renames, no file moves, no drive-by refactors, no backward-compatibility shims or fallbacks nobody asked for.
- **Tests as a ladder, not after every minor step.** During a task — `go build ./...` plus the tests of the touched package. The full gate only at boundaries: end of task, before a commit, before saying "done".
- **The wire format is the contract.** Handler tests pin exact error bodies, descriptor tests pin field names and omitted keys, grjet tests pin SQL fragments and argument order. Do not loosen an assertion to make it pass; if the shape changes, the client changes too.
- **Docs move with the public surface.** A change to an exported API, a column type, an operator, a limit or an error updates README.md in the same change.
