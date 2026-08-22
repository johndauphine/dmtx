# Stage 6 acceptance matrix

## Purpose and authority

Stage 6 is release hardening. This matrix maps the normative release contract
in `RECREATE_DMT.md` Sections 18, 20, 21, and 22 to repository evidence and
names every remaining gap. Earlier stage closeouts remain evidence; Stage 6
does not reopen their accepted product decisions or weaken their gates.

Stage 6 is complete only when every row marked Required is Covered and the
closeout records a clean tagged release run plus an upgrade-fixture run. The
previous closeout is reopened: DMTX's first product release is 1.0, not the
DMT 5.6.0 compatibility target. A `v1.0.0-rc.1` (or later 1.0 RC) must be
published and verified before Stage 6 can be accepted again.

## Release gates

| Requirement | Status | Current evidence | Remaining work |
| --- | --- | --- | --- |
| macOS x86-64 and ARM64, Linux x86-64 and ARM64, and Windows x86-64 binaries | Blocked | The tagged run and published `v5.6.0-rc.4` assets are superseded historical evidence: they used the incorrect DMTX product version. | Publish and verify the five artifacts from `v1.0.0-rc.1` (or a later 1.0 RC). |
| SHA-256 manifest covering every packaged binary | Blocked | The `v5.6.0-rc.4` checksum evidence is superseded historical evidence only. | Verify a fresh manifest for every `v1.0.0-rc.1` artifact after upload. |
| Release version reported by `dmtx --version` | Blocked | The historical run injected and reported `5.6.0-rc.4`, which is not DMTX's product version. | Execute `v1.0.0-rc.1` artifacts on Linux, macOS Intel/ARM64, and Windows and confirm `1.0.0-rc.1`. |
| Clean offline tests, vet, lint, and race detector | Blocked | Verify run `32551315762` is superseded historical evidence for `817b4d3720c138859a841adfd56fc2ac9570a8a1`, not evidence for the intended DMTX 1.0 release commit. | Run the offline tests, vet, lint, race detector, and Linux/Windows builds in Verify for `v1.0.0-rc.1`. |
| Dependency vulnerability scanning | Blocked | The zero-finding `govulncheck` result in Verify run `32551315762` is superseded historical evidence only. | Run the pinned reachable-dependency scan in Verify for `v1.0.0-rc.1` and record the result. |
| Armed live database gate | Blocked | The normal and race five-fixture runs in Verify `32551315762` are superseded historical evidence only. | Run the armed live and armed live-race Verify gates for `v1.0.0-rc.1`, with no unexpected skips. |
| Historical state upgrade support window | Covered | `TestStage6HistoricalStateUpgradeMatrix`, `TestStage6YAMLStateRejectsFutureVersion`, and the application-level ambiguous-resume matrix passed in the exact-candidate offline and live suites, preserving completed history, failing closed on ambiguous evidence, upgrading persistent formats, and refusing future formats. | None. |
| SemVer and deprecation compatibility | Covered | `public-contract-v5.json`, `TestStage6PublicCompatibilityFixture`, `TestStage6PublicJSONReadersTolerateAdditiveFields`, and `TestStage6DeprecatedFieldsWarnThroughEveryApplicationSurface` passed in exact-candidate Verify and native release jobs. | None. |
| Cross-platform private-file and atomic-replacement behavior | Blocked | Tagged run `32551007314` and Verify `32551315762` are superseded historical native-platform evidence for the wrongly versioned candidate. | Run the native macOS Intel/ARM64 and Windows suites plus the Linux Verify coverage for `v1.0.0-rc.1`. |
| Operator and recovery documentation | Covered | `OPERATOR_GUIDE.md` covers verified installation, configuration, privileges, operation, observability, stop/resume/recovery, upgrade, rollback limits, security incidents, and supported boundaries. | Keep release-specific supported-version details current. |
| Requirements-to-tests traceability | Pending | `STAGE6_REQUIREMENTS_TESTS.md` retains named tests, detailed earlier-stage matrices, exact armed commands, and superseded external evidence. | Add the actual DMTX 1.0 Verify and tagged Release evidence after `v1.0.0-rc.1` completes. |

## Section 21 coverage groups

These groups prevent the release checklist from hiding data-plane regressions.
Each must link to named tests or an explicitly armed command before closeout.

| Section | Status | Authoritative evidence or gap |
| --- | --- | --- |
| 21.1 Build and public surface | Blocked | Stage 5 parity evidence remains accepted, but the tagged run and `v5.6.0-rc.4` assets are superseded historical evidence because they use the wrong DMTX product version. Rerun the tagged artifact and native version smoke checks for `v1.0.0-rc.1`. |
| 21.2 Configuration and security | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps defaults, template preservation, redaction, native permission tests, and cgroup v1/v2 memory fixtures. |
| 21.3 Deterministic DDL and schema | Covered | The traceability index incorporates the all-five-dialect Stage 3/4 planner and live fixture inventories with boundary tests. |
| 21.4 Driver conformance | Covered | The traceability index incorporates the exact Stage 3 shared/live conformance and version-floor matrix. |
| 21.5 Transfer and retry correctness | Covered | The traceability index maps pagination, acknowledgment, cancellation/leaks, transaction/prefix behavior, replay, and memory-bound evidence. |
| 21.6 State, lease, and resume | Covered | Restartability and fencing matrices are retained; immutable v0 SQLite/YAML fixtures prove completed-history upgrade, fail-closed ambiguous evidence, persistent format upgrade, and future-version refusal. |
| 21.7 Incremental and delete behavior | Covered | Stage 4 closeout plus the release trace preserve the armed commands and named tests. |
| 21.8 Strict consistency | Covered | Stage 4 closeout plus the release trace preserve engine limitations, live proofs, and pre-mutation refusals. |
| 21.9 Schema contract and validation | Covered | Stage 4 closeout plus the release trace preserve schema-evolution, validation, and canonicalization evidence. |
| 21.10 Operational surfaces | Covered | Stage 5 closeout plus the release trace preserve authentication, redaction, observability, cancellation, and parity gates. |
| 21.11 Live database matrix and CI | Blocked | Verify `32551315762` and tagged Release `32551007314` are superseded historical evidence for the wrongly versioned candidate; they do not prove the intended DMTX 1.0 release commit. Run Verify, armed live, armed live-race, and tagged Release workflows for `v1.0.0-rc.1`, then record their URLs and results. |

## Superseded external evidence

The following is retained only as an audit trail. `v5.6.0-rc.4` was an
incorrect DMTX product release version, not a DMTX 5.6 release. It must not be
used to close the 1.0 release gate.

- Release-candidate commit: `817b4d3720c138859a841adfd56fc2ac9570a8a1`.
- Exact-candidate Verify with armed live race:
  <https://github.com/johndauphine/dmtx/actions/runs/32551315762>.
- Clean tagged release workflow:
  <https://github.com/johndauphine/dmtx/actions/runs/32551007314>.
- Published release and six assets:
  <https://github.com/johndauphine/dmtx/releases/tag/v5.6.0-rc.4>.
- Annotated tag `v5.6.0-rc.4` dereferences to the release-candidate commit.

## Required 1.0 release evidence

Before accepting Stage 6 again, create and publish `v1.0.0-rc.1` from the
intended release commit, then record its clean Verify and Release workflow
runs, all five uploaded artifacts, fresh `SHA256SUMS` verification, and native
`dmtx --version` output of `1.0.0-rc.1` on Linux, macOS Intel/ARM64, and
Windows. Do not claim that this evidence exists until those external steps are
complete.
