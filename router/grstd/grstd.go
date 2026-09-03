// Package grstd mounts a gridraw.Handler on net/http's ServeMux.
package grstd

import (
	"net/http"

	"github.com/qrotux/gridraw-go"
)

// Register mounts GET base/{name} and POST base/{name}/rows; guard may be nil.
func Register(mux *http.ServeMux, base string, guard func(http.Handler) http.Handler, h *gridraw.Handler) {
	wrap := func(f func(http.ResponseWriter, *http.Request, string)) http.Handler {
		var hh http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f(w, r, r.PathValue("name"))
		})
		if guard != nil {
			hh = guard(hh)
		}
		return hh
	}
	mux.Handle("GET "+base+"/{name}", wrap(h.Descriptor))
	mux.Handle("POST "+base+"/{name}/rows", wrap(h.Rows))
}
