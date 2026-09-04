// Command advanced shows the seams beyond the basic wiring: a joined table,
// a hand-written binding, a per-request grid, a guard middleware, locale
// negotiation and a custom logger, all on net/http's ServeMux.
//
//	DATABASE_URL=postgres://user:pass@localhost:5432/db API_KEY=secret go run ./examples/advanced
//	curl -H 'X-Api-Key: secret' -H 'Accept-Language: ru' localhost:8080/api/grids/members
//	curl -H 'X-Api-Key: secret' -H 'X-Role: admin' -X POST localhost:8080/api/grids/members/rows \
//	     -d '{"columns":["email","team","active"],"filters":[[{"field":"active","op":"eq","value":false}]]}'
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qrotux/gridraw-go"
	"github.com/qrotux/gridraw-go/adapter/grjet"
	"github.com/qrotux/gridraw-go/adapter/grpgx"
	"github.com/qrotux/gridraw-go/router/grstd"
)

var (
	colID       = postgres.StringColumn("id")
	colEmail    = postgres.StringColumn("email")
	colTeamID   = postgres.IntegerColumn("team_id")
	colActive   = postgres.BoolColumn("active") // nullable: NULL means "never decided"
	colLastSeen = postgres.TimestampzColumn("last_seen_at")
	colPrefs    = postgres.StringColumn("prefs")
	members     = postgres.NewTable("public", "example_advanced_members", "", colID, colEmail, colTeamID, colActive, colLastSeen, colPrefs)

	colTeamPK   = postgres.IntegerColumn("id")
	colTeamName = postgres.StringColumn("name")
	teams       = postgres.NewTable("public", "example_advanced_teams", "", colTeamPK, colTeamName)
)

// Both tables have an "id" column, so the projection of each side is
// qualified by its table; go-jet does that from the table the column
// belongs to, and JoinStrCol aliases the joined one to the grid key.
func joined() postgres.ReadableTable {
	return members.INNER_JOIN(teams, colTeamID.EQ(colTeamPK))
}

type roleKey struct{}

// membersGrid is the registered definition. It is validated once; the
// per-request variant derived in ForContext is not, so it only removes
// columns and never adds bindings.
func membersGrid() gridraw.Grid {
	base := gridraw.Grid{
		Name:        "members",
		IDColumn:    "id",
		PageSize:    25,
		DefaultSort: gridraw.SortSpec{Column: "email", Dir: "asc"},
		Binding:     grjet.Base(joined),
		Columns: []gridraw.Column{
			grjet.UUIDCol("id", colID),
			grjet.StrCol("email", colEmail).WithSearch().Vis(),
			grjet.JoinStrCol("team", colTeamName).WithSearch().Vis(),
			{
				// A nullable boolean: the filter treats NULL as false through
				// COALESCE while the projection still shows null to the client.
				Key: "active", Type: gridraw.TypeBool, Sortable: true, DefaultVisible: true,
				Binding: grjet.Binding{
					Projection: colActive,
					Filter:     postgres.COALESCE(colActive, postgres.Bool(false)),
				},
				Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpEq}},
			},
			grjet.TsCol("lastSeenAt", colLastSeen).Nullable(), // NULL means "never seen"
			grjet.JSONCol("prefs", colPrefs),
		},
	}
	base.ForContext = func(ctx context.Context) gridraw.Grid {
		if ctx.Value(roleKey{}) == "admin" {
			return base
		}
		// Non-admins never see prefs: the column is absent from the
		// descriptor and requesting it answers 400.
		g := base
		g.Columns = make([]gridraw.Column, 0, len(base.Columns))
		for _, c := range base.Columns {
			if c.Key != "prefs" {
				g.Columns = append(g.Columns, c)
			}
		}
		return g
	}
	return base
}

var i18n = map[string]map[string]string{
	"en": {
		"grid.members.email": "Email", "grid.members.team": "Team", "grid.members.active": "Active",
		"grid.members.lastSeenAt": "Last seen", "grid.members.prefs": "Preferences", "grid.members.id": "ID",
		"grid.operators.eq": "equals", "grid.operators.neq": "does not equal",
		"grid.operators.contains": "contains", "grid.operators.notContains": "does not contain",
		"grid.operators.starts": "starts with", "grid.operators.ends": "ends with",
		"grid.operators.gt": "after", "grid.operators.gte": "on or after",
		"grid.operators.lt": "before", "grid.operators.lte": "on or before", "grid.operators.between": "between", "grid.operators.notBetween": "not between",
		"grid.operators.in": "is one of", "grid.operators.notIn": "is not one of",
		"grid.operators.isNull": "is empty", "grid.operators.isNotNull": "is not empty",
		"grid.operators.containsAny": "contains any of", "grid.operators.containsAll": "contains all of",
		"grid.operators.notContainsAny": "contains none of",
		"grid.operators.isEmpty":        "is empty list", "grid.operators.isNotEmpty": "is not empty list",
	},
	"ru": {
		"grid.members.email": "Почта", "grid.members.team": "Команда", "grid.members.active": "Активен",
		"grid.members.lastSeenAt": "Был в сети", "grid.members.prefs": "Настройки", "grid.members.id": "ID",
		"grid.operators.eq": "равно", "grid.operators.neq": "не равно",
		"grid.operators.contains": "содержит", "grid.operators.notContains": "не содержит",
		"grid.operators.starts": "начинается с", "grid.operators.ends": "заканчивается на",
		"grid.operators.gt": "после", "grid.operators.gte": "не раньше",
		"grid.operators.lt": "до", "grid.operators.lte": "не позже", "grid.operators.between": "между", "grid.operators.notBetween": "вне диапазона",
		"grid.operators.in": "один из", "grid.operators.notIn": "не один из",
		"grid.operators.isNull": "пусто", "grid.operators.isNotNull": "не пусто",
		"grid.operators.containsAny": "содержит любой из", "grid.operators.containsAll": "содержит все из",
		"grid.operators.notContainsAny": "не содержит ни одного из",
		"grid.operators.isEmpty":        "пустой список", "grid.operators.isNotEmpty": "непустой список",
	},
}

func translate(locale, key string) string {
	if t, ok := i18n[locale][key]; ok {
		return t
	}
	if t, ok := i18n["en"][key]; ok {
		return t
	}
	return key
}

func locale(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("Accept-Language"), "ru") {
		return "ru"
	}
	return "en"
}

// guard rejects requests without the API key and stores the caller's role
// in the context for ForContext to read.
func guard(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != apiKey {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), roleKey{}, r.Header.Get("X-Role"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := seed(ctx, pool); err != nil {
		log.Fatal(err)
	}

	compiler := grjet.Compiler{}
	reg, err := gridraw.NewRegistry(compiler, membersGrid())
	if err != nil {
		log.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	h := gridraw.NewHandler(gridraw.Options{
		Registry:   reg,
		Translator: translate,
		Locale:     locale,
		Compiler:   compiler,
		Executor:   grpgx.New(pool),
		Log:        logger, // query failures are logged here and answered as 500 "query failed"
	})

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "secret"
	}
	mux := http.NewServeMux()
	grstd.Register(mux, "/api/grids", guard(apiKey), h)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	logger.Info("listening", "addr", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func seed(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS example_advanced_teams (
			id   serial PRIMARY KEY,
			name text NOT NULL
		);
		CREATE TABLE IF NOT EXISTS example_advanced_members (
			id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			email        text NOT NULL,
			team_id      integer NOT NULL REFERENCES example_advanced_teams(id),
			active       boolean,
			last_seen_at timestamptz,
			prefs        jsonb NOT NULL DEFAULT '{}'
		)`)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO example_advanced_teams (name)
		SELECT * FROM (VALUES ('platform'), ('growth')) AS v(name)
		WHERE NOT EXISTS (SELECT 1 FROM example_advanced_teams);
		INSERT INTO example_advanced_members (email, team_id, active, last_seen_at, prefs)
		SELECT v.email, t.id, v.active, v.last_seen_at, v.prefs::jsonb
		FROM (VALUES
			('ann@example.com',   'platform', true,  now() - interval '2 hours', '{"theme":"dark"}'),
			('bob@example.com',   'growth',   false, now() - interval '9 days',  '{}'),
			('carol@example.com', 'platform', NULL,  NULL,                       '{"theme":"light","beta":true}'),
			('dan@example.com',   'growth',   true,  now() - interval '1 day',   '{}')
		) AS v(email, team, active, last_seen_at, prefs)
		JOIN example_advanced_teams t ON t.name = v.team
		WHERE NOT EXISTS (SELECT 1 FROM example_advanced_members)`)
	return err
}
