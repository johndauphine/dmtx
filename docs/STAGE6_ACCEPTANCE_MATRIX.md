# Stage 6 acceptance matrix

## Purpose and authority

Stage 6 is release hardening. This matrix maps the normative release contract
in `RECREATE_DMT.md` Sections 18, 20, 21, and 22 to repository evidence and
names every remaining gap. Earlier stage closeouts remain evidence; Stage 6
does not reopen their accepted product decisions or weaken their gates.

Stage 6 is complete only when every row marked Required is Covered and the
closeout records a clean tagged release run plus an upgrade-fixture run.

## Release gates

| Requirement | Status | Current evidence | Remaining work |
| --- | --- | --- | --- |
| macOS x86-64 and ARM64, Linux x86-64 and ARM64, and Windows x86-64 binaries | Implemented, unverified in CI | `.github/workflows/release.yml` builds the five required targets with `CGO_ENABLED=0` and `-trimpath`, verifies them, and attaches the archives and checksum manifest to a tagged GitHub release only after every native/checksum job passes. | Run the workflow from a release candidate tag and retain the release evidence. |
| SHA-256 manifest covering every packaged binary | Implemented, awaiting CI evidence | The release workflow creates `SHA256SUMS`, verifies it before upload, downloads the uploaded artifact into a fresh job, and verifies it again. | Retain the first successful release workflow as closeout evidence. |
| Release version reported by `dmtx --version` | Implemented, awaiting CI evidence | `internal/app.Version` has a development default and is injected from the validated release version with Go linker flags; the Linux artifact and native macOS/Windows builds are executed in CI. | Retain the first successful tagged or dispatched workflow as closeout evidence. |
| Clean offline tests, vet, lint, and race detector | Covered | `.github/workflows/verify.yml`; Stage 5 closeout records the successful authoritative run. | Retain unchanged as a required release check. |
| Dependency vulnerability scanning | Implemented, awaiting CI evidence | `verify.yml` installs the official `govulncheck` v1.1.4 release and scans all reachable packages. `docs/SECURITY.md` defines the exception policy. | Retain a successful scan as closeout evidence. |
| Armed live database gate | Covered for the accepted Stage 4 matrix | `verify.yml` runs the required live suite; the race variant runs on schedule or dispatch. | Decide and document which live subsets gate a release candidate versus the scheduled full matrix. |
| Historical state upgrade support window | Implemented, awaiting full-suite evidence | `TestStage6HistoricalStateUpgradeMatrix` loads immutable v0 SQLite SQL and YAML fixtures, preserves completed history, retains ambiguous incomplete checkpoints without inventing identity, persists the current format, and `TestStage6YAMLStateRejectsFutureVersion` refuses downgrade reads. The application-level ambiguous-resume matrix proves no target mutation. | Retain full-suite and CI evidence. |
| SemVer and deprecation compatibility | Implemented, awaiting full-suite evidence | `public-contract-v5.json` pins stable commands, aliases, exit codes, and deprecations. `TestStage6PublicCompatibilityFixture`, `TestStage6PublicJSONReadersTolerateAdditiveFields`, and `TestStage6DeprecatedFieldsWarnThroughEveryApplicationSurface` enforce the contract; configuration warnings travel through the shared outcome. | Retain full-suite and CI evidence. |
| Cross-platform private-file and atomic-replacement behavior | Implemented, awaiting CI evidence | Platform-specific implementations and tests run in the existing Linux gate and the release workflow's native macOS Intel, macOS ARM64, and Windows jobs. | Retain successful native jobs as closeout evidence. |
| Operator and recovery documentation | Covered | `OPERATOR_GUIDE.md` covers verified installation, configuration, privileges, operation, observability, stop/resume/recovery, upgrade, rollback limits, security incidents, and supported boundaries. | Keep release-specific supported-version details current. |
| Requirements-to-tests traceability | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps every Section 21 group to named tests, detailed earlier-stage matrices, exact armed commands, workflows, and remaining external evidence. | Preserve exact test names and commands through closeout. |

## Section 21 coverage groups

These groups prevent the release checklist from hiding data-plane regressions.
Each must link to named tests or an explicitly armed command before closeout.

| Section | Status | Authoritative evidence or gap |
| --- | --- | --- |
| 21.1 Build and public surface | Implemented, awaiting CI evidence | Stage 5 parity evidence is accepted; the v5 public fixture pins commands, aliases, exit codes, and version behavior; release and native smoke workflows cover all required platforms. |
| 21.2 Configuration and security | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps defaults, template preservation, redaction, native permission tests, and cgroup v1/v2 memory fixtures. |
| 21.3 Deterministic DDL and schema | Covered | The traceability index incorporates the all-five-dialect Stage 3/4 planner and live fixture inventories with boundary tests. |
| 21.4 Driver conformance | Covered | The traceability index incorporates the exact Stage 3 shared/live conformance and version-floor matrix. |
| 21.5 Transfer and retry correctness | Covered | The traceability index maps pagination, acknowledgment, cancellation/leaks, transaction/prefix behavior, replay, and memory-bound evidence. |
| 21.6 State, lease, and resume | Covered | Restartability and fencing matrices are retained; immutable v0 SQLite/YAML fixtures prove completed-history upgrade, fail-closed ambiguous evidence, persistent format upgrade, and future-version refusal. |
| 21.7 Incremental and delete behavior | Covered | Stage 4 closeout plus the release trace preserve the armed commands and named tests. |
| 21.8 Strict consistency | Covered | Stage 4 closeout plus the release trace preserve engine limitations, live proofs, and pre-mutation refusals. |
| 21.9 Schema contract and validation | Covered | Stage 4 closeout plus the release trace preserve schema-evolution, validation, and canonicalization evidence. |
| 21.10 Operational surfaces | Covered | Stage 5 closeout plus the release trace preserve authentication, redaction, observability, cancellation, and parity gates. |
| 21.11 Live database matrix and CI | Implemented, awaiting CI evidence | Verify runs offline/race/static/vulnerability and armed live gates; Release artifacts runs cross-platform builds, native smokes, and pre/post-upload checksum verification; the release trace names the route matrices. |

## Implementation order

1. Prove the release and native smoke workflows in CI.
2. Run the complete local static/offline verification suite.
3. Publish Stage 6 closeout only after Verify and Release artifacts are green
   at the exact release-candidate commit.
