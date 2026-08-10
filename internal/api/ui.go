package api

import (
	_ "embed"
	"net/http"
)

//go:embed static/index.html
var consoleHTML []byte

func (server *Server) console(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(consoleHTML)
}
