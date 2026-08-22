# Stage 6 closeout handoff

## Status

Stage 6 is **accepted** for DMTX prerelease `v1.0.0-rc.1` at main merge commit
`02e9a207fdfdbe190e9c7475771ffe7a5e5b467e` (PR #50). DMTX's first product
release line is 1.0; DMT 5.6.0 is the upstream compatibility/reference target
and is not DMTX's release version.

The previously accepted `v5.6.0-rc.4` publication at commit
`817b4d3720c138859a841adfd56fc2ac9570a8a1` is retained below only as
explicitly superseded historical evidence. It was published under the wrong
DMTX product version and cannot close this release gate. The remote tag and
release are intentionally not altered by this repository correction.

## Delivered release hardening

- Reproducible, trimmed, self-contained archives for macOS Intel/ARM64, Linux
  x86-64/ARM64, and Windows x86-64.
- Linker-injected release version with native execution smoke tests.
- SHA-256 manifest verification before upload and after a fresh download.
- Tagged GitHub release publication only after artifacts, native tests, and
  downloaded checksums pass.
- Pinned reachable-dependency scanning and a documented short-lived exception
  policy.
- Upgrade to Go 1.26.6 and `golang.org/x/text` 0.39.0, which removed every
  reachable finding reported by the Stage 6 scanner.
- Immutable v0 SQLite and YAML state fixtures proving completed-history
  preservation, fail-closed ambiguous incomplete evidence, persistent format
  upgrade, and newer-format refusal.
- Immutable v5 public contract fixture for commands, aliases, exit codes, and
  deprecations; shared application diagnostics make old-field warnings visible
  in CLI, API, and WebUI outcomes.
- Native macOS Intel/ARM64 and Windows file/config/application/API verification.
- Release operator, security, acceptance, and requirement-to-test documents.

## Historical local evidence on 2026-08-21

The checks below are useful historical evidence for the candidate source, but
they do not verify a DMTX 1.0 release artifact.

| Gate | Result |
| --- | --- |
| `go test ./... -count=1 -timeout 20m` | Pass, every package. |
| `go vet ./...` | Pass. |
| `golangci-lint v2.12.2 run` | Pass, zero issues. |
| `govulncheck v1.1.4 ./...` on Go 1.26.6 | Pass, zero reachable vulnerabilities. |
| `actionlint v1.7.12` on Verify and Release workflows | Pass. |
| Five `CGO_ENABLED=0` release cross-builds | Pass. |
| Linux artifact `--version` with release-candidate injection | Pass. |
| Local SHA-256 manifest verification for all five binaries | Pass. |
| `TestStage6*` focused compatibility and upgrade suites | Pass. |
| `git diff --check` | Pass. |

The local race command could not start in the restricted workspace because it
has no C development headers. The GitHub offline race gate for that historical
candidate passed in run `32551315762`; the authoritative DMTX 1.0 result is
recorded below.

## Superseded external evidence on 2026-08-22

All evidence in this section concerns the incorrectly versioned
`v5.6.0-rc.4` publication. It is retained for auditability only and is not
evidence of a DMTX 1.0 release.

| Evidence | Result |
| --- | --- |
| Release-candidate commit | `817b4d3720c138859a841adfd56fc2ac9570a8a1`. |
| [Exact-candidate Verify dispatch](https://github.com/johndauphine/dmtx/actions/runs/32551315762) | Pass: offline tests, vet, lint, reachable-dependency scan, offline race, Linux/Windows builds, armed five-fixture live suite, and armed live race. |
| [Tagged Release artifacts run](https://github.com/johndauphine/dmtx/actions/runs/32551007314) | Pass: five platform builds, source tests, pre-upload checksums, Linux version execution, macOS Intel/ARM64 and Windows native/version execution, fresh-download checksum verification, and publication. |
| [Published `v5.6.0-rc.4` release](https://github.com/johndauphine/dmtx/releases/tag/v5.6.0-rc.4) | Published with five platform archives and `SHA256SUMS`. |
| Independent publication audit | A fresh download of all six assets passed `sha256sum --check SHA256SUMS` for all five archives; the published Linux x86-64 binary reported `5.6.0-rc.4`. |
| Tag audit | The remote annotated `v5.6.0-rc.4` tag dereferences to the exact release-candidate commit. |

The earlier `v5.6.0-rc.1`, `v5.6.0-rc.2`, and `v5.6.0-rc.3` tags remain
untouched failed candidates without published releases. `v5.6.0-rc.4` is the
first candidate with a clean tagged run and published asset set, but all four
tags use the incorrect DMTX product-version line.

## Authoritative DMTX 1.0 external evidence on 2026-08-22

| Evidence | Result |
| --- | --- |
| Tag and commit audit | The `v1.0.0-rc.1` remote tag and `origin/main` resolved to `02e9a207fdfdbe190e9c7475771ffe7a5e5b467e` at audit time. |
| [Exact-tag Verify dispatch](https://github.com/johndauphine/dmtx/actions/runs/32584626335) | Pass: offline tests, vet, golangci-lint, zero reachable `govulncheck` findings, offline race, Linux/Windows builds, armed five-fixture live suite, and armed live race. |
| [Tagged Release artifacts run](https://github.com/johndauphine/dmtx/actions/runs/32584307354) | Pass: five platform artifacts, SHA256 generation and pre-upload validation, Linux version smoke, macOS Intel/ARM64 and Windows native tests and version smokes, re-downloaded manifest verification, and publication. |
| [Published `v1.0.0-rc.1` prerelease](https://github.com/johndauphine/dmtx/releases/tag/v1.0.0-rc.1) | Published, not a draft, marked prerelease, and contains exactly five platform archives plus `SHA256SUMS`. |
| Independent publication audit | A fresh download passed `shasum -a 256 -c SHA256SUMS` for all five archives; the downloaded Darwin ARM64 binary reported `1.0.0-rc.1`. |

## DMTX 1.0 external acceptance checklist

For DMTX `v1.0.0-rc.1` at the recorded release commit:

- [x] Verify workflow: offline tests, vet, lint, vulnerability scan, full
  offline race, and existing builds are green.
- [x] Armed live job is green with all five fixtures and no unexpected skip.
- [x] Armed live race job is green from workflow dispatch.
- [x] Release artifacts workflow is green for macOS Intel/ARM64, Linux
  x86-64/ARM64, and Windows x86-64.
- [x] Uploaded checksum verification job is green.
- [x] Downloaded `SHA256SUMS` validates every published archive.
- [x] Native Linux, macOS, and Windows binaries report `1.0.0-rc.1` in CI
  version smokes.
- [x] Links to the exact Verify run, Release artifacts run, and tagged release
  are recorded here.

## Acceptance rule

Every checkbox is closed with exact `v1.0.0-rc.1` evidence. Stage 6 and the
reconstruction are accepted while the Stage 3, Stage 4, and Stage 5 boundary
decisions remain unchanged.
