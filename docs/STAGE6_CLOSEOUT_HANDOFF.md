# Stage 6 closeout handoff

## Status

Stage 6 implementation is release-candidate ready but is **not yet accepted**.
All local implementation and documentation gates are complete. Acceptance
requires the external GitHub and armed live evidence listed below at the exact
candidate commit; this file must be updated with links before changing the
status to accepted.

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

## Local evidence on 2026-08-21

| Gate | Result |
| --- | --- |
| `go test ./... -count=1 -timeout 20m` | Pass, every package. |
| `go vet ./...` | Pass. |
| `golangci-lint v2.12.2 run` | Pass, zero issues. |
| `govulncheck v1.1.4 ./...` on Go 1.26.6 | Pass, zero reachable vulnerabilities. |
| `actionlint v1.7.12` on Verify and Release workflows | Pass. |
| Five `CGO_ENABLED=0` release cross-builds | Pass. |
| Linux artifact `--version` with `5.6.0-rc.1` injection | Pass. |
| Local SHA-256 manifest verification for all five binaries | Pass. |
| `TestStage6*` focused compatibility and upgrade suites | Pass. |
| `git diff --check` | Pass. |

The local race command could not start in the restricted workspace because it
has no C development headers. The configured GitHub offline race gate remains
authoritative; this is not satisfied by the ordinary local suite.

## Required external acceptance evidence

At the exact release-candidate commit:

- [ ] Verify workflow: offline tests, vet, lint, vulnerability scan, full
  offline race, and existing builds are green.
- [ ] Armed live job is green with all five fixtures and no unexpected skip.
- [ ] Armed live race job is green from schedule or workflow dispatch.
- [ ] Release artifacts workflow is green for macOS Intel/ARM64, Linux
  x86-64/ARM64, and Windows x86-64.
- [ ] Uploaded checksum verification job is green.
- [ ] Downloaded `SHA256SUMS` validates every published archive.
- [ ] Native Linux, macOS, and Windows binaries report the candidate version.
- [ ] Links to the exact Verify run, Release artifacts run, and tagged release
  are recorded here.

## Acceptance rule

Do not declare Stage 6 or the reconstruction complete while any checkbox above
is open. When every item is evidenced, replace the candidate status with
accepted, record the exact commit and run links, and preserve the Stage 3,
Stage 4, and Stage 5 boundary decisions unchanged.
