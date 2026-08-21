package state

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStage6HistoricalStateUpgradeMatrix(t *testing.T) {
	for _, fixture := range []struct {
		name string
		ext  string
	}{
		{name: "sqlite-v0", ext: ".db"},
		{name: "yaml-v0", ext: ".yaml"},
	} {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			path := materializeStage6StateFixture(t, fixture.ext)
			backend, err := NewBackend(path)
			if err != nil {
				t.Fatal(err)
			}

			runs, err := backend.List()
			if err != nil {
				t.Fatalf("read historical runs: %v", err)
			}
			if len(runs) != 2 || runs[0].ID != "completed-v0" ||
				runs[0].Outcome != Success || runs[0].Resumable ||
				runs[1].ID != "ambiguous-v0" || !runs[1].Resumable {
				t.Fatalf("upgraded runs = %#v", runs)
			}

			completed, err := backend.ListTasks("completed-v0")
			if err != nil {
				t.Fatalf("read completed history: %v", err)
			}
			if len(completed) != 1 || completed[0].Status != "completed" ||
				completed[0].RowsDone != 2 {
				t.Fatalf("completed tasks = %#v", completed)
			}

			ambiguous, err := backend.ListTasks("ambiguous-v0")
			if err != nil {
				t.Fatalf("read ambiguous history: %v", err)
			}
			if len(ambiguous) != 1 || ambiguous[0].Status != "running" ||
				ambiguous[0].RowsDone != 1 ||
				ambiguous[0].IntegerWatermark != nil ||
				ambiguous[0].RowNumberWatermark != nil {
				t.Fatalf("ambiguous tasks = %#v", ambiguous)
			}
			if _, err := runs[1].BoundLease(); !errors.Is(err, ErrRunLeaseEvidenceUnavailable) {
				t.Fatalf("legacy lease evidence error = %v", err)
			}

			// A normal post-upgrade mutation must retain history and persist the
			// current format rather than only decoding the old format in memory.
			if err := backend.SaveResumeCompatibilityHash("completed-v0", "stage6-compatible"); err != nil {
				t.Fatalf("write upgraded state: %v", err)
			}
			hash, found, err := backend.ResumeCompatibilityHash("completed-v0")
			if err != nil || !found || hash != "stage6-compatible" {
				t.Fatalf("upgraded compatibility hash = %q, found=%t, err=%v", hash, found, err)
			}
			if fixture.ext == ".yaml" {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(string(data), "version: 5\n") {
					t.Fatalf("upgraded YAML did not persist current version:\n%s", data)
				}
			}
		})
	}
}

func materializeStage6StateFixture(t *testing.T, extension string) string {
	t.Helper()
	fixture := filepath.Join("testdata", "stage6", "state-v0"+map[string]string{
		".db":   ".sql",
		".yaml": ".yaml",
	}[extension])
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state"+extension)
	if extension == ".yaml" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(string(data)); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStage6YAMLStateRejectsFutureVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.yaml")
	if err := os.WriteFile(path, []byte("version: 6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := (YAMLStore{Path: path}).Latest()
	if err == nil || !strings.Contains(err.Error(), "unsupported version 6") {
		t.Fatalf("future state error = %v", err)
	}
}
