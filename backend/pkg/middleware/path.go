package middleware

import "net/http"

// NormalizePath clears r.URL.RawPath so chi's router always matches and
// extracts params from the already-decoded r.URL.Path. chi prefers RawPath
// over Path when Go's net/url has set it (which happens whenever a client
// percent-encodes a character, like ':', that doesn't strictly require
// encoding in a path segment) — without this, route params for values
// containing such characters (e.g. our colon-delimited artifact IDs) come
// back still percent-encoded, silently breaking lookups keyed on them.
func NormalizePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawPath = ""
		next.ServeHTTP(w, r)
	})
}
