// Package grchi mounts a gridraw.Handler on a chi router.
package grchi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/qrotux/gridraw-go"
)

// Register mounts GET base/{name} and POST base/{name}/rows; guard may be nil.
func Register(r chi.Router, base string, guard func(http.Handler) http.Handler, h *gridraw.Handler) {
	if guard == nil {
		guard = func(next http.Handler) http.Handler { return next }
	}
	r.With(guard).Get(base+"/{name}", func(w http.ResponseWriter, req *http.Request) {
		h.Descriptor(w, req, chi.URLParam(req, "name"))
	})
	r.With(guard).Post(base+"/{name}/rows", func(w http.ResponseWriter, req *http.Request) {
		h.Rows(w, req, chi.URLParam(req, "name"))
	})
}
