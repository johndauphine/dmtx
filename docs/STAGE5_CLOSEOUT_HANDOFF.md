# Stage 5 closeout handoff

## Snapshot

- Branch: `codex/stage5-closeout`
- Repository HEAD when this handoff was updated:
  `c005e5f7be79231a48fd6ca7f8ba39e30d8b21a3`
- Working tree: intentionally uncommitted Stage 5 implementation and closeout
  documentation changes; this handoff is evidence for review, not a release
  declaration. The 2026-08-12 browser result exercised that uncommitted tree,
  so no commit SHA uniquely identifies the tested contents.
- Command matrix: [STAGE5_COMMAND_MATRIX.md](STAGE5_COMMAND_MATRIX.md)
- Operator instructions: [STAGE5_WEBUI_OPERATIONS.md](STAGE5_WEBUI_OPERATIONS.md)

## Evidence table

| Requirement | Current evidence | Status |
| --- | --- | --- |
| No `Planned` registry commands; each command supported or omitted | `internal/contract/registry.go`; matrix lists application commands, browser-local commands, and omissions. | Evidence collected |
| Encrypted profile persistence and portability | Focused tests cover encrypted store behavior, portable export/import round trip, owner-only output, tamper refusal, invalid import, and bounded/link-safe reads. The browser runner executes real read-only `/profile list` against one seeded entry in a temporary encrypted SQLite store. | Evidence collected; profile mutations/export/import are not browser-executed. |
| Supported command behavior | Focused application/API tests cover every supported command, including `init-secrets` creation/refusal/permission behavior, browser-local command execution, setup state machines, and applicable cancellation paths. The real-browser runner checks shared-parser/default parity for every supported server command. | Evidence collected within the accepted Stage 5 contract; side-effecting provider commands are covered below the browser rather than executed against production-like systems from the UI fixture. |
| AI and setup registered as supported | AI config-review parsing/execution and SQLite/PostgreSQL/SQL Server setup application/API tests exist. PostgreSQL and SQL Server setup use protected credential origins, required encrypted TLS, and verified source/target connections. PostgreSQL defaults to `ssl_mode=require`; SQL Server can validate the peer with a supplied CA file. | Evidence collected within the accepted Stage 5 scope. |
| Real browser console interaction | The latest `go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1` run passed with local Google Chrome on 2026-08-12. The controlled fixture uses temporary local state and encrypted profile storage, with no Docker or external migration DB. | Evidence collected; no commit SHA is claimed for the uncommitted tested tree. |
| Discoverability, parse parity, completion, recall, PWA restrictions | Same real-browser runner verifies all supported registry spellings, slash/`@` completion, safe parse parity, local recall exclusions, active service-worker control, Chrome's CDP-parsed manifest, and the exact fixed Cache Storage entry set remaining unchanged after authenticated root/API fetches. It also verifies real profile-list rendering. | Evidence collected within fixture scope; no install-prompt click is claimed. |
| Job behavior | `internal/api/job_test.go` covers client loss, cancellation, stream lifetime/status/recovery, retained events, and progress event shape. Browser runner covers reload/recovery and explicit cancellation. | Focused and full-suite evidence collected. |
| Durable migration history vs browser recall | DMTX state stores provide durable run history used by `/history`; console recall is bounded browser-local storage; `/logs` is a session transcript. | Normative wording reconciled and implementation distinction recorded |
| SSE progress durability | Progress occurs after durable checkpoint work and SSE reconnect has retained presentation state; sequence numbers may be non-contiguous. The SSE event buffer is not a restart-surviving archive. | Evidence collected for presentation behavior only |
| Loopback, authentication, SSH, idle/no-browser, PWA | Server/auth/idle/instance/UI tests and operations guide document the behavior; browser runner covers authenticated launch and PWA restrictions. Deterministic security tests cover failed-auth throttling; literal localhost/loopback Host validation with differing SSH local/remote ports; arbitrary DNS, non-loopback, and malformed Host rejection; absolute expiry; credential rotation after expiry; and a non-sliding pre-expiry handoff. | Evidence collected. |
| Completion containment | `internal/api/completion_test.go` covers root confinement, symlink handling, special-file exclusion, auth, uniform refusal, and disabled completion. | Focused and full-suite evidence collected. |
| Shipped-sink redaction | Behavioral sentinels exercise CLI text/JSON/progress renderers, synchronous API output, retained job status, replayed SSE progress/outcomes, console/setup/history protections, state, audit, AI, and portable profiles. Individual payload wire-shape tests protect structured schemas. `TestStage5CrossSurfaceRedactionEvidence` is only a drift guard that keeps those test declarations in the manifest; it is not treated as execution evidence. | Focused and full-suite evidence collected across every sink that ships in the accepted Stage 5 scope. |
| CI formatting and browser gate | `verify.yml` fails on gofmt output and requires `/usr/bin/google-chrome` before running the isolated browser test. | Workflow change and local equivalent evidence collected; hosted CI result pending. |
| Offline/race/static/live release gates | `test/fixtures/gate.sh --race` passed build, vet, offline tests/race, golangci-lint, Linux/Windows builds, fixture health/provisioning, and fully armed Stage 4 normal/race matrices on the current integrated working tree. Module hygiene, formatting, JS syntax, and real Chrome passed separately on the same tree. | Current-tree local evidence collected; immutable commit and hosted CI remain pending. |

## Exact commands recorded in this lane

```sh
go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1 -v
# PASS (local Google Chrome, 2026-08-12; test 1.44s, internal/api 1.695s)

source test/fixtures/env.sh
DMTX_STAGE4_LIVE_REQUIRED=1 go test ./internal/app -run TestMSSQLSetupLiveTLS -count=1 -v
# PASS (disposable SQL Server 2022 TLS fixture, 2026-08-12). The real guided
# flow verified both endpoints and produced only protected password-file
# origins in a temporary output configuration.

node --check internal/api/static/console.js
go mod tidy -diff
go test ./... -count=1
go vet ./...
golangci-lint run
go test -race ./... -count=1
go build ./...
GOOS=windows GOARCH=amd64 go build -o /private/tmp/dmtx-stage5.exe ./cmd/dmtx
git diff --check
gofmt -l $(rg --files -g '*.go')
# all passed on the current integrated tree; golangci-lint reported 0 issues,
# go mod tidy -diff produced no diff, and gofmt produced no paths

source test/fixtures/env.sh
DMTX_STAGE4_LIVE_REQUIRED=1 go test ./... -count=1 -timeout 30m
# PASS on current integrated tree through test/fixtures/gate.sh --race;
# internal/migrate 182.721s

DMTX_STAGE4_LIVE_REQUIRED=1 go test -race ./... -count=1 -timeout 30m
# PASS on current integrated tree through test/fixtures/gate.sh --race;
# internal/migrate 273.315s
```

The complete local gate reported `all checks passed`. It used all 16 armed
endpoint variables and five healthy, freshly provisioned TLS fixtures. It ran
against the uncommitted working tree, so formal publication still needs an
immutable commit and hosted workflow result; no commit SHA is claimed here.

The browser command found and used:

```text
/Applications/Google Chrome.app/Contents/MacOS/Google Chrome
```

## Deliberate omissions and boundaries

The registry marks `wizard`, `verbosity`, `explore`, `cache`, and `serve` within
the console omitted, with implementation rationales in the command matrix. The
TUI is normatively omitted; CLI plus the loopback/SSH-forwarded WebUI is the
implemented operator model. A native desktop shell and remote bind remain out
of scope. The accepted Stage 5 contract has no cache because no safe cache
exists; it does not imply a future cache implementation.

Stage 6 owns release hardening: clean-environment/full-gate evidence,
cross-platform release artifacts, upgrade/deprecation tests, dependency
vulnerability work, and final release documentation. It does not weaken the
Stage 5 UI/auth/redaction contract.

## Residual risks and open requirements

- The real-browser fixture executes read-only `/profile list` from its temporary
  encrypted store. It safely parses other provider/side-effecting commands but
  does not execute a real migration, AI provider, profile mutation, or profile
  export/import from the browser.
- Browser SSE retention supports reconnect while the process is alive; a
  restart relies on durable migration state, not a durable event transcript.
- The first armed race attempt lost the disposable SQL Server process (container
  exit 139, not OOM-killed), after which SQL Server routes correctly failed
  closed. After restart and reprovision, the first affected test passed alone
  under race and the complete armed race matrix passed. This is recorded as
  fixture instability rather than erased from the evidence trail.
- Hosted CI still needs to run the changed workflow after publication. Local
  `actionlint` was unavailable; the workflow was YAML-parsed and reviewed, but
  that is not a substitute for the hosted check.

## Accepted owner scope decisions

The owner accepted the following exact Stage 5 contract on 2026-08-12. These
are normative scope decisions, reflected in `RECREATE_DMT.md`; they are not
claims that deferred features are implemented.

| Area | Accepted Stage 5 boundary | Evidence or consequence |
| --- | --- | --- |
| Interactive surfaces and retention | CLI plus authenticated loopback/SSH WebUI; no TUI. Durable run history lives in the state DB, browser recall is bounded/local, and orchestration owns full logs/transcripts. | Registry, browser runner, state/history tests, and operations guide. |
| Shipped observability | Sanitized structured progress and terminal outcomes only. No Stage 5 structured-log subsystem, Prometheus, OTLP, or Slack lifecycle notifications. | Shipped sinks remain subject to redaction; any future sink inherits that requirement. |
| Audit policy | Mandatory append-only SHA-256 hash-linked audit, fail-closed on audit failure. No disable, optional-chain, compliance-mode, or warn-and-continue control; terminal read-only handling is not required. | Existing lifecycle/tamper evidence applies to the accepted policy. |
| AI | Metadata-only display configuration review; `runbook` is its alias. No evals, patches, distinct runbook, or operation-specific hooks. | Provider behavior remains advisory and redacted; no cache exists or is required. |
| Cache | No cache command: DMTX has no safe cache to clear. | Clearing durable coordination state remains unsafe and misleading. |
| Guided setup | SQLite, PostgreSQL, and SQL Server only; source/target connection testing is part of setup and `analyze` is a separate explicit command. | PostgreSQL/SQL Server credential/TLS/connection evidence and the SQL Server live TLS setup test are recorded above. |
| Global automation controls | No Stage 5 requirement for an explicit new-run ID, global JSON/file output selector, configurable shutdown timeout, or machine-progress interval. | Supported config/profile/state and WebUI controls remain documented; deferred controls require future additive contracts. |

Acceptance of these boundaries does not by itself close the immutable
publication gates listed elsewhere in this handoff and checklist.
