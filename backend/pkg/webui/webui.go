// Package webui embeds and serves the built React frontend, so the whole
// application (UI + API + every registry protocol) is reachable through a
// single port — required for running cargobay behind a Kubernetes ingress
// controller, which cannot address multiple backend ports/containers.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded frontend build. It mirrors nginx's
// `try_files $uri $uri/ /index.html`: static assets are served directly when
// present, and any other path falls back to index.html so client-side
// routing (React Router) can handle deep links and hard refreshes.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if f, err := sub.Open(trimLeadingSlash(path)); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
