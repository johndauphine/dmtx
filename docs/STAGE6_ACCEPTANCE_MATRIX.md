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
| macOS x86-64 and ARM64, Linux x86-64 and ARM64, and Windows x86-64 binaries | Covered | The [tagged Release artifacts run](https://github.com/johndauphine/dmtx/actions/runs/32551007314) at `817b4d3720c138859a841adfd56fc2ac9570a8a1` built and verified all five targets. The [published `v5.6.0-rc.4` release](https://github.com/johndauphine/dmtx/releases/tag/v5.6.0-rc.4) contains exactly the five platform archives plus `SHA256SUMS`. | None. |
| SHA-256 manifest covering every packaged binary | Covered | Tagged run `32551007314` verified `SHA256SUMS` before upload and again after a fresh artifact download. A separate post-publication download also reported `OK` for all five published archives. | None. |
| Release version reported by `dmtx --version` | Covered | Tagged run `32551007314` executed the injected version on Linux, macOS Intel, macOS ARM64, and Windows; an independent execution of the published Linux x86-64 archive reported `5.6.0-rc.4`. | None. |
| Clean offline tests, vet, lint, and race detector | Covered | The [exact-candidate Verify dispatch](https://github.com/johndauphine/dmtx/actions/runs/32551315762) covers offline tests, vet, golangci-lint, the offline race detector, and Linux/Windows builds at `817b4d3720c138859a841adfd56fc2ac9570a8a1`. | None. |
| Dependency vulnerability scanning | Covered | Exact-candidate Verify run `32551315762` ran pinned `govulncheck` v1.1.4 with zero reachable findings. `docs/SECURITY.md` retains the exception policy. | None. |
| Armed live database gate | Covered | Exact-candidate Verify run `32551315762` ran the armed five-fixture live suite both normally and with the race detector. | None. |
| Historical state upgrade support window | Covered | `TestStage6HistoricalStateUpgradeMatrix`, `TestStage6YAMLStateRejectsFutureVersion`, and the application-level ambiguous-resume matrix passed in the exact-candidate offline and live suites, preserving completed history, failing closed on ambiguous evidence, upgrading persistent formats, and refusing future formats. | None. |
| SemVer and deprecation compatibility | Covered | `public-contract-v5.json`, `TestStage6PublicCompatibilityFixture`, `TestStage6PublicJSONReadersTolerateAdditiveFields`, and `TestStage6DeprecatedFieldsWarnThroughEveryApplicationSurface` passed in exact-candidate Verify and native release jobs. | None. |
| Cross-platform private-file and atomic-replacement behavior | Covered | Tagged run `32551007314` passed the native file, configuration, application, and API suites on macOS Intel, macOS ARM64, and Windows; exact-candidate Verify covered Linux. | None. |
| Operator and recovery documentation | Covered | `OPERATOR_GUIDE.md` covers verified installation, configuration, privileges, operation, observability, stop/resume/recovery, upgrade, rollback limits, security incidents, and supported boundaries. | Keep release-specific supported-version details current. |
| Requirements-to-tests traceability | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps every Section 21 group to named tests, detailed earlier-stage matrices, exact armed commands, workflows, and completed external evidence. | None. |

## Section 21 coverage groups

These groups prevent the release checklist from hiding data-plane regressions.
Each must link to named tests or an explicitly armed command before closeout.

| Section | Status | Authoritative evidence or gap |
| --- | --- | --- |
| 21.1 Build and public surface | Covered | Stage 5 parity evidence remains accepted; the v5 public fixture passed in exact-candidate Verify, and tagged run `32551007314` plus the published `v5.6.0-rc.4` assets cover every required platform and version surface. |
| 21.2 Configuration and security | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps defaults, template preservation, redaction, native permission tests, and cgroup v1/v2 memory fixtures. |
| 21.3 Deterministic DDL and schema | Covered | The traceability index incorporates the all-five-dialect Stage 3/4 planner and live fixture inventories with boundary tests. |
| 21.4 Driver conformance | Covered | The traceability index incorporates the exact Stage 3 shared/live conformance and version-floor matrix. |
| 21.5 Transfer and retry correctness | Covered | The traceability index maps pagination, acknowledgment, cancellation/leaks, transaction/prefix behavior, replay, and memory-bound evidence. |
| 21.6 State, lease, and resume | Covered | Restartability and fencing matrices are retained; immutable v0 SQLite/YAML fixtures prove completed-history upgrade, fail-closed ambiguous evidence, persistent format upgrade, and future-version refusal. |
| 21.7 Incremental and delete behavior | Covered | Stage 4 closeout plus the release trace preserve the armed commands and named tests. |
| 21.8 Strict consistency | Covered | Stage 4 closeout plus the release trace preserve engine limitations, live proofs, and pre-mutation refusals. |
| 21.9 Schema contract and validation | Covered | Stage 4 closeout plus the release trace preserve schema-evolution, validation, and canonicalization evidence. |
| 21.10 Operational surfaces | Covered | Stage 5 closeout plus the release trace preserve authentication, redaction, observability, cancellation, and parity gates. |
| 21.11 Live database matrix and CI | Covered | Exact-candidate Verify run `32551315762` covers offline, race, static, vulnerability, armed live, and armed live-race gates. Tagged run `32551007314` covers cross-platform builds, native smokes, pre/post-upload checksums, and publication. |

## Closeout evidence

- Release-candidate commit: `817b4d3720c138859a841adfd56fc2ac9570a8a1`.
- Exact-candidate Verify with armed live race:
  <https://github.com/johndauphine/dmtx/actions/runs/32551315762>.
- Clean tagged release workflow:
  <https://github.com/johndauphine/dmtx/actions/runs/32551007314>.
- Published release and six assets:
  <https://github.com/johndauphine/dmtx/releases/tag/v5.6.0-rc.4>.
- Annotated tag `v5.6.0-rc.4` dereferences to the release-candidate commit.
