package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/app"
	"github.com/johndauphine/dmtx/internal/state"
)

// TestBareHistoryReadsTheConsoleState verifies the browser path rather than
// only app's --state handling. A WebUI server gives a project a durable SQLite
// state, so /history is useful before an operator has configured a session
// default and returns the runs that console has recorded.
func TestBareHistoryReadsTheConsoleState(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	statePath := consoleStatePath(sessionPath, root)
	server, err := New(Options{
		Root:             root,
		DefaultStatePath: statePath,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.listener.Close() })

	// The parse response is what the console gives back to the jobs endpoint,
	// so it must reveal the resolved fallback rather than leave the command to
	// fail later with the CLI usage line.
	parsed := parseLine(t, server, "/history")
	if !parsed.Dispatched {
		t.Fatalf("/history was not dispatched: %+v", parsed.Outcome)
	}
	if parsed.Request.StatePath != statePath {
		t.Fatalf("history state = %q, want console state %q", parsed.Request.StatePath, statePath)
	}

	store, err := state.NewBackend(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeRun(state.Run{
		ID:        "console-run",
		Source:    "source.db",
		Target:    "target.db",
		Outcome:   state.Running,
		Resumable: true,
		StartedAt: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}, "config-hash"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/execute",
		strings.NewReader(`{"command":"history"}`),
	)
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("history returned %d: %s", recorder.Code, recorder.Body)
	}

	var outcome app.Outcome
	if err := json.NewDecoder(recorder.Body).Decode(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.ExitCode != app.Success || outcome.Payload == nil || outcome.Payload.Kind != app.PayloadRuns {
		t.Fatalf("history outcome = %+v", outcome)
	}
	if !strings.Contains(string(outcome.Payload.Data), "console-run") {
		t.Fatalf("history omitted the console run: %s", outcome.Payload.Data)
	}
}

// TestSessionStateBeatsTheConsoleFallback keeps the explicit-session,
// project-fallback precedence visible. A user who selected another state file
// must never have /history silently redirected to the console database.
func TestSessionStateBeatsTheConsoleFallback(t *testing.T) {
	server := newTestServer(t)
	server.defaultStatePath = "console.state.db"
	if err := server.defaults.set(SessionState, "selected.state.db"); err != nil {
		t.Fatal(err)
	}

	parsed := parseLine(t, server, "/history")
	if parsed.Request.StatePath != "selected.state.db" {
		t.Fatalf("history state = %q, want session state", parsed.Request.StatePath)
	}
}

// TestStateBearingJobBecomesBareHistoryDefault covers the browser's two-step
// flow: parse a run, start the resolved request as a job, then parse a bare
// history command. The config-derived state is application-owned parsing, and
// the API must retain that exact result rather than falling back to an empty
// console database.
func TestStateBearingJobBecomesBareHistoryDefault(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	server, err := New(Options{
		Root:             root,
		SessionPath:      sessionPath,
		DefaultStatePath: consoleStatePath(sessionPath, root),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = server.listener.Close() })
	server.jobs.execute = func(_ context.Context, request app.Request, _ app.ProgressFunc) app.Outcome {
		return app.Outcome{Command: request.Command, ExitCode: app.Success}
	}

	configPath := filepath.Join(root, "migration.yaml")
	run := parseLine(t, server, "/run --config "+configPath)
	if !run.Dispatched {
		t.Fatalf("run was not dispatched: %+v", run.Outcome)
	}
	wantState := configPath + ".state.db"
	if run.Request.StatePath != wantState {
		t.Fatalf("run state = %q, want config-derived %q", run.Request.StatePath, wantState)
	}

	body, err := json.Marshal(run.Request)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer "+server.auth.session)
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("start run = %d: %s", recorder.Code, recorder.Body)
	}
	if got := server.defaults.get(SessionState); got != wantState {
		t.Fatalf("remembered state = %q, want %q", got, wantState)
	}
	if got := newSessionDefaults(sessionPath).get(SessionState); got != wantState {
		t.Fatalf("persisted state = %q, want %q", got, wantState)
	}

	history := parseLine(t, server, "/history")
	if !history.Dispatched || history.Request.StatePath != wantState {
		t.Fatalf("bare history = %+v, want state %q", history, wantState)
	}
}

func TestConsoleStatePathIsStableAndProjectScoped(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()

	first := consoleStatePath(sessionPath, firstRoot)
	if again := consoleStatePath(sessionPath, firstRoot); again != first {
		t.Fatalf("same project state changed from %q to %q", first, again)
	}
	if second := consoleStatePath(sessionPath, secondRoot); second == first {
		t.Fatalf("different projects share console state %q", first)
	}
	if filepath.Dir(first) != filepath.Dir(sessionPath) {
		t.Fatalf("console state %q is not beside session state %q", first, sessionPath)
	}
}

// TestProjectSessionsDoNotLeakRememberedStateAcrossRestarts proves the
// project namespace covers the session file as well as the empty fallback.
// Otherwise project B would load project A's remembered SQLite path, and that
// explicit-looking value would defeat B's own fallback on the next restart.
func TestProjectSessionsDoNotLeakRememberedStateAcrossRestarts(t *testing.T) {
	stateDirectory := t.TempDir()
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	firstSession := consoleSessionPath(stateDirectory, firstRoot)
	secondSession := consoleSessionPath(stateDirectory, secondRoot)
	if firstSession == secondSession {
		t.Fatalf("project sessions collide at %q", firstSession)
	}

	first, err := New(Options{
		Root:             firstRoot,
		SessionPath:      firstSession,
		DefaultStatePath: consoleStatePath(firstSession, firstRoot),
	})
	if err != nil {
		t.Fatalf("new first server: %v", err)
	}
	first.rememberStatePath(filepath.Join(firstRoot, "migration.yaml.state.db"))
	if got := newSessionDefaults(firstSession).get(SessionState); got == "" {
		t.Fatal("first project did not persist its selected state")
	}
	_ = first.listener.Close()

	secondFallback := consoleStatePath(secondSession, secondRoot)
	second, err := New(Options{
		Root:             secondRoot,
		SessionPath:      secondSession,
		DefaultStatePath: secondFallback,
	})
	if err != nil {
		t.Fatalf("new second server: %v", err)
	}
	t.Cleanup(func() { _ = second.listener.Close() })
	if got := second.defaults.get(SessionState); got != "" {
		t.Fatalf("second project inherited state %q", got)
	}

	history := parseLine(t, second, "/history")
	if !history.Dispatched || history.Request.StatePath != secondFallback {
		t.Fatalf("second project history = %+v, want fallback %q", history, secondFallback)
	}
}
