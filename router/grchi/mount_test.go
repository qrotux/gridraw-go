package grchi

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/qrotux/gridraw-go"
)

func mount(t *testing.T, base string, guard func(http.Handler) http.Handler, h *gridraw.Handler) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	Register(r, base, guard, h)
	return r
}
