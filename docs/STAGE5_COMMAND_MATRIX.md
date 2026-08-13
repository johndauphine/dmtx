# Stage 5 command matrix

## Authority and terminology

This is the final Stage 5 command-contract matrix. It describes the registry,
the accepted Stage 5 scope, and the evidence collected for this branch. Its
contract disposition is reconciled with
[RECREATE_DMT.md](RECREATE_DMT.md).

**Supported** means discoverable in the authenticated console and implemented
through its documented application or browser-local seam. **Omitted** means
intentionally unavailable, with the operator-facing rationale stated here.
There are no `Planned` commands in the current registry.

`history` is durable migration/run history held in the selected DMTX state
database (SQLite by default, or the explicit YAML state backend). It is not the
console's command recall. The browser retains only a bounded, per-browser local
recall list; it is not sent to DMTX. Durable full logs and transcripts belong to
the calling orchestration system, which captures the CLI/API progress and final
outcome stream.

## Registered migration and application commands

| Command | WebUI | Evidence and operator notes |
| --- | --- | --- |
| `run` | Supported | Authenticated parse/job execution uses the shared application request seam; progress, reconnect, and explicit cancellation are covered by job and real-browser tests. |
| `resume` | Supported | Shared parser/dispatch with state and origin validation; browser parse parity includes it. |
| `status` | Supported | Reads selected/current durable state; browser controlled-job fixture also proves submit/outcome rendering. |
| `history` | Supported | Reads durable run history from DMTX state, distinct from browser recall. Browser parse parity covers its request. |
| `validate` | Supported | Shared parser/dispatch and browser parse parity; failures remain sanitized. |
| `diagnose` | Supported | Shared durable-state diagnosis seam and browser parse parity. |
| `preflight` (`health-check`) | Supported | Canonical spelling and alias are registry-discoverable and browser parse-tested. |
| `analyze` | Supported | Offline effective-plan report uses the shared parse/application seam. |
| `profile save|list|delete|export|import` | Supported | Profiles use encrypted local storage. Portable export/import uses a passphrase file, encrypted format, owner-only output, round-trip/tamper/permission tests, and sanitized errors. The browser test executes a real read-only `/profile list` and renders a seeded entry from a temporary encrypted SQLite store. Save/delete/export/import are not browser-executed; their evidence remains in focused application tests. |
| `ai config-review` | Supported | Registered and parsed through the same request seam. The `runbook` spelling aliases the same metadata-only, display-only configuration review. Stage 5 intentionally omits provider evals, patch output, a distinct runbook, and operation-specific AI hooks. |
| `init` | Supported | Shared parser/dispatch and browser parse parity; overwrite safeguards remain application-owned. |
| `init-secrets` | Supported | Shared parser/dispatch and browser parse parity; focused tests cover protected creation, idempotent refusal, explicit forced replacement, owner-only permissions, and safe failure output. |
| `setup` | Supported | `/setup [postgres|sqlserver] [CONFIG | @CONFIG | --config CONFIG | --profile NAME]` drives the browser-local guided state machine. Stage 5 setup scope is SQLite, PostgreSQL, and SQL Server; analysis remains the separate `analyze` command. PostgreSQL and SQL Server use protected credential origins, required encrypted TLS, and verified source/target connections. PostgreSQL defaults to `ssl_mode=require`; SQL Server can validate the peer with a supplied CA file. On 2026-08-12, the armed `TestMSSQLSetupLiveTLS` drove both real SQL Server endpoints against the disposable SQL Server 2022 TLS fixture and verified protected temporary output. The real-browser runner drives SQL Server setup through its masked source-password prompt, verifies the redacted transcript and absent browser recall, then stops at the optional CA prompt before any connection is attempted. Other engine setup flows are post-Stage-5 work. |
| `config` | Supported | Authenticated shared report seam; browser parse parity covers discovery and request shape. |

## Browser-local console commands

These commands are intentionally browser-local chrome, rather than application
commands. They are still registry-discoverable and exercised by console tests
or the real-browser runner where interaction matters.

| Command | WebUI | Operator behavior |
| --- | --- | --- |
| `session` | Supported | Stores/removes authenticated console defaults; it does not replace migration state. |
| `logs` | Supported | Saves the current browser-session transcript; this is not a durable orchestration log archive. |
| `about` | Supported | Shows console/version information. |
| `help` | Supported | Shows local command discovery/help. |
| `clear` | Supported | Clears browser console rendering only. |
| `quit` (`exit`) | Supported | Leaves the browser console/tab; it does not cancel a server job. |

## Deliberate omissions

| Command | WebUI | Rationale |
| --- | --- | --- |
| `wizard` | Omitted | `setup` is the guided configuration flow; there is no separate in-place editor, and setup will not overwrite an existing configuration. |
| `verbosity` | Omitted | DMTX has no configurable logging subsystem for a stored level to control. |
| `explore` | Omitted | DMTX tuning is deterministic safety control, not DMT exploration policy. |
| `cache` | Omitted | DMTX has no safe cache to clear; clearing durable coordination state would be unsafe and misleading. Stage 5 has no AI cache, so no cache-invalidation command is exposed. |
| `serve` (`webui`, `gui`) | Omitted in the console | It launches the console, so offering it inside that console would start a competing surface. |

Every registry entry is `TUI: Omitted`: Stage 5 uses CLI/WebUI parity and the
SSH-forwarded WebUI model, not a terminal UI.

## Browser-runner evidence

The opt-in runner is a real Chromium-family browser test, isolated with a
temporary local working directory and controlled `/status` job. It uses no
Docker or external migration database. It creates a temporary HOME, protected
secrets directory, and encrypted SQLite profile store solely to execute a
read-only `/profile list`:

```sh
go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1
```

The latest local run on 2026-08-12 used Google Chrome and passed. The hosted
real-browser step also passed for immutable commit `b09c540`. Together they
prove authenticated launch/token exchange, discovery (every Supported registry
spelling), slash and `@` completion, parse parity, submit/progress rendering,
reload/recovery, explicit cancellation, local-recall exclusions, and PWA asset
restrictions, plus real read-only profile-list execution/rendering. It
intentionally does not execute destructive migration, profile mutation or
export/import, init, or AI-provider side effects in a browser; those remain
focused application/API evidence rather than a claim of full end-to-end
provider behavior. The committed result is published in
[PR #43](https://github.com/johndauphine/dmtx/pull/43).

## Release evidence

The published implementation has passed WebUI authentication hardening,
offline/race/static analysis,
real-browser acceptance, and the fully armed Stage 4 normal/race gates through
`test/fixtures/gate.sh --race`. Hosted workflow run
[31658869628](https://github.com/johndauphine/dmtx/actions/runs/31658869628)
passed both PR jobs for commit `b09c540`. Stage 5 redaction evidence covers
shipped sinks; future observability and notification sinks must meet the same
requirement before they are exposed. The accepted AI, cache, setup,
audit-policy, and global-control boundaries are recorded in the closeout
handoff.
