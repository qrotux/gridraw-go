// Command translations prints descriptors showing translations and literal title fallbacks without a database.
//
//	go run ./examples/translations
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/qrotux/gridraw-go"
)

var translations = map[string]map[string]string{
	"en": {
		"grid.users.id":                "ID",
		"grid.users.role":              "Role",
		"grid.users.description":       "Application users",
		"grid.users.role_values.user":  "User",
		"grid.users.role_values.admin": "Admin",
		"grid.operators.eq":            "equals",
		"grid.operators.in":            "is one of",
	},
	"ru": {
		"grid.users.id":                "ID",
		"grid.users.email":             "Почта",
		"grid.users.role":              "Роль",
		"grid.users.description":       "Пользователи приложения",
		"grid.users.role_values.user":  "Пользователь",
		"grid.users.role_values.admin": "Администратор",
		"grid.operators.eq":            "равно",
		"grid.operators.in":            "один из",
	},
}

func translate(locale, key string) string {
	if text, ok := translations[locale][key]; ok {
		return text
	}
	return key
}

func main() {
	grid := gridraw.Grid{
		Name: "users", Description: "User directory", IDColumn: "id", PageSize: 25,
		DefaultSort: gridraw.SortSpec{Column: "email", Dir: "asc"},
		Columns: []gridraw.Column{
			{Key: "id", Type: gridraw.TypeUUID},
			{Key: "email", Type: gridraw.TypeString, Sortable: true, Searchable: true, DefaultVisible: true,
				Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpEq}}},
			{Key: "role", Type: gridraw.TypeEnum, DefaultVisible: true, Enum: []string{"user", "admin"},
				Filter: &gridraw.FilterSpec{Operators: []gridraw.Op{gridraw.OpIn}}},
		},
	}
	// English deliberately omits the email title; the Russian translation takes precedence over this fallback.
	withTitles := gridraw.WithTitles(translate, map[string]string{
		"grid.users.email": "Email address",
	})
	output := map[string]gridraw.Descriptor{
		"withoutTranslator": gridraw.BuildDescriptor(&grid, nil, ""),
		"english":           gridraw.BuildDescriptor(&grid, translate, "en"),
		"englishWithTitles": gridraw.BuildDescriptor(&grid, withTitles, "en"),
		"russianWithTitles": gridraw.BuildDescriptor(&grid, withTitles, "ru"),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		log.Fatal(err)
	}
}
