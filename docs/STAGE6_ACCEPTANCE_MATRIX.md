# Stage 6 acceptance matrix

## Purpose and authority

Stage 6 is release hardening. This matrix maps the normative release contract
in `RECREATE_DMT.md` Sections 18, 20, 21, and 22 to repository evidence and
names every remaining gap. Earlier stage closeouts remain evidence; Stage 6
does not reopen their accepted product decisions or weaken their gates.

Stage 6 is complete only when every row marked Required is Covered and the
closeout records a clean tagged release run plus an upgrade-fixture run. DMTX's
first product release is 1.0; DMT 5.6.0 remains the compatibility target. The
`v1.0.0-rc.1` prerelease satisfies the current release-candidate evidence.

## Release gates

| Requirement | Status | Current evidence | Remaining work |
| --- | --- | --- | --- |
| macOS x86-64 and ARM64, Linux x86-64 and ARM64, and Windows x86-64 binaries | Covered | The [tagged Release artifacts run](https://github.com/johndauphine/dmtx/actions/runs/32584307354) built and published all five artifacts for `v1.0.0-rc.1` at `02e9a207fdfdbe190e9c7475771ffe7a5e5b467e`. The [published prerelease](https://github.com/johndauphine/dmtx/releases/tag/v1.0.0-rc.1) contains those five archives and `SHA256SUMS`. | None. |
| SHA-256 manifest covering every packaged binary | Covered | Release run `32584307354` generated and verified `SHA256SUMS` before upload and after artifact re-download. An independent fresh-download audit passed `shasum -a 256 -c SHA256SUMS` for all five archives. | None. |
| Release version reported by `dmtx --version` | Covered | Release run `32584307354` passed Linux, macOS Intel/ARM64, and Windows version smokes for `1.0.0-rc.1`. The independently downloaded Darwin ARM64 artifact also reported `1.0.0-rc.1`. | None. |
| Clean offline tests, vet, lint, and race detector | Covered | The [exact-tag Verify dispatch](https://github.com/johndauphine/dmtx/actions/runs/32584626335) passed offline tests, vet, golangci-lint, the offline race detector, and Linux/Windows builds at `02e9a207fdfdbe190e9c7475771ffe7a5e5b467e`. | None. |
| Dependency vulnerability scanning | Covered | Verify run `32584626335` ran pinned `govulncheck` with zero reachable findings. | None. |
| Armed live database gate | Covered | Verify run `32584626335` passed the armed five-fixture live suite and armed live race suite. | None. |
| Historical state upgrade support window | Covered | `TestStage6HistoricalStateUpgradeMatrix`, `TestStage6YAMLStateRejectsFutureVersion`, and the application-level ambiguous-resume matrix passed in the exact-tag offline and live suites, preserving completed history, failing closed on ambiguous evidence, upgrading persistent formats, and refusing future formats. | None. |
| SemVer and deprecation compatibility | Covered | `public-contract-v5.json`, `TestStage6PublicCompatibilityFixture`, `TestStage6PublicJSONReadersTolerateAdditiveFields`, and `TestStage6DeprecatedFieldsWarnThroughEveryApplicationSurface` passed in exact-tag Verify and native release jobs. | None. |
| Cross-platform private-file and atomic-replacement behavior | Covered | Release run `32584307354` passed the native file, configuration, application, and API suites on macOS Intel, macOS ARM64, and Windows; Verify run `32584626335` covered Linux. | None. |
| Operator and recovery documentation | Covered | `OPERATOR_GUIDE.md` covers verified installation, configuration, privileges, operation, observability, stop/resume/recovery, upgrade, rollback limits, security incidents, and supported boundaries. | Keep release-specific supported-version details current. |
| Requirements-to-tests traceability | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps named tests, earlier-stage matrices, exact armed commands, and the completed `v1.0.0-rc.1` Verify and Release evidence. | None. |

## Section 21 coverage groups

These groups prevent the release checklist from hiding data-plane regressions.
Each must link to named tests or an explicitly armed command before closeout.

| Section | Status | Authoritative evidence or gap |
| --- | --- | --- |
| 21.1 Build and public surface | Covered | Stage 5 parity evidence remains accepted. Release run `32584307354` built all five artifacts and passed Linux, macOS Intel/ARM64, and Windows version smokes for `v1.0.0-rc.1`. |
| 21.2 Configuration and security | Covered | `STAGE6_REQUIREMENTS_TESTS.md` maps defaults, template preservation, redaction, native permission tests, and cgroup v1/v2 memory fixtures. |
| 21.3 Deterministic DDL and schema | Covered | The traceability index incorporates the all-five-dialect Stage 3/4 planner and live fixture inventories with boundary tests. |
| 21.4 Driver conformance | Covered | The traceability index incorporates the exact Stage 3 shared/live conformance and version-floor matrix. |
| 21.5 Transfer and retry correctness | Covered | The traceability index maps pagination, acknowledgment, cancellation/leaks, transaction/prefix behavior, replay, and memory-bound evidence. |
| 21.6 State, lease, and resume | Covered | Restartability and fencing matrices are retained; immutable v0 SQLite/YAML fixtures prove completed-history upgrade, fail-closed ambiguous evidence, persistent format upgrade, and future-version refusal. |
| 21.7 Incremental and delete behavior | Covered | Stage 4 closeout plus the release trace preserve the armed commands and named tests. |
| 21.8 Strict consistency | Covered | Stage 4 closeout plus the release trace preserve engine limitations, live proofs, and pre-mutation refusals. |
| 21.9 Schema contract and validation | Covered | Stage 4 closeout plus the release trace preserve schema-evolution, validation, and canonicalization evidence. |
| 21.10 Operational surfaces | Covered | Stage 5 closeout plus the release trace preserve authentication, redaction, observability, cancellation, and parity gates. |
| 21.11 Live database matrix and CI | Covered | Verify run `32584626335` passed offline, race, static, vulnerability, armed live, and armed live-race gates. Release run `32584307354` passed cross-platform builds, native smokes, pre/post-upload checksums, and prerelease publication. |

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

## DMTX 1.0 release evidence

- Tag `v1.0.0-rc.1` dereferences to main merge commit
  `02e9a207fdfdbe190e9c7475771ffe7a5e5b467e` (PR #50).
- [Exact-tag Verify run](https://github.com/johndauphine/dmtx/actions/runs/32584626335)
  succeeded with offline tests, vet, golangci-lint, zero reachable
  `govulncheck` findings, offline race, Linux/Windows builds, armed live, and
  armed live race.
- [Tagged Release artifacts run](https://github.com/johndauphine/dmtx/actions/runs/32584307354)
  succeeded with five artifacts, pre-upload and re-downloaded checksum
  verification, Linux version smoke, macOS Intel/ARM64 and Windows native tests
  and version smokes, and publication.
- The [published `v1.0.0-rc.1` prerelease](https://github.com/johndauphine/dmtx/releases/tag/v1.0.0-rc.1)
  is not a draft, is marked prerelease, and contains exactly five platform
  archives plus `SHA256SUMS`.
- An independent fresh-download audit passed `shasum -a 256 -c SHA256SUMS` for
  all five archives; the downloaded Darwin ARM64 binary reported `1.0.0-rc.1`.
