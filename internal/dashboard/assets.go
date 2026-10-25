package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var assets embed.FS

// Handler serves the Vite build embedded in the exchange binary.
func Handler() http.Handler {
	files, _ := fs.Sub(assets, "dist")
	server := http.FileServer(http.FS(files))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			if _, err := fs.Stat(files, "index.html"); err != nil {
				http.Error(w, "Dashboard assets missing. Run npm --prefix web ci && npm --prefix web run build, then rebuild/restart the exchange.", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		server.ServeHTTP(w, r)
	})
}
