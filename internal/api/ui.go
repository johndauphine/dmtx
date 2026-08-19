package api

import (
	_ "embed"
	"net/http"
)

//go:embed static/index.html
var consoleHTML []byte

//go:embed static/console.js
var consoleJS []byte

//go:embed static/console.css
var consoleCSS []byte

//go:embed static/manifest.webmanifest
var consoleManifest []byte

//go:embed static/icon.svg
var consoleIcon []byte

//go:embed static/sw.js
var consoleWorker []byte

const consoleCSP = "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'; require-trusted-types-for 'script'"

func consoleHeaders(writer http.ResponseWriter) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Content-Security-Policy", consoleCSP)
}

func (server *Server) console(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	consoleHeaders(writer)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(consoleHTML)
}

// consoleAsset serves the shell's fixed, same-origin assets. The browser
// cannot name an arbitrary embedded file, so adding a future asset remains an
// explicit routing decision instead of a file-serving surface.
func (server *Server) consoleAsset(writer http.ResponseWriter, request *http.Request) {
	consoleHeaders(writer)
	switch request.URL.Path {
	case "/static/console.js":
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = writer.Write(consoleJS)
	case "/static/console.css":
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = writer.Write(consoleCSS)
	case "/static/icon.svg":
		writer.Header().Set("Content-Type", "image/svg+xml")
		_, _ = writer.Write(consoleIcon)
	case "/manifest.webmanifest":
		writer.Header().Set("Content-Type", "application/manifest+json")
		_, _ = writer.Write(consoleManifest)
	case "/sw.js":
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = writer.Write(consoleWorker)
	default:
		http.NotFound(writer, request)
	}
}
