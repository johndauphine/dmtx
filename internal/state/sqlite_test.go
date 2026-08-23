package state

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStorePersistsRunTransitions(t *testing.T) {
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "private", "runs.db")}
	started := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	completed := started.Add(time.Minute)

	if err := store.Append(Run{ID: "run-1", Source: "source.db", Target: "target.db", Outcome: Running, Resumable: true, Reason: "migration in progress", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Run{ID: "run-1", Source: "source.db", Target: "target.db", Outcome: Success, Resumable: false, Reason: "migration completed", StartedAt: started, EndedAt: completed}); err != nil {
		t.Fatal(err)
	}

	latest, found, err := store.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if !found || latest.Outcome != Success || !latest.EndedAt.Equal(completed) {
		t.Fatalf("latest = %#v, found = %v", latest, found)
	}

	runs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Outcome != Running || runs[1].Outcome != Success {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestPersistentSQLiteBackendReusesOneCommandScopedPool(t *testing.T) {
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "state.db")}
	backend, closeBackend := WithPersistentSQLiteBackend(store)
	persistent, ok := backend.(SQLiteStore)
	if !ok {
		t.Fatalf("persistent backend type = %T, want SQLiteStore", backend)
	}

	first, closeFirst, err := persistent.openForOperation()
	if err != nil {
		t.Fatal(err)
	}
	second, closeSecond, err := persistent.openForOperation()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("persistent backend opened more than one command-scoped pool")
	}
	if err := closeFirst(); err != nil {
		t.Fatal(err)
	}
	if err := closeSecond(); err != nil {
		t.Fatal(err)
	}
	if err := closeBackend(); err != nil {
		t.Fatal(err)
	}
	if err := first.Ping(); err == nil {
		t.Fatal("command-scoped SQLite pool remained open after release")
	}
}

func TestSQLiteStoreRejectsUnknownCoreSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store := SQLiteStore{Path: path}
	database, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if version != sqliteCoreSchemaVersion {
		database.Close()
		t.Fatalf("core schema version = %d, want %d", version, sqliteCoreSchemaVersion)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 99`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if database, err := store.Open(); err == nil {
		database.Close()
		t.Fatal("future core schema version was accepted")
	}
}
