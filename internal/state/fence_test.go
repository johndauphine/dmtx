package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseGuardProtectConcurrentOverlapsStateWritesAndBlocksTakeover(
	t *testing.T,
) {
	t.Parallel()
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "leases.db")}
	lease, err := store.AcquireLease("postgres:target", "run-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE leases SET heartbeat_at = ? WHERE target = ?`,
		time.Unix(0, 0).UTC(),
		lease.Target,
	); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	guard := NewLeaseGuard(store, lease)
	entered := make(chan int, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			done <- guard.ProtectConcurrent(context.Background(), func() error {
				entered <- index
				<-release
				return nil
			})
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("target writes did not overlap beneath one lease fence")
		}
	}
	stateMutation := make(chan error, 1)
	go func() {
		stateMutation <- guard.Protect(context.Background(), func() error {
			return nil
		})
	}()
	select {
	case mutationErr := <-stateMutation:
		if mutationErr != nil {
			t.Fatalf("state mutation beside active target cohort: %v", mutationErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("state mutation waited for an independent target write")
	}

	takeover := make(chan error, 1)
	go func() {
		_, acquireErr := store.AcquireLease(lease.Target, "run-2", time.Second)
		takeover <- acquireErr
	}()
	select {
	case acquireErr := <-takeover:
		if acquireErr == nil {
			t.Fatal("takeover interleaved with concurrent protected target writes")
		}
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for index := 0; index < 2; index++ {
		if err := <-done; err != nil {
			t.Fatalf("protected target write %d: %v", index, err)
		}
	}
	select {
	case acquireErr := <-takeover:
		if acquireErr != nil {
			t.Fatalf("takeover after protected target cohort: %v", acquireErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("takeover remained blocked after protected target cohort")
	}
}

func TestLeaseGuardProtectConcurrentDoesNotBarrierCompletedWriters(
	t *testing.T,
) {
	t.Parallel()
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "leases.db")}
	lease, err := store.AcquireLease("postgres:target", "run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	guard := NewLeaseGuard(store, lease)

	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		firstDone <- guard.ProtectConcurrent(context.Background(), func() error {
			close(firstEntered)
			<-firstRelease
			return nil
		})
	}()
	<-firstEntered
	go func() {
		secondDone <- guard.ProtectConcurrent(context.Background(), func() error {
			close(secondEntered)
			<-secondRelease
			return nil
		})
	}()
	<-secondEntered

	close(firstRelease)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first protected write: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("completed writer waited for an overlapping writer to finish")
	}
	select {
	case err := <-secondDone:
		t.Fatalf("second protected write returned before release: %v", err)
	default:
	}
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatalf("second protected write: %v", err)
	}
}

func TestLeaseGuardReleaseReportsConcurrentProtectionFailure(t *testing.T) {
	t.Parallel()
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "leases.db")}
	lease, err := store.AcquireLease("postgres:target", "run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	guard := NewLeaseGuard(store, lease)
	protectionErr := errors.New("release concurrent lease fence")
	protection := &leaseTargetProtection{
		done: make(chan struct{}),
		err:  protectionErr,
	}
	close(protection.done)
	guard.targetProtection = protection

	err = guard.Release()
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("release error = %v, want lease loss", err)
	}
	if !errors.Is(err, protectionErr) {
		t.Fatalf("release error = %v, want concurrent protection failure", err)
	}
}

func TestLeaseGuardReleaseRejectsNewConcurrentProtection(t *testing.T) {
	t.Parallel()
	store := SQLiteStore{Path: filepath.Join(t.TempDir(), "leases.db")}
	lease, err := store.AcquireLease("postgres:target", "run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	guard := NewLeaseGuard(store, lease)
	protection := &leaseTargetProtection{done: make(chan struct{})}
	guard.targetProtection = protection
	released := make(chan error, 1)
	go func() { released <- guard.Release() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		guard.targetMu.Lock()
		closing := guard.targetClosing
		guard.targetMu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release did not close the concurrent protection gate")
		}
		time.Sleep(time.Millisecond)
	}
	operationCalled := false
	err = guard.ProtectConcurrent(context.Background(), func() error {
		operationCalled = true
		return nil
	})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("concurrent protection during release error = %v, want lease loss", err)
	}
	if operationCalled {
		t.Fatal("operation ran after lease release began")
	}
	close(protection.done)
	if err := <-released; err != nil {
		t.Fatalf("release lease: %v", err)
	}
}

func TestFenceBackendAdvertisesOptionalStage4CapabilitiesOnlyWhenUnderlyingSupportsThem(
	t *testing.T,
) {
	raw := YAMLStore{Path: filepath.Join(t.TempDir(), "state.yaml")}
	masked := stage4AggregateWithoutRebuildRecovery{
		Backend:                raw,
		RangeBackend:           raw,
		Stage4Backend:          raw,
		Stage4AggregateBackend: raw,
	}
	fencedMasked := FenceBackend(masked, nil)
	if _, ok := fencedMasked.(Stage4AggregateBackend); !ok {
		t.Fatal("fenced aggregate backend lost aggregate capability")
	}
	if _, ok := fencedMasked.(Stage4RebuildRecoveryBackend); ok {
		t.Fatal("fenced aggregate backend invented rebuild recovery capability")
	}
	if _, ok := fencedMasked.(Stage4DeleteJournalReadinessBackend); ok {
		t.Fatal("fenced aggregate backend invented delete-journal readiness capability")
	}

	fencedRaw := FenceBackend(raw, nil)
	if _, ok := fencedRaw.(Stage4RebuildRecoveryBackend); !ok {
		t.Fatal("fenced YAML backend lost rebuild recovery capability")
	}
	if _, ok := fencedRaw.(Stage4DeleteJournalReadinessBackend); !ok {
		t.Fatal("fenced YAML backend lost delete-journal readiness capability")
	}
}

type stage4AggregateWithoutRebuildRecovery struct {
	Backend
	RangeBackend
	Stage4Backend
	Stage4AggregateBackend
}

func TestFencedBackendsRejectEveryOldGenerationMutationAfterTakeover(t *testing.T) {
	for _, stateKind := range []string{"sqlite", "yaml"} {
		t.Run(stateKind, func(t *testing.T) {
			directory := t.TempDir()
			leaseStore := SQLiteStore{Path: filepath.Join(directory, "lease.db")}
			firstLease, err := leaseStore.AcquireLease("sqlite:target", "run-1", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var raw Backend
			if stateKind == "sqlite" {
				raw = SQLiteStore{Path: filepath.Join(directory, "state.db")}
			} else {
				raw = YAMLStore{Path: filepath.Join(directory, "state.yaml")}
			}
			old := FenceBackend(raw, NewLeaseGuard(leaseStore, firstLease))
			started := time.Now().UTC()
			if err := old.InitializeRun(Run{
				ID: "run-1", Source: "source", Target: "target", Outcome: Running,
				Resumable: true, Reason: "running", StartedAt: started,
			}, "hash"); err != nil {
				t.Fatal(err)
			}
			if err := old.CreateTask(Task{RunID: "run-1", Table: "items", StartedAt: started}); err != nil {
				t.Fatal(err)
			}
			rangeBackend := old.(RangeBackend)
			key := TaskKey{Type: "table-copy", Table: "items"}
			workTask := WorkTask{RunID: "run-1", Key: key, Strategy: "integer_keyset", TopologyHash: "a", StartedAt: started}
			workRanges := []RangeState{{ID: "0", Upper: TypedTuple{Int64Value(10)}, UpperInclusive: true}}
			if _, err := rangeBackend.EnsureWorkPlan(workTask, workRanges); err != nil {
				t.Fatal(err)
			}
			if err := rangeBackend.BeginRangeChunk(RangeChunkIntent{
				RunID: "run-1", Task: key, RangeID: "0", TopologyHash: "a",
				Sequence: 0, ChunkRows: 1, EndFrontier: TypedTuple{Int64Value(1)},
				FrontierValid: true, At: started,
			}); err != nil {
				t.Fatal(err)
			}

			database, err := leaseStore.Open()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE leases SET heartbeat_at = ? WHERE target = ?`, time.Unix(0, 0).UTC(), firstLease.Target); err != nil {
				database.Close()
				t.Fatal(err)
			}
			database.Close()
			secondLease, err := leaseStore.AcquireLease("sqlite:target", "run-2", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if secondLease.Generation <= firstLease.Generation {
				t.Fatalf("generations = %d, %d", firstLease.Generation, secondLease.Generation)
			}

			mutations := []func() error{
				func() error {
					return old.Append(Run{ID: "run-1", Source: "source", Target: "target", Outcome: Failed, Resumable: true, Reason: "old", StartedAt: started})
				},
				func() error { return old.ReactivateRun("run-1", "old") },
				func() error { return old.UpdateFailure("run-1", "old", started) },
				func() error { return old.UpdateRecoverableOutcome("run-1", Cancelled, "old", started) },
				func() error { return old.AbandonRun("run-1", "old owner", started) },
				func() error { return old.CreateTask(Task{RunID: "run-1", Table: "other", StartedAt: started}) },
				func() error { return old.CreateTasks([]Task{{RunID: "run-1", Table: "more", StartedAt: started}}) },
				func() error { return old.AdvanceIntegerKeysetTask("run-1", "items", 1, 1) },
				func() error { return old.AdvanceRowNumberTask("run-1", "items", 1, 1) },
				func() error { return old.CompleteTask("run-1", "items", 1, started) },
				func() error { return old.SaveConfigHash("other", "hash") },
				func() error {
					_, err := rangeBackend.AcknowledgeRange(RangeAcknowledgement{
						RunID: "run-1", Task: key, RangeID: "0", TopologyHash: "a",
						ChunkRows: 1, DurableRows: 1,
					})
					return err
				},
				func() error {
					return rangeBackend.RecordRangeAttempt(RangeAttempt{
						RunID: "run-1", Task: key, RangeID: "0", TopologyHash: "a",
						Sequence: 0, At: started,
					})
				},
				func() error { return rangeBackend.ResetWorkPlan(workTask, workRanges) },
				func() error { return rangeBackend.CompleteRange("run-1", key, "0", "a", 0, started) },
				func() error { return rangeBackend.CompleteWorkTask("run-1", key, "a", started) },
			}
			for index, mutate := range mutations {
				if err := mutate(); !errors.Is(err, ErrLeaseLost) {
					t.Fatalf("mutation %d error = %v, want lease loss", index, err)
				}
			}
			runs, err := raw.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != 1 || runs[0].Outcome != Running {
				t.Fatalf("old generation changed runs: %#v", runs)
			}
			tasks, err := raw.ListTasks("run-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 || tasks[0].RowsDone != 0 || tasks[0].Status != "running" {
				t.Fatalf("old generation changed tasks: %#v", tasks)
			}
			_, ranges, err := raw.(RangeBackend).ListWork("run-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(ranges) != 1 || ranges[0].RowsDone != 0 || ranges[0].Attempts != 0 ||
				len(ranges[0].Pending) != 1 || ranges[0].Pending[0].Attempts != 0 {
				t.Fatalf("old generation changed work ranges: %#v", ranges)
			}
		})
	}
}
