package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/app"
	_ "modernc.org/sqlite"
)

// TestStage5BrowserE2E is deliberately opt-in: it drives the shipped
// authenticated shell in Edge/Playwright against app.ExecuteWithProgress and
// a real tiny SQLite migration. No browser fixture may synthesize command
// outcomes; only the surrounding browser tooling is external.
func TestStage5BrowserE2E(t *testing.T) {
	if os.Getenv("DMTX_STAGE5_BROWSER") != "1" {
		t.Skip("set DMTX_STAGE5_BROWSER=1 with Edge/Playwright paths")
	}
	required := func(name string) string {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			t.Fatalf("%s is required", name)
		}
		return value
	}
	node, playwright, edge := required("DMTX_STAGE5_NODE"), required("DMTX_STAGE5_PLAYWRIGHT"), required("DMTX_STAGE5_EDGE")
	root := t.TempDir()
	t.Setenv("HOME", root)
	sourcePath, targetPath := filepath.Join(root, "source.db"), filepath.Join(root, "target.db")
	configPath, statePath := filepath.Join(root, "migration.yaml"), filepath.Join(root, "migration.state.db")
	setupPath := filepath.Join(root, "guided.yaml")
	setupTargetPath := filepath.Join(root, "guided-target.db")
	cancelSetupPath := filepath.Join(root, "cancelled.yaml")
	source, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT); INSERT INTO notes (body) VALUES ('browser fixture')`); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("source:\n  type: sqlite\n  database: %q\ntarget:\n  type: sqlite\n  database: %q\nmigration:\n  target_mode: drop_recreate\n", sourcePath, targetPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Root: root, SessionPath: filepath.Join(root, "session.json")})
	if err != nil {
		t.Fatal(err)
	}
	// The ordinary commands below stay on the real application seam.  Only the
	// second run is held open so the browser can prove cancellation/reload
	// recovery deterministically without making a database migration slow.
	runs := 0
	server.jobs.execute = func(command context.Context, request app.Request, report app.ProgressFunc) app.Outcome {
		if request.Command != "run" || runs == 0 {
			if request.Command == "run" {
				runs++
			}
			return app.ExecuteWithProgress(command, request, report)
		}
		<-command.Done()
		return app.Outcome{Command: "run", ExitCode: app.Cancelled}
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
	})
	rootPath, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{
		"url": server.URL(), "config": configPath, "state": statePath,
		"source": sourcePath, "target": targetPath, "setup": setupPath,
		"setupTarget": setupTargetPath, "cancelSetup": cancelSetupPath,
	})
	command := exec.CommandContext(t.Context(), node, filepath.Join(rootPath, "test", "browser", "stage5.cjs"), playwright, edge, string(input))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Edge acceptance: %v\n%s", err, output)
	}
	target, err := sql.Open("sqlite", targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var copied int
	if err := target.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&copied); err != nil || copied != 1 {
		t.Fatalf("browser did not run real migration: copied=%d err=%v", copied, err)
	}
	if _, err := os.Stat(setupPath); err != nil {
		t.Fatalf("browser setup did not write its configuration: %v", err)
	}
	if _, err := os.Stat(cancelSetupPath); !os.IsNotExist(err) {
		t.Fatalf("cancelled browser setup wrote a configuration: %v", err)
	}
}
