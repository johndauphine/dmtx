package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/johndauphine/dmtx/internal/app"
	_ "modernc.org/sqlite"
)

// TestSuccessfulValidateHasItsOwnPayloadKindAndAPIParity makes the successful
// validation response part of the surface-parity contract. Validation returns
// per-table count findings, not migrate.Result's aggregate transfer fields, so
// a consumer must be able to choose its renderer from the kind alone.
func TestSuccessfulValidateHasItsOwnPayloadKindAndAPIParity(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	targetPath := filepath.Join(directory, "target.db")
	for _, path := range []string{sourcePath, targetPath} {
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT); INSERT INTO notes (body) VALUES ('first');`); err != nil {
			_ = database.Close()
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(directory, "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: " + sourcePath + "\ntarget:\n  type: sqlite\n  database: " + targetPath + "\n"
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	command := app.Request{Command: "validate", ConfigPath: configPath}
	direct := app.Execute(context.Background(), command)
	if direct.ExitCode != app.Success || direct.Payload == nil {
		t.Fatalf("direct validation = %+v", direct)
	}
	if direct.Payload.Kind != app.PayloadValidationResult {
		t.Fatalf("direct validation payload kind = %q, want %q", direct.Payload.Kind, app.PayloadValidationResult)
	}
	var result struct {
		Passed bool `json:"passed"`
		Tables []struct {
			Table      string `json:"table"`
			SourceRows int    `json:"source_rows"`
			TargetRows int    `json:"target_rows"`
			Match      bool   `json:"match"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(direct.Payload.Data, &result); err != nil {
		t.Fatalf("decode validation result: %v", err)
	}
	if !result.Passed || len(result.Tables) != 1 ||
		result.Tables[0].Table != "notes" ||
		result.Tables[0].SourceRows != 1 || result.Tables[0].TargetRows != 1 ||
		!result.Tables[0].Match {
		t.Fatalf("validation result = %+v", result)
	}

	server := newTestServer(t)
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("API validation = %d: %s", recorder.Code, recorder.Body)
	}
	want, err := json.Marshal(direct)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.TrimSpace(recorder.Body.Bytes()); !bytes.Equal(got, want) {
		t.Fatalf("API validation response differs from direct outcome:\n  direct: %s\n  API: %s", want, got)
	}
}
