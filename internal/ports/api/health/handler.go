// Package health — health-check endpoint.
package health

import "net/http"

// Register регистрирует /health.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}
