// Package web serves rflib's web interface: the single-page app built from
// web/src into web/dist, embedded in the binary. Build it with make web;
// without a build, a page says so.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const placeholder = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>rflib</title></head>
<body style="font-family: sans-serif; max-width: 40em; margin: 4em auto">
<h1>rflib</h1>
<p>This rflib was built without its web interface. Run <code>make web</code>
(it needs Node.js), then build rflib again. The API is at
<a href="/api/v1/openapi.yaml">/api/v1</a>.</p>
</body></html>
`

// Built reports whether the web interface was built into this binary.
func Built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

// Handler serves the app. Paths that aren't files get index.html, so the
// app's own routes (such as /radios/HT) load it too.
func Handler() http.Handler {
	if !Built() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(placeholder))
		})
	}
	root, _ := fs.Sub(dist, "dist")
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Built assets are named by their content.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, root, "index.html")
	})
}
