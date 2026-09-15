package main

import (
	"fmt"
	"net/http"
)

// portMiddleware intercepts GET /tsession-port and reports the port the
// embedded internal/webui server is actually listening on, as JSON. Every
// other request is passed straight through to next (the Wails asset
// server, which serves gui/frontend/dist). This is how the static loader
// page (frontend/dist/index.html) learns where to navigate without any
// Wails Go<->JS runtime binding.
func portMiddleware(port func() int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tsession-port" {
			p := port()
			if p <= 0 {
				http.Error(w, "tsession embedded server is not ready", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"port":%d}`, p)
			return
		}
		next.ServeHTTP(w, r)
	})
}
