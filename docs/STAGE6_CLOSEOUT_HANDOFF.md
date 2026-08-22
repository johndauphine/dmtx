# Stage 6 closeout handoff

## Status

Stage 6 is **accepted**. Every required local, GitHub, native-platform,
checksum, publication, armed-live, and armed-live-race gate passed for release
candidate `v5.6.0-rc.4` at commit
`817b4d3720c138859a841adfd56fc2ac9570a8a1`.

The closeout documentation follows the tagged commit and changes evidence only;
the immutable release tag remains attached to the exact code and workflow
candidate that passed the gates.

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
| Linux artifact `--version` with release-candidate injection | Pass. |
| Local SHA-256 manifest verification for all five binaries | Pass. |
| `TestStage6*` focused compatibility and upgrade suites | Pass. |
| `git diff --check` | Pass. |

The local race command could not start in the restricted workspace because it
has no C development headers. The exact-candidate GitHub offline race gate is
therefore authoritative and passed in run `32551315762`.

## Authoritative external evidence on 2026-08-22

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
first candidate with a clean tagged run and published asset set.

## External acceptance checklist

At the exact release-candidate commit:

- [x] Verify workflow: offline tests, vet, lint, vulnerability scan, full
  offline race, and existing builds are green.
- [x] Armed live job is green with all five fixtures and no unexpected skip.
- [x] Armed live race job is green from workflow dispatch.
- [x] Release artifacts workflow is green for macOS Intel/ARM64, Linux
  x86-64/ARM64, and Windows x86-64.
- [x] Uploaded checksum verification job is green.
- [x] Downloaded `SHA256SUMS` validates every published archive.
- [x] Native Linux, macOS, and Windows binaries report the candidate version.
- [x] Links to the exact Verify run, Release artifacts run, and tagged release
  are recorded here.

## Acceptance rule

Every checkbox is closed with exact evidence. Stage 6 and the reconstruction
are accepted while the Stage 3, Stage 4, and Stage 5 boundary decisions remain
unchanged.
