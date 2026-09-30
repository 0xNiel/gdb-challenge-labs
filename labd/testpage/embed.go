// Package testpage serves the dev-only xterm.js page at /dev/term (Phase 3, task 3.8). labd
// mounts it only when dev_testpage is on. Like sandbox/embed.go, it sits outside internal/
// because its files live here (labd/README.md).
package testpage

import (
	"embed"
	"net/http"
)

//go:embed index.html vendor/*.js vendor/*.css
var files embed.FS

// Handler serves GET /dev/term (the page) and GET /dev/vendor/* (xterm.js).
func Handler() http.Handler {
	static := http.StripPrefix("/dev/", http.FileServerFS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/dev/term" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			b, _ := files.ReadFile("index.html")
			_, _ = w.Write(b)
			return
		}
		static.ServeHTTP(w, r)
	})
}
