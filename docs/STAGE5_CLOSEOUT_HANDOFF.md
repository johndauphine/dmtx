# Stage 5 closeout handoff

## Status

Stage 5 is an acceptance candidate based on the fresh remote baseline
`eea32e14c641c4465fa49d2f866cc95cde46ae90` on
`codex/webui-command-palette-metadata`. Final acceptance requires the GitHub
offline race/static job and the unchanged armed Stage 4 live job to pass on the
published commit. The final commit SHA and workflow links are recorded below
after publication.

- Final commit: pending
- GitHub verification: pending
- Armed Stage 4 live gate: pending

## Accepted scope and command contract

`STAGE5_COMMAND_MATRIX.md` is the final command matrix. Every registered
command has a Supported or Omitted disposition; no command remains Planned.
The approved reductions are:

- CLI and the authenticated WebUI are the supported operator surfaces. The TUI
  is omitted; remote operators use SSH port forwarding.
- Encrypted profile save/list/delete ship. Portable export/import is deferred,
  plaintext export is refused, and the action is absent from help.
- AI is optional, display-only `config-review` over sanitized facts. Runbook,
  evaluation, patch-application, and archive actions are omitted.
- Observability and Slack sinks use YAML configuration only and remain outside
  migration/resume identity hashes.
- External orchestration owns durable operator-log capture and retention. DMTX
  retains migration-state history and bounded browser-local input recall.

## Delivered Stage 5 surface

- Authenticated loopback-only PWA console with registry-driven discovery,
  aliases, autocomplete, bounded history/transcript, plain/boxed/error output,
  and browser-shell help/about/session/logs/clear/quit actions.
- Shared application/API behavior for run, resume, status, history, validate,
  diagnose, preflight, analyze, configuration, encrypted profiles, init,
  init-secrets, setup, and the reduced AI config review.
- Durable job execution with reconnectable SSE, explicit cancellation, safe
  reload/recovery, a single run/resume slot, and sanitized public request and
  terminal-result projections.
- PWA manifest, icon, service worker, installable shell, and network-first auth
  behavior that never substitutes a cached shell for an expired session.
- Session lifetime/rotation, failed-auth limiting, Host/Origin checks,
  no-store responses, bounded completion, credential-shaped input/output
  screening, and text-only DOM rendering.
- Best-effort structured text/NDJSON logs, Prometheus metrics, OTLP/HTTP traces,
  and optional Slack summaries. Core instrumentation reports real phase,
  write-duration, active-writer, queue, retry, byte, fallback, adjustment, and
  error facts without allowing a telemetry failure to alter migration results.

## Local acceptance evidence

All commands used the repository Go version through the pinned local Go 1.25.7
toolchain and an isolated module/build cache.

| Gate | Result |
| --- | --- |
| `go test ./... -count=1 -timeout 15m` | Pass: every package, including API, app, migrate, config, and observability. |
| `go vet ./...` | Pass. |
| `golangci-lint v2.12.2 run` | Pass: `0 issues.` |
| `gofmt -l internal cmd test` and `git diff --check` | Pass: no output. |
| Linux `go build ./cmd/dmtx` | Pass. |
| Windows amd64 `go build ./cmd/dmtx` | Pass. |
| Node syntax check for `test/browser/stage5.cjs` | Pass. |
| Race detector, sandbox-safe packages | Pass for audit, config, contract, engine, migrate, observability, profiles, schema, secrets, and state. |
| Full local race detector | The privileged runner's approval review timed out twice; GitHub CI is the authoritative full-suite race gate. |

The local race run used the official Zig 0.16.0 x86_64 Linux archive as its C
compiler. The downloaded archive matched the SHA-256 published in Zig's
official release metadata before it was used.

### Armed browser evidence

The opt-in Playwright harness ran against Microsoft Edge and a real loopback
DMTX process:

```text
DMTX_STAGE5_BROWSER=1 go test ./internal/api \
  -run '^TestStage5BrowserE2E$' -count=1 -v -timeout 5m
--- PASS: TestStage5BrowserE2E (6.00s)
```

It discovers every Supported WebUI command and exercises real SQLite migration,
inspection/advisory commands, profile operations, setup success/cancel/refusal,
masked failure, input-recall exclusions, exact started-event projection,
reload/SSE recovery, explicit cancel, PWA control/cache/session expiry, mobile
layout, ARIA, transcript bounds, and browser-console cleanliness. Only the
second migration is held synthetically to make cancel/recovery deterministic;
the first migration is real SQLite work through the application seam.

## GitHub and live evidence

Pending publication. Record the pull request or workflow URL, the `test` job,
and the `Armed live gate` job here. The live job must retain
`DMTX_STAGE4_LIVE_REQUIRED=1`; a normal offline skip is not evidence.

## Deliberate omissions and residual risks

- TUI, cache, in-console serve, a remote-bind flag, a native desktop shell,
  global sink flags, portable profile export/import, and broader AI actions are
  deliberate omissions described in the command matrix.
- DMTX does not store a durable operator log archive; deployment orchestration
  must capture stdout/stderr and apply its retention policy.
- OTLP and Slack delivery are best effort. Network or sink failure cannot fail
  or change a migration.
- Profile portability remains unavailable until passphrase-protected export and
  import ship together with round-trip and redaction evidence.
- The pull-request armed live gate runs without the race detector; live race is
  retained by the scheduled/workflow-dispatch gate, while every pull request
  still runs the full offline race suite.

## Exact Stage 6 boundary

Stage 5 ends at operator surfaces, their shared application/API seam,
observability, security, delivery, and parity evidence. Stage 6 and later own
migration-engine capability expansion, type mapping, schema behavior, transfer
semantics, live cross-engine coverage, and new advisory or profile-portability
features. Later work must not move migration decisions into the browser or
weaken Stage 5 authentication, redaction, parity, or release gates.
