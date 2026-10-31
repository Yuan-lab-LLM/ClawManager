package backupgate

import (
	"net/http"
)

// HTTPBoundary covers the separately served internal Core API. Passing the
// original ResponseWriter preserves streaming/Hijacker behavior; release is
// tied to actual handler completion rather than client cancellation.
func HTTPBoundary(next http.Handler, unit string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		release, err := HoldWorkUnit(r.Context(), unit)
		if err != nil {
			http.Error(w, "backup_maintenance", http.StatusServiceUnavailable)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
