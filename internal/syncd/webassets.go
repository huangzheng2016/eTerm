package syncd

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed webapp.html
var webAppHTML string

//go:embed webstatic
var webStaticFS embed.FS

// RegisterWebRoutes mounts the web SPA on mux:
//
//	GET /login         login page
//	GET /app/...       SPA shell (login state is enforced client side via /api/v1/web/me)
//	GET /webstatic/... embedded static assets (xterm.* is served from the share embed)
//	GET /*              SPA fallback, except /api/, /x/ and /b/ which keep their 404s
func RegisterWebRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", writeWebApp)
	mux.HandleFunc("GET /app", writeWebApp)
	mux.HandleFunc("GET /app/", writeWebApp)
	mux.HandleFunc("GET /webstatic/", writeWebStatic)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/x/") || strings.HasPrefix(p, "/b/") {
			http.NotFound(w, r)
			return
		}
		writeWebApp(w, r)
	})
}

func writeWebApp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(webAppHTML))
}

func writeWebStatic(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	var contentType string
	switch path.Ext(name) {
	case ".js":
		contentType = "text/javascript; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := webStaticFS.ReadFile("webstatic/" + name)
	if err != nil {
		// xterm assets are shared with the share page instead of duplicated.
		data, err = shareStaticFS.ReadFile("sharestatic/" + name)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(data)
}
