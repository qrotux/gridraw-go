// Command basic serves one grid over a users table with chi, go-jet and pgx.
//
//	DATABASE_URL=postgres://user:pass@localhost:5432/db go run ./examples/basic
//	curl localhost:8080/api/grids/users
//	curl localhost:8080/api/grids/-/list
//	curl localhost:8080/api/grids/-/registry
//	curl -X POST localhost:8080/api/grids/users/rows -d '{"columns":["email","role"],"search":"ann"}'
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-jet/jet/v2/postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qrotux/gridraw-go"
	"github.com/qrotux/gridraw-go/adapter/grjet"
	"github.com/qrotux/gridraw-go/adapter/grpgx"
	"github.com/qrotux/gridraw-go/router/grchi"
)

// Column definitions written by hand; a real project uses go-jet's generator.
var (
	colID        = postgres.StringColumn("id")
	colEmail     = postgres.StringColumn("email")
	colRole      = postgres.StringColumn("role")
	colRating    = postgres.FloatColumn("rating")
	colIsBanned  = postgres.BoolColumn("is_banned")
	colCreatedAt = postgres.TimestampzColumn("created_at")
	users        = postgres.NewTable("public", "example_basic_users", "", colID, colEmail, colRole, colRating, colIsBanned, colCreatedAt)
)

var titles = map[string]string{
	"grid.users.id":                 "ID",
	"grid.users.email":              "Email",
	"grid.users.role":               "Role",
	"grid.users.rating":             "Rating",
	"grid.users.isBanned":           "Banned",
	"grid.users.createdAt":          "Created",
	"grid.users.role_values.user":   "User",
	"grid.users.role_values.admin":  "Admin",
	"grid.operators.eq":             "equals",
	"grid.operators.neq":            "does not equal",
	"grid.operators.contains":       "contains",
	"grid.operators.notContains":    "does not contain",
	"grid.operators.starts":         "starts with",
	"grid.operators.ends":           "ends with",
	"grid.operators.gt":             ">",
	"grid.operators.gte":            "≥",
	"grid.operators.lt":             "<",
	"grid.operators.lte":            "≤",
	"grid.operators.between":        "between",
	"grid.operators.notBetween":     "not between",
	"grid.operators.in":             "is one of",
	"grid.operators.notIn":          "is not one of",
	"grid.operators.isNull":         "is empty",
	"grid.operators.isNotNull":      "is not empty",
	"grid.operators.containsAny":    "contains any of",
	"grid.operators.containsAll":    "contains all of",
	"grid.operators.notContainsAny": "contains none of",
	"grid.operators.isEmpty":        "is empty list",
	"grid.operators.isNotEmpty":     "is not empty list",
}

func translate(_ string, key string) string {
	if t, ok := titles[key]; ok {
		return t
	}
	return key
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

	grid := gridraw.Grid{
		Name:        "users",
		Description: "Application users",
		IDColumn:    "id",
		PageSize:    10,
		DefaultSort: gridraw.SortSpec{Column: "createdAt", Dir: "desc"},
		Binding:     grjet.Base(func() postgres.ReadableTable { return users }),
		Columns: []gridraw.Column{
			grjet.UUIDCol("id", colID),
			grjet.StrCol("email", colEmail).WithSearch().Vis().WithDescription("Login email"),
			grjet.EnumCol("role", colRole, []string{"user", "admin"}).Vis(),
			grjet.NumCol("rating", colRating),
			grjet.BoolCol("isBanned", colIsBanned),
			grjet.TsCol("createdAt", colCreatedAt).Vis(),
		},
	}

	compiler := grjet.Compiler{}
	reg, err := gridraw.NewRegistry(compiler, grid)
	if err != nil {
		log.Fatal(err) // a broken grid definition fails here, at startup
	}
	h := gridraw.NewHandler(gridraw.Options{
		Registry:   reg,
		Translator: translate,
		Locale:     func(*http.Request) string { return "en" },
		Compiler:   compiler,
		Executor:   grpgx.New(pool),
	})

	r := chi.NewRouter()
	grchi.Register(r, "/api/grids", nil, h)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func seed(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS example_basic_users (
			id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			email      text NOT NULL,
			role       text NOT NULL,
			rating     numeric,
			is_banned  boolean NOT NULL DEFAULT false,
			created_at timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO example_basic_users (email, role, rating, is_banned, created_at)
		SELECT * FROM (VALUES
			('ann@example.com',   'admin', 4.8,  false, now() - interval '30 days'),
			('bob@example.com',   'user',  3.2,  false, now() - interval '20 days'),
			('carol@example.com', 'user',  NULL, true,  now() - interval '10 days'),
			('dan@example.com',   'user',  4.1,  false, now() - interval '5 days'),
			('eve@example.com',   'admin', 2.7,  false, now() - interval '1 day')
		) AS v(email, role, rating, is_banned, created_at)
		WHERE NOT EXISTS (SELECT 1 FROM example_basic_users)`)
	return err
}
