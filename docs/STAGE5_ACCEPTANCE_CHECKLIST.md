# Stage 5 acceptance and closeout checklist

## Purpose and authority

This document defines the evidence required to close Stage 5, Operator
Surfaces. It turns the decisions and proposals in
[STAGE5_DESIGN.md](STAGE5_DESIGN.md) into an acceptance checklist without
changing the normative product contract in
[RECREATE_DMT.md](RECREATE_DMT.md).

Stage 5 is complete only when every required item below is satisfied and
recorded in a closeout note. A merged foundation, a route not exposed by a real
console, or a command that remains Planned is not completion.

## Scope boundary

Stage 5 owns the operator surface around the existing application decision
seam: CLI/WebUI parity, the authenticated browser console, its delivery and
security behavior, and the deferred operator-state obligations listed here.
It presents facts decided by internal/app and does not reimplement migration
correctness in the front end.

Stage 6 and later own migration-core work such as engine routes, type mapping,
schema behavior, and transfer semantics. They remain separate workstreams.
Stage 6 changes may merge independently, but must not be cited as Stage 5
completion or weaken Stage 5 parity, redaction, or release checks.

## Required acceptance criteria

### 1. Command and application contract

- [ ] Publish the final Stage 5 command matrix and closeout handoff with the
  accepted contract disposition. The matrix itself has no Planned commands.
- [x] Review DMT domain commands against internal/contract; add a missing
  domain capability or record an approved explicit reduction. Browser-only
  shell actions may be UI chrome instead of application commands. The accepted
  Stage 5 contract is recorded in the matrix, handoff, and RECREATE_DMT.
- [x] Implement and test the application/API behavior for every supported
  command, including failure and cancellation behavior. This includes run,
  resume, status, history, validate, diagnose, preflight, analyze, profiles,
  AI actions, init, init-secrets, setup, and configuration actions if they
  remain in the registry. Guided setup covers SQLite, PostgreSQL, and SQL
  Server; PostgreSQL/SQL Server require protected credential origins, encrypted
  TLS, and source/target connection checks. PostgreSQL defaults to
  `ssl_mode=require`; SQL Server can validate the peer with a supplied CA file.
  Focused application/API tests cover command lifecycle and refusal paths,
  while the real-browser runner executes every browser-local command and
  validates shared-parser/default parity for every supported server command.
- [x] Keep cache and serve as deliberate omissions unless their documented
  rationale changes. Cache has no safe backing store in Stage 5; `serve` starts
  the interactive surface and is therefore omitted inside that surface.
- [x] Resolve the TUI decision in the normative contract: CLI/WebUI parity is
  the contract, and the TUI is deliberately omitted. Record the result in the
  closeout.
- [x] Reconcile the normative history/log-retention wording: DMTX retains
  durable migration/run history in its state DB; bounded browser-local command
  recall is separate; orchestration software owns durable full logs and
  transcripts.

### 2. Authenticated WebUI console

- [x] Replace the root placeholder with a usable authenticated browser console.
- [x] Build command discovery from the canonical registry, including slash
  autocomplete and help.
- [x] Support history, command execution, outcomes, and plain, boxed/error,
  and migration-progress output.
- [x] Use the job model for long-running work. Client disconnect must not
  cancel a migration; cancellation is explicit and observable.
- [x] Render retained/reconnectable SSE presentation progress without assuming
  contiguous event sequence numbers. This is backed by durable migration state
  at checkpoint boundaries, but the in-memory SSE event presentation itself is
  not a restart-surviving event archive.
- [x] Emit the same sanitized step/progress/terminal-outcome information during
  CLI execution; DMTX does not persist a durable log archive because
  orchestration software owns capture and retention. API/console SSE and the
  CLI stderr progress sink share sanitized application facts.
- [x] Use the authenticated completion API only. Completion remains
  root-confined, symlink-safe, regular-file/directory only, non-leaking on
  failure, and disabled rather than widened when its root cannot resolve.
- [x] Add real-browser discoverability and shared-parser parity evidence. It
  proves every Supported spelling is discoverable and that its safe parse case
  has the same request/default semantics as the CLI/API seam; controlled
  `/status` and read-only `/profile list` execute in the browser, while focused
  application/API tests cover side-effecting command semantics. Route
  existence alone is insufficient.
- [x] Exercise the visible browser-console shell controls against a loopback
  DMTX server without an external migration database or Docker. Use temporary
  local state and fixtures, and a temporary encrypted SQLite profile store only
  for read-only profile-list coverage. The interaction run covers
  suggestions/completion, history recall, parse/submit, job progress and
  outcome, explicit cancellation, reconnect/state recovery, and that
  read-only profile interaction; it does not browser-execute provider calls,
  migrations, or profile mutations.
- [x] Record the browser-capable runner and its result. Static HTML checks,
  handler tests, or API-only tests do not satisfy the prior item; if no runner
  is available, name the exact remaining browser interaction run in the
  closeout evidence.

### 3. Delivery and security

- [x] Serve only on loopback by default and document the SSH port-forward
  workflow. Another bind model requires an explicit normative change and
  security review.
- [x] Require authentication for local and forwarded sessions; test launch
  token exchange, absolute session lifetime, failed-authentication rate
  limiting, loopback Host/DNS-rebinding validation, session behavior, and
  single-instance handoff. Expired credentials rotate and a pre-expiry handoff
  cannot slide the absolute deadline.
- [x] Ship and test the PWA shell and assets for a standalone-capable
  chromeless browser experience. Real Chrome parses the manifest and its
  192px/512px PNG declarations and confirms active service-worker control; Go
  asset tests decode the actual PNG dimensions. The exact fixed Cache Storage
  entry set remains unchanged after authenticated root/API fetches, proving
  document and API responses are bypassed. A native desktop shell is not
  required. This is installability evidence, not a claim that an
  environment-specific install prompt was accepted.
- [x] Verify idle shutdown never terminates a running job and that session,
  browser-launch, and no-browser behavior work as documented.
- [x] Apply redaction tests across every shipped API response, console output,
  jobs/SSE, CLI progress, state, audit data, setup/profile output, and AI
  payload. Logs, telemetry, and notifications do not ship in Stage 5; any
  future sink inherits this requirement before exposure. Behavioral sentinel
  tests exercise CLI renderers, synchronous API output, retained job status,
  replayed SSE progress/outcomes, console/setup/history protections, state,
  audit, AI, and portable profiles. The cross-surface AST manifest is a drift
  guard for those tests, not execution evidence by itself; structured payload
  schemas remain protected by their individual wire-shape tests.
- [x] If profile export/import ships, provide encryption, secure permissions,
  round-trip tests, and redaction coverage before exposing it.

### 4. Release evidence

- [x] Run focused application, API, console, and security tests, including
  client-loss, cancellation, reconnect, completion-containment, and redaction.
- [x] Run CI-required offline, race, static-analysis, and formatting checks.
- [x] Run the armed Stage 4 normal and race live gates unchanged on the current
  integrated closeout tree, with all 16 required endpoint variables loaded so
  missing fixtures cannot silently skip. Both passed through
  `test/fixtures/gate.sh --race`; publication against an immutable commit is
  still recorded separately below.
- [x] Run the focused armed SQL Server guided-setup test against the disposable
  SQL Server 2022 TLS fixture. On 2026-08-12,
  `DMTX_STAGE4_LIVE_REQUIRED=1 go test ./internal/app -run TestMSSQLSetupLiveTLS -count=1 -v`
  verified both source and target connections and protected temporary output.
- [ ] Publish docs/STAGE5_CLOSEOUT_HANDOFF.md with the accepted command matrix,
  evidence, deliberate omissions, residual risks, and the exact Stage 6
  boundary.

## Deliberate omissions

These are not missing Stage 5 functionality while their rationale stays true:

- cache, because DMTX has no safe cache to clear;
- serve inside an interactive surface, because it starts that surface;
- a native Wails/desktop shell, which is additive to the served WebUI;
- any remote-bind flag, unless separately approved and designed.
