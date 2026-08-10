package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/app"
)

func TestSetupAPIStartsAndDrivesApplicationPrompts(t *testing.T) {
	server := newTestServer(t)
	routes := server.routes()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/prompt", nil)
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("prompt before start = %d, want %d", recorder.Code, http.StatusConflict)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/setup/start", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder = httptest.NewRecorder()
	routes.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("start = %d, want %d", recorder.Code, http.StatusOK)
	}
	var prompt app.SetupPrompt
	if err := json.NewDecoder(recorder.Body).Decode(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.Step != "source_database" {
		t.Fatalf("start prompt = %+v", prompt)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/setup/input", strings.NewReader(`{"input":"missing.db"}`))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder = httptest.NewRecorder()
	routes.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("input = %d, want %d", recorder.Code, http.StatusOK)
	}
	if err := json.NewDecoder(recorder.Body).Decode(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.Step != "source_database" || prompt.Error == "" {
		t.Fatalf("application validation was not returned: %+v", prompt)
	}
}

func TestSetupAPIRefusesMalformedRequests(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/start", strings.NewReader(`{"unknown":true}`))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown setup field = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSetupAPICompletesAConfigurationThroughApplicationFlow(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "migration.yaml")
	server := newTestServer(t)
	routes := server.routes()
	call := func(endpoint string, value any) app.SetupPrompt {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(data)))
		request.Header.Set("Authorization", "Bearer "+server.auth.session)
		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s = %d, want %d", endpoint, recorder.Code, http.StatusOK)
		}
		var prompt app.SetupPrompt
		if err := json.NewDecoder(recorder.Body).Decode(&prompt); err != nil {
			t.Fatal(err)
		}
		return prompt
	}
	prompt := call("/api/v1/setup/start", map[string]string{"config_path": path})
	if prompt.Step != "source_database" {
		t.Fatalf("start prompt = %+v", prompt)
	}
	for _, input := range []string{source, filepath.Join(directory, "target.db"), "drop_recreate", path, "yes"} {
		prompt = call("/api/v1/setup/input", map[string]string{"input": input})
	}
	if !prompt.Done || prompt.Error != "" {
		t.Fatalf("completion prompt = %+v", prompt)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("setup API did not write the configuration: %v", err)
	}
}

func TestSetupAPIAcceptsPostgresWorkflow(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/start", strings.NewReader(`{"engine":"postgres"}`))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("postgres start = %d, want %d", recorder.Code, http.StatusOK)
	}
	var prompt app.SetupPrompt
	if err := json.NewDecoder(recorder.Body).Decode(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.Step != "source_host" || prompt.Masked {
		t.Fatalf("postgres start prompt = %+v", prompt)
	}
}
