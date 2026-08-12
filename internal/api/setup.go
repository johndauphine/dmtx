package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/johndauphine/dmtx/internal/app"
)

// setupState owns the server's one guided setup conversation. The conversation
// itself is application state; this mutex only makes replacing or advancing it
// safe when a browser retries a request while another is in flight.
type setupState struct {
	mu   sync.Mutex
	flow app.SetupFlow
}

// setupStart begins a fresh guided setup session. Starting again is explicit
// replacement, matching a browser operator choosing to start over rather than
// allowing two half-completed configurations to interleave.
func (server *Server) setupStart(writer http.ResponseWriter, request *http.Request) {
	var asked struct {
		ConfigPath  string `json:"config_path"`
		Engine      string `json:"engine"`
		ProfileName string `json:"profile_name"`
	}
	if !decodeSetupRequest(writer, request, &asked) {
		return
	}
	if asked.ProfileName != "" && asked.ConfigPath != "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "choose a configuration path or profile, not both",
		})
		return
	}
	var flow app.SetupFlow
	var err error
	if asked.ProfileName != "" {
		flow, err = app.NewSetupForProfile(asked.ProfileName, "", asked.Engine)
	} else {
		flow, err = app.NewSetupForEngine(asked.ConfigPath, asked.Engine)
	}
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{
			"error": app.SetupStartErrorMessage(err),
		})
		return
	}
	server.setup.mu.Lock()
	server.setup.flow = flow
	prompt := server.setup.flow.Prompt()
	server.setup.mu.Unlock()
	writeJSON(writer, http.StatusOK, prompt)
}

func (server *Server) setupPrompt(writer http.ResponseWriter, request *http.Request) {
	server.setup.mu.Lock()
	defer server.setup.mu.Unlock()
	if server.setup.flow == nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "setup has not started"})
		return
	}
	writeJSON(writer, http.StatusOK, server.setup.flow.Prompt())
}

func (server *Server) setupInput(writer http.ResponseWriter, request *http.Request) {
	var asked struct {
		Input string `json:"input"`
	}
	if !decodeSetupRequest(writer, request, &asked) {
		return
	}
	server.setup.mu.Lock()
	defer server.setup.mu.Unlock()
	if server.setup.flow == nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "setup has not started"})
		return
	}
	writeJSON(writer, http.StatusOK, server.setup.flow.Input(asked.Input))
}

func decodeSetupRequest(writer http.ResponseWriter, request *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "malformed request: " + err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "request body holds more than one JSON document"})
		return false
	}
	return true
}
