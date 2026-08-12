package syncd

import (
	"embed"
	"html/template"
	"net/http"
	"path"
	"time"
)

//go:embed sharepage.html
var sharePageHTML string

//go:embed sharestatic
var shareStaticFS embed.FS

var sharePageTmpl = template.Must(template.New("share").Parse(sharePageHTML))

func writeSharePage(w http.ResponseWriter, share *ShareEntry) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = sharePageTmpl.Execute(w, map[string]interface{}{
		"Token":     share.Token,
		"Name":      share.Name,
		"ExpiresAt": share.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func writeShareStatic(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	var contentType string
	switch name {
	case "xterm.js", "xterm-addon-fit.js":
		contentType = "text/javascript; charset=utf-8"
	case "xterm.css":
		contentType = "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := shareStaticFS.ReadFile("sharestatic/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = w.Write(data)
}
