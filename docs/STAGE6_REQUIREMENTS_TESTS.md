# Stage 6 requirements-to-tests traceability

This document is the release-wide index for the observable acceptance criteria
in `RECREATE_DMT.md` Section 21. Detailed route-level evidence remains in
`STAGE3_REQUIREMENTS_TESTS.md` and `STAGE4_REQUIREMENTS_TESTS.md`; Stage 5
surface evidence remains in `STAGE5_ACCEPTANCE_CHECKLIST.md` and
`STAGE5_CLOSEOUT_HANDOFF.md`. Those documents are incorporated here by exact
section and named gate rather than replaced with a broad package-pass claim.

## Required commands

Offline release gate:

```sh
go test ./... -count=1 -timeout 20m
go test -race ./... -count=1 -timeout 30m
go vet ./...
golangci-lint run
govulncheck ./...
```

Armed data-plane gate, using the five TLS fixtures in `test/fixtures`:

```sh
DMTX_STAGE4_LIVE_REQUIRED=1 go test ./... -count=1 -timeout 30m
DMTX_STAGE4_LIVE_REQUIRED=1 go test -race ./... -count=1 -timeout 30m
```

The release workflow must additionally pass its artifact/checksum job and all
three native smoke jobs. A live test that skips in an armed run is a failure of
evidence even if `go test` exits successfully.

## 21.1 Build and public surface

| Contract | Evidence |
| --- | --- |
| One self-contained executable on every release platform | `.github/workflows/release.yml` builds macOS Intel/ARM64, Linux x86-64/ARM64, and Windows x86-64 with `CGO_ENABLED=0`; native jobs execute the macOS and Windows builds. |
| Version, commands, aliases, global behavior, and exit codes remain stable | `TestVersion`, `TestStage6PublicCompatibilityFixture`, `TestDMTArgumentSyntaxBuildsCanonicalRequests`, `TestResolveFindsCommandsByNameAndAlias`, `TestNoSurfaceCallsARegisteredCommandUnknown`, and the immutable `internal/app/testdata/stage6/public-contract-v5.json`. |
| CLI/WebUI parity | `TestNoSurfaceCallsARegisteredCommandUnknown`, `TestAnAliasAnswersLikeTheCommandItNames`, `TestStage5BrowserE2E`, and the Stage 5 command matrix. |
| AI is optional | `TestExecuteAIMissingSecretsIsAdvisoryUnavailable`, the ordinary run/validate suites without AI configuration, and Stage 5 browser coverage. |

## 21.2 Configuration and security

| Contract | Evidence |
| --- | --- |
| Defaults, aliases, invalid combinations, same-endpoint guard | `TestParseAppliesCompatibilityDefaults`, `TestParseAppliesProductionSemanticsDefaults`, `TestParseCanonicalizesDeprecatedProductionSettings`, `TestParseRejectsInvalidProductionSemantics`, `TestPreflightRejectsSameDatabase`, and the table-driven config suites. |
| Template expansion preserves YAML structure | `TestParsePreservesSecretTemplates` and config/secrets parser suites exercising punctuation and multiline substitutions. |
| Raw/edit loading preserves templates | `TestParsePreservesSecretTemplates` and initialization/setup round-trip tests. |
| Secret exclusion on every surface | `TestStage4RunRecordRoundTripAndRedaction`, `TestPreflightOutputNeverContainsResolvedOrConfiguredPassword`, `TestSinkRedactsAndRemovesRunGauges`, `TestOTLPHTTPExportUsesTraceServiceJSONAndRedacts`, Stage 5 security hardening tests, and AI prompt/redaction tests. |
| Restrictive private permissions | Unix/Windows tests in `internal/secrets`, plus the native release smoke jobs. |
| Container memory cannot inherit unsafe host memory | `TestResolveEffectiveTransferPlanUsesFiniteCgroupV2BudgetAndCapsConcurrency`, `TestResolveEffectiveTransferPlanUsesFiniteCgroupV1RemainingMemory`, and unlimited-cgroup fallback tests. |

## 21.3 Deterministic DDL and schema

| Contract | Evidence |
| --- | --- |
| Planner is in-process and deterministic | `TestDDLStatementIsRendererOwnedAndDialectBound`, `TestTargetSchemaEvolutionPlanIsDeterministicImmutableAndComplete`, and planner boundary tests. |
| Exact rich DDL across five dialects | SQLite schema tests; `TestPlanPostgresDropRecreateObjectsExactDDLAndGlobalOrder`; `TestPlanMySQLDropRecreateObjectsExactDDLAndOrder`; SQL Server planner tests; ClickHouse fixtures indexed in the Stage 3 matrix. |
| Ordered batches, affinity, cleanup, and primary-error preservation | Schema batch-execution tests and target schema evolution transaction/session tests indexed in Stage 4 Block B. |
| No ad hoc migrated-schema DDL | `TestDDLStatementIsRendererOwnedAndDialectBound`, dialect-bound planned-object tests, and Stage 4 planner boundary checks. |
| Difficult metadata regressions | MySQL enum/set/full-type tests, spatial SRID route tests, PostgreSQL identifier allocation tests, and quoted-name DDL fixtures indexed in Stage 4 Block B. |
| Unsupported features are typed failures | `TestSQLiteSourceAdapterAppliesSchemaSafetyChecks`, catalog type validation tests, and capability pre-mutation refusal matrices. |

## 21.4 Driver conformance

The exact relational, MariaDB, and ClickHouse inventory is the **Exit-criterion
map** in `STAGE3_REQUIREMENTS_TESTS.md`. It names discovery, secure DSN,
qualification, placeholders, row counts, date columns, pagination, table/PK
existence, native bulk, capability, preflight, same-endpoint, and live
version-floor fixtures. Credential-error redaction is additionally covered by
endpoint validation, setup, preflight, and run-record redaction tests.

The authoritative command is the Stage 3 aggregate gate in that document,
embedded in the broader armed Stage 4 gate above.

## 21.5 Transfer and retry correctness

| Contract | Evidence |
| --- | --- |
| Integer, tuple, and ROW_NUMBER pagination boundaries | `TestRangeBackendConformance`, tuple/keyset planner and crash-resume tests, values-above-2^53 tests, and Stage 4 live collation matrices. |
| Contiguous acknowledgement frontier | range attempt/ack conformance, `TestNetworkStateCoordinatorRichReplayEvidenceConformance`, and out-of-order completion fault tests in Stage 2/4. |
| Failure cancellation and no leaks | `TestSQLitePipelineRepeatedCancellationDoesNotLeakResources`, `TestByteBudgetCancellationReleasesAndUnblocks`, `TestResumableNetworkTransferCancellationDrainsAndReleases`, and strict-owner/reader release tests. |
| Transaction and committed-prefix semantics | PostgreSQL/SQL Server native writer rollback tests, MySQL prefix tests, and `TestMariaDBNativeWriterLocalInfileWarningLeavesTargetUntouchedLive`. |
| Replay safety | Stage 1 hard-kill matrices, `TestStage4UpsertProcessKillReplayLive`, and the rebuild/delete receipt replay matrices. |
| Effective memory bound | `TestSQLitePipelineHonorsByteBudgetForWideRows`, retained-row byte-ceiling tests, and resource-plan cgroup tests. |

## 21.6 State, lease, and resume

| Contract | Evidence |
| --- | --- |
| SQLite and YAML restartability conformance | `TestBackendConformance`, `TestRangeBackendConformance`, `TestRangeAttemptBackendConformance`, and `TestStage4BackendConformance`. |
| Atomic YAML replacement and concurrent writers | YAML atomic/crash process tests and Stage 4 YAML crash tests. |
| Required writes and task creation fail closed | required-write exit tests, atomic initialization/task tests, and Stage 4 state-failure suites. |
| Ownership, takeover, and stale-generation fencing | `TestTargetLeaseTwoProcessRace`, `TestSQLiteStoreMatchingLeaseSerializesAliasRace`, `TestStage4MutationsRejectStaleLeaseGeneration`, and takeover process tests. |
| Drift, completed-table verification, topology reset, and outcome matrix | resume config/hash tests, completion verification tests, range topology tests, `TestOutcomeAndAbandonmentConformance`, and `TestPersistAttemptDispositionConformance`. |
| Historical upgrades and newer-format refusal | `TestStage6HistoricalStateUpgradeMatrix`, `TestStage6YAMLStateRejectsFutureVersion`, `TestResumeRejectsAmbiguousLegacyProgressForEveryStateBackend`, and immutable v0 fixtures. |

## 21.7 Incremental and delete behavior

Stage 4 Blocks C and D in `STAGE4_REQUIREMENTS_TESTS.md` are the detailed map.
The aggregate proofs are `TestStage4IncrementalCertifiedRouteMatrixLiveTLS`,
`TestStage4IncrementalNetworkProcessKillResumeLive`,
`TestStage4CertifiedRelationalDeleteRouteMatrixLive`, the engine receipt replay
tests, and dry-run no-write tests. They cover baseline watermarks, strict lower
bounds, zero-row runs, fallback full upsert, immutable fences, resume replay,
due/not-due reconciliation, counts, and dry-run disclosure.

## 21.8 Strict consistency

Stage 4 Block E is the detailed map. The release retains
`TestStage4StrictProcessKillResumeMatrixLive`, PostgreSQL stable-snapshot,
MySQL/MariaDB table snapshot, SQL Server snapshot, SQLite pinned-reader, target
validation-count, missing-snapshot refusal, and unsupported-scope pre-mutation
tests. These are exercised by the armed gate and its scheduled/dispatch race
variant.

## 21.9 Schema contract and validation

Stage 4 Blocks B and F are the detailed map. The release retains
`TestStage4SchemaContractTargetMatrixLive`, schema decision audit tests,
discard dependency/key refusal tests, `TestValidationCoreNullParityDetectsSystematicConversion`,
sample canonicalization tests, validation route matrices, timeout/count policy
tests, and explicit `full`-mode refusal.

## 21.10 Operational surfaces

| Contract | Evidence |
| --- | --- |
| Stable actionable preflight and visible skips | preflight core/report suites and `TestPostgresPreflightPrivilegesLiveTLS`. |
| Signal cancellation and truthful resumability | signal/final-checkpoint tests and the process-kill/resume matrices. |
| Correlated redacted logs, metrics, traces, audit, notifications | observability suites, runtime/schema audit tests, redaction tests, and Stage 5 closeout. |
| Audit-chain tamper detection | `TestAppendCreatesHashLinkedAuditEvents` and `TestAppendRejectsTamperedAuditStream`. |
| WebUI security and behavior | `TestStage5BrowserE2E`, API authentication/session/Host/Origin/rate-limit tests, completion containment, SSE recovery, and `internal/api/security_hardening_test.go`. |

## 21.11 Live database matrix and CI

`.github/workflows/verify.yml` is the pull-request/main/scheduled gate: tests,
vet, lint, reachable vulnerability scan, offline race, Linux/Windows builds,
five-engine fixture provisioning, armed live tests, and scheduled/dispatch live
race. `STAGE3_REQUIREMENTS_TESTS.md` names the directed 12-pair and ClickHouse
fixtures. `STAGE4_REQUIREMENTS_TESTS.md` names schema evolution, incremental,
delete, resume-fault, strict snapshot, and state-backend scenarios.

`.github/workflows/release.yml` adds the five release artifacts, embedded
version smoke tests, SHA-256 manifest generation and pre/post-upload
verification, native macOS Intel/ARM64 and Windows security/application/API
tests, and tagged GitHub release publication after every prerequisite passes.

## Superseded external evidence

The following external evidence is retained as an audit trail for the
incorrectly versioned `v5.6.0-rc.4` publication at commit
`817b4d3720c138859a841adfd56fc2ac9570a8a1`. It does not close the DMTX 1.0
release gate:

1. [Verify run `32551315762`](https://github.com/johndauphine/dmtx/actions/runs/32551315762)
   passed the offline/static/vulnerability/race gates and the armed five-engine
   live suite both normally and under the race detector;
2. [Release artifacts run `32551007314`](https://github.com/johndauphine/dmtx/actions/runs/32551007314)
   passed all five builds, macOS Intel/ARM64 and Windows native tests, embedded
   version executions, and pre/post-upload checksum verification;
3. the [published `v5.6.0-rc.4` release](https://github.com/johndauphine/dmtx/releases/tag/v5.6.0-rc.4)
   contains the five required archives and `SHA256SUMS`;
4. an independent fresh download reported `OK` for every archive in
   `sha256sum --check SHA256SUMS`; and
5. the workflow executed the candidate version on Linux, macOS, and Windows,
   while an independent execution of the published Linux x86-64 binary also
   reported `5.6.0-rc.4`.

## DMTX 1.0 release evidence

The `v1.0.0-rc.1` tag dereferences to main merge commit
`02e9a207fdfdbe190e9c7475771ffe7a5e5b467e` (PR #50).

1. [Exact-tag Verify run `32584626335`](https://github.com/johndauphine/dmtx/actions/runs/32584626335)
   passed offline tests, vet, golangci-lint, pinned `govulncheck` with zero
   reachable findings, offline race, Linux/Windows builds, the armed
   five-fixture live suite, and armed live race;
2. [Tagged Release artifacts run `32584307354`](https://github.com/johndauphine/dmtx/actions/runs/32584307354)
   passed all five platform artifacts, SHA256 generation and pre-upload check,
   Linux version smoke, macOS Intel/ARM64 and Windows native tests and version
   smokes, uploaded-manifest re-download verification, and publication;
3. the [published `v1.0.0-rc.1` prerelease](https://github.com/johndauphine/dmtx/releases/tag/v1.0.0-rc.1)
   is not a draft, is marked prerelease, and contains exactly five platform
   archives and `SHA256SUMS`; and
4. an independent fresh-download audit passed `shasum -a 256 -c SHA256SUMS`
   for all five archives, while the downloaded Darwin ARM64 artifact reported
   `1.0.0-rc.1`.

At audit time, the remote tag and `origin/main` both resolved to the same
`02e9a207fdfdbe190e9c7475771ffe7a5e5b467e` commit.
