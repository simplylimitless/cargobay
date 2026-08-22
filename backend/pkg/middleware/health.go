package middleware

import (
	"encoding/json"
	"net/http"
)

// pinger is implemented by dependencies that can report connectivity.
type pinger interface {
	Ping() error
}

// storageConnector is implemented by storage.StorageAdapter; checked via
// Connect() since adapters expose no separate ping (Connect is idempotent
// for all four backends).
type storageConnector interface {
	Connect() error
}

// NewReadinessHandler returns a /healthz handler that checks the database,
// cache, and storage backend are actually reachable, rather than just
// confirming the process is running (which is all chi's Heartbeat on
// /health does).
func NewReadinessHandler(db pinger, cache pinger, storage storageConnector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		healthy := true

		if err := db.Ping(); err != nil {
			checks["database"] = err.Error()
			healthy = false
		} else {
			checks["database"] = "ok"
		}

		if err := cache.Ping(); err != nil {
			checks["cache"] = err.Error()
			healthy = false
		} else {
			checks["cache"] = "ok"
		}

		if err := storage.Connect(); err != nil {
			checks["storage"] = err.Error()
			healthy = false
		} else {
			checks["storage"] = "ok"
		}

		status := "ok"
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"checks": checks,
		})
	}
}
