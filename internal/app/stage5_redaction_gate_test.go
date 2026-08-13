package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestNoArgumentsExplainCurrentOperatorSurfaces(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != Success {
		t.Fatalf("no-argument exit code = %d, stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "DMTX has no terminal UI; use dmtx serve for the WebUI or --help for CLI commands.\n"; got != want {
		t.Fatalf("no-argument guidance = %q, want %q", got, want)
	}
}

// TestStage5CrossSurfaceRedactionEvidence is a small drift gate for the
// independent sentinel tests that make up Stage 5's redaction evidence.
//
// A single fake migration cannot honestly exercise the AI transport, encrypted
// portable profiles, audit writer, setup flow, browser-owned local history, and
// API job/SSE envelope at once.  The focused tests below can: each injects a
// value that would be sensitive in that surface and asserts that its public
// representation does not contain it.  Keep this list deliberately explicit.
// Removing or renaming one of those tests therefore requires an equally
// deliberate update here, rather than silently narrowing the closeout gate.
func TestStage5CrossSurfaceRedactionEvidence(t *testing.T) {
	tests := map[string][]string{
		"Request and Outcome public JSON envelopes": {
			"internal/app/wire_shape_test.go:TestRequestWireShape",
			"internal/app/wire_shape_test.go:TestOutcomeEnvelopeWireShape",
			"internal/app/wire_shape_test.go:TestRunPayloadWireShapeExcludesSecrets",
			"internal/app/config_report_test.go:TestConfigPayloadWireShapeExcludesSecrets",
			"internal/app/run_record_redaction_test.go:TestStage4RunRecordRoundTripAndRedaction",
		},
		"CLI progress and state-derived messages": {
			"internal/app/seam_test.go:TestRenderProgressEmitsSanitizedMachineReadableRecord",
			"internal/app/seam_test.go:TestPublicRenderersRedactCredentialDiagnostics",
			"internal/app/public_output_test.go:TestRedactPublicDiagnosticCredentialShapes",
			"internal/app/preflight_test.go:TestPreflightOutputNeverContainsResolvedOrConfiguredPassword",
		},
		"API outcomes, jobs/SSE, and console rendering": {
			"internal/api/server_test.go:TestAPIAndCLIProduceIdenticalOutcomes",
			"internal/api/job_test.go:TestProgressReportsBecomeStreamEvents",
			"internal/api/job_test.go:TestPublicJobSurfacesRedactExecutorDiagnostics",
			"internal/api/ui_test.go:TestConsolePayloadRendererBehavior",
			"internal/api/ui_test.go:TestConsoleHistoryAndMaskedSetupProtectionsAreShipped",
			"internal/api/browser_acceptance_test.go:TestBrowserConsoleControls",
		},
		"Audit records": {
			"internal/app/runtime_tuning_audit_test.go:TestAppendAttemptTerminalAuditWritesRedactedRuntimeTuningBeforeOutcome",
			"internal/app/schema_decision_audit_test.go:TestTableCheckpointObserverPublishesStableCompleteSchemaDecisionAudit",
		},
		"AI prompt, request, and error paths": {
			"internal/app/ai_args_test.go:TestBuildAIAdvisoryPromptOmitsConnectionValues",
			"internal/app/ai_args_test.go:TestExecuteAISecretsLoadFailureFailsWithoutLeakingError",
			"internal/ai/client_test.go:TestGoogleUsesGoogleProtocolAndRedactsCredentialFromURL",
		},
		"Setup and encrypted profile portability": {
			"internal/app/setup_postgres_test.go:TestPostgresSetupUsesProtectedPasswordOrigins",
			"internal/app/setup_postgres_test.go:TestPostgresSetupRedactsConnectionFailures",
			"internal/app/setup_mssql_test.go:TestMSSQLSetupUsesProtectedPasswordOriginsAndVerifiesBothEndpoints",
			"internal/app/setup_mssql_test.go:TestMSSQLSetupVerificationIsBoundedAndRedactsFailures",
			"internal/app/profiles_test.go:TestProfilePortableExportImportWritesOwnerOnlyCiphertext",
			"internal/profiles/portable_test.go:TestPortableRefusesMalformedAndTamperedInputsWithoutLeaks",
		},
	}

	for surface, evidence := range tests {
		t.Run(surface, func(t *testing.T) {
			for _, item := range evidence {
				path, name := splitStage5RedactionEvidence(t, item)
				if !stage5TestExists(t, filepath.Join("..", "..", path), name) {
					t.Errorf("required Stage 5 redaction evidence %s is missing; replace it with an equivalent sentinel test and update this gate", item)
				}
			}
		})
	}
}

func splitStage5RedactionEvidence(t *testing.T, item string) (string, string) {
	t.Helper()
	for index := len(item) - 1; index >= 0; index-- {
		if item[index] == ':' {
			return item[:index], item[index+1:]
		}
	}
	t.Fatalf("invalid Stage 5 redaction evidence %q", item)
	return "", ""
}

func stage5TestExists(t *testing.T, path, want string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == want {
			return true
		}
	}
	return false
}
