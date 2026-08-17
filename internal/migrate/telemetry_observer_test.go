package migrate

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

type panicTelemetryObserver struct{}

func (panicTelemetryObserver) BeforeTable(context.Context, string) error        { return nil }
func (panicTelemetryObserver) AfterTable(context.Context, string, int) error    { return nil }
func (panicTelemetryObserver) ObserveNetworkTelemetry(NetworkTelemetry)         { panic("sink") }
func (panicTelemetryObserver) ObserveTargetWriteTelemetry(TargetWriteTelemetry) { panic("sink") }
func (panicTelemetryObserver) ObserveWriterQueueDepth(int)                      { panic("sink") }
func (panicTelemetryObserver) ObservePayloadBytes(string, int64)                { panic("sink") }
func (panicTelemetryObserver) ObserveMigrationPhase(string)                     { panic("sink") }
func (panicTelemetryObserver) ObserveMigrationFallback(string)                  { panic("sink") }
func (panicTelemetryObserver) ObserveMigrationRetry(string)                     { panic("sink") }

func TestBestEffortTelemetryCannotAffectMigrationCallbacks(t *testing.T) {
	observer := panicTelemetryObserver{}
	networkTelemetryCallback(observer)(NetworkTelemetry{})
	emitNetworkTelemetry(func(NetworkTelemetry) { panic("sink") }, NetworkTelemetry{})
	observeTargetWriteTelemetry(observer, TargetWriteTelemetry{Duration: time.Millisecond})
	observeWriterQueueDepth(observer, 1)
	observePayloadBytes(observer, "table", 1)
	observeMigrationPhase(observer, "transfer")
	observeMigrationRetry(observer, "sqlite_write")
}

func TestMySQLFallbackDrainsExactlyOnce(t *testing.T) {
	writer := &mysqlNativeWriter{warn: func(string) {}}
	writer.useMySQLStrictInsertFallback(mysqlLocalInfileFallbackWarning)
	writer.useMySQLStrictInsertFallback(mysqlLocalInfileFallbackWarning)
	if got := writer.DrainFallbackEvents(); got != 1 {
		t.Fatalf("fallbacks=%d, want 1", got)
	}
	if got := writer.DrainFallbackEvents(); got != 0 {
		t.Fatalf("drained fallbacks=%d", got)
	}
}

type phaseTelemetryObserver struct{ phases map[string]int }

func (o *phaseTelemetryObserver) BeforeTable(context.Context, string) error     { return nil }
func (o *phaseTelemetryObserver) AfterTable(context.Context, string, int) error { return nil }
func (o *phaseTelemetryObserver) ObserveMigrationPhase(phase string)            { o.phases[phase]++ }

func TestSQLiteFreshAndResumeEmitStableObservedPhases(t *testing.T) {
	for _, resume := range []bool{false, true} {
		sourcePath := t.TempDir() + "/source.db"
		targetPath := t.TempDir() + "/target.db"
		db, err := sql.Open("sqlite", sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items VALUES (1, 'one')"); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		observer := &phaseTelemetryObserver{phases: map[string]int{}}
		cfg := sqlitePipelineTestConfig(sourcePath, targetPath)
		if resume {
			_, err = SQLiteToSQLiteResumeWithProgress(context.Background(), cfg, nil, nil, observer)
		} else {
			_, err = SQLiteToSQLiteWithObserver(context.Background(), cfg, observer)
		}
		if err != nil {
			t.Fatalf("resume=%t: %v", resume, err)
		}
		for _, phase := range []string{"preflight", "schema_extraction", "target_preparation", "transfer", "finalization", "validation"} {
			if observer.phases[phase] == 0 {
				t.Fatalf("resume=%t missing phase %s: %#v", resume, phase, observer.phases)
			}
		}
	}
}

type captureNetworkTelemetryObserver struct{ calls int }

func (*captureNetworkTelemetryObserver) BeforeTable(context.Context, string) error     { return nil }
func (*captureNetworkTelemetryObserver) AfterTable(context.Context, string, int) error { return nil }
func (observer *captureNetworkTelemetryObserver) ObserveNetworkTelemetry(NetworkTelemetry) {
	observer.calls++
}

func TestStage4TelemetryCallbackFactoriesPropagate(t *testing.T) {
	for _, test := range []struct {
		name    string
		factory func(*captureNetworkTelemetryObserver) func(NetworkTelemetry)
	}{
		{
			name: "fresh network strategy",
			factory: func(observer *captureNetworkTelemetryObserver) func(NetworkTelemetry) {
				return (&stage4AdapterNetworkExecution{}).callbacks(observer).Telemetry
			},
		},
		{
			name: "resume table strategy",
			factory: func(observer *captureNetworkTelemetryObserver) func(NetworkTelemetry) {
				execution := &stage4AdapterNetworkTableExecution{parent: &stage4AdapterNetworkExecution{}, coordinator: &networkStateCoordinator{}}
				return execution.callbacks(observer).Telemetry
			},
		},
		// Strict and rebuild routes both use the migration-wide execution
		// callback factory; retain named cases so a future route-specific
		// factory cannot silently omit the telemetry field.
		{
			name: "strict network strategy",
			factory: func(observer *captureNetworkTelemetryObserver) func(NetworkTelemetry) {
				return (&stage4AdapterNetworkExecution{}).callbacks(observer).Telemetry
			},
		},
		{
			name: "rebuild network strategy",
			factory: func(observer *captureNetworkTelemetryObserver) func(NetworkTelemetry) {
				return (&stage4AdapterNetworkExecution{}).callbacks(observer).Telemetry
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			observer := &captureNetworkTelemetryObserver{}
			callback := test.factory(observer)
			if callback == nil {
				t.Fatal("telemetry callback was dropped")
			}
			callback(NetworkTelemetry{})
			if observer.calls != 1 {
				t.Fatalf("calls=%d, want 1", observer.calls)
			}
		})
	}

	t.Run("dependency wave preserves telemetry", func(t *testing.T) {
		calls := 0
		base := NetworkTransferCallbacks{Telemetry: func(NetworkTelemetry) { calls++ }}
		wave := stage4AdapterNetworkWave{plan: NetworkTransferPlan{Ranges: []NetworkRangePlan{{}}}, global: []NetworkRangePlan{{}}}
		callbacks, err := wave.callbacks(base)
		if err != nil {
			t.Fatal(err)
		}
		if callbacks.Telemetry == nil {
			t.Fatal("wave dropped telemetry callback")
		}
		callbacks.Telemetry(NetworkTelemetry{})
		if calls != 1 {
			t.Fatalf("calls=%d, want 1", calls)
		}
	})
}
