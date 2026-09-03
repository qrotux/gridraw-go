package grstd

import (
	"net/http"
	"testing"

	"github.com/qrotux/gridraw-go"
)

func mount(t *testing.T, base string, guard func(http.Handler) http.Handler, h *gridraw.Handler) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, base, guard, h)
	return mux
}
