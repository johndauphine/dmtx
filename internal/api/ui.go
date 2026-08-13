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

//go:embed static/service-worker.js
var consoleServiceWorker []byte

//go:embed static/icon.svg
var consoleIcon []byte

//go:embed static/icon-192.png
var consoleIcon192 []byte

//go:embed static/icon-512.png
var consoleIcon512 []byte

const consoleCSP = "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'; require-trusted-types-for 'script'; trusted-types dmtx-service-worker"

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
	case "/static/manifest.webmanifest":
		writer.Header().Set("Content-Type", "application/manifest+json")
		_, _ = writer.Write(consoleManifest)
	case "/static/service-worker.js":
		// A service worker must be able to control the root shell, while the
		// handler remains a fixed embedded asset behind session auth.
		writer.Header().Set("Service-Worker-Allowed", "/")
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = writer.Write(consoleServiceWorker)
	case "/static/icon.svg":
		writer.Header().Set("Content-Type", "image/svg+xml")
		_, _ = writer.Write(consoleIcon)
	case "/static/icon-192.png":
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(consoleIcon192)
	case "/static/icon-512.png":
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(consoleIcon512)
	default:
		http.NotFound(writer, request)
	}
}
