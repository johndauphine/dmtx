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

- [x] Publish the final Stage 5 command matrix. Every substantive registered
  command is Supported in the WebUI or deliberately Omitted with an approved
  operator-facing reason. Planned is not permitted at closeout.
- [x] Review DMT domain commands against internal/contract; add a missing
  domain capability or record an approved explicit reduction. Browser-only
  shell actions may be UI chrome instead of application commands.
- [x] Implement and test the application/API behavior for every supported
  command, including failure and cancellation behavior. This includes run,
  resume, status, history, validate, diagnose, preflight, analyze, profiles,
  AI actions, init, init-secrets, setup, and configuration actions if they
  remain in the registry.
- [x] Keep cache and serve as deliberate omissions unless their documented
  rationale changes.
- [x] Resolve the TUI decision in the normative contract: amend parity to
  CLI/WebUI or implement the TUI. Record the result in the closeout.
- [x] Record that durable operator log/archive history belongs to orchestration
  software; DMTX retains migration-state history plus bounded browser-local
  command recall (see STAGE5_COMMAND_MATRIX.md) through an explicit approved
  contract change.

### 2. Authenticated WebUI console

- [x] Replace the root placeholder with a usable authenticated browser console.
- [x] Build command discovery from the canonical registry, including slash
  autocomplete and help.
- [x] Support history, command execution, outcomes, and plain, boxed/error,
  and migration-progress output.
- [x] Use the job model for long-running work. Client disconnect must not
  cancel a migration; cancellation is explicit and observable.
- [x] Render durable application progress and reconnect over SSE without
  assuming contiguous event sequence numbers.
- [x] Emit the same sanitized step/progress/terminal-outcome information during CLI execution; DMTX must not persist a durable log archive because orchestration software owns capture and retention. Current API/console SSE and CLI stderr progress sink are present; focused renderer/wiring tests cover the stream shape and redaction.
- [x] Use the authenticated completion API only. Completion remains
  root-confined, symlink-safe, regular-file/directory only, non-leaking on
  failure, and disabled rather than widened when its root cannot resolve.
- [x] Add an end-to-end WebUI parity/discoverability test. It proves every
  supported command is discoverable and invokes the same application semantics
  as the CLI/API seam; route existence alone is insufficient.
- [x] Exercise every visible browser-console control against a loopback DMTX
  server without an external migration database or Docker. Use temporary local
  state and fixtures, and a temporary encrypted SQLite profile store only for
  profile coverage. The interaction run must cover suggestions/completion,
  history recall, parse/submit, job progress and outcome, explicit
  cancellation, reconnect/state recovery, and profile interactions.
- [x] Record the browser-capable runner and its result. Static HTML checks,
  handler tests, or API-only tests do not satisfy the prior item; if no runner
  is available, name the exact remaining browser interaction run in the
  closeout evidence.

### 3. Delivery and security

- [x] Serve only on loopback by default and document the SSH port-forward
  workflow. Another bind model requires an explicit normative change and
  security review.
- [x] Require authentication for local and forwarded sessions; test launch
  token exchange, session behavior, and single-instance handoff.
- [x] Ship and test the PWA shell and assets for an installable chromeless
  browser experience. A native desktop shell is not required.
- [x] Verify idle shutdown never terminates a running job and that session,
  browser-launch, and no-browser behavior work as documented.
- [x] Apply redaction tests across API responses, console output, jobs/SSE,
  logs, state, audit data, notifications, and AI payloads.
- [x] Profile export/import does not ship: plaintext export is refused and the
  approved deferred action is absent from help. Encryption, round-trip, and
  redaction evidence remain mandatory before it can be exposed later.

### 4. Release evidence

- [x] Run focused application, API, console, and security tests, including
  client-loss, cancellation, reconnect, completion-containment, and redaction.
- [ ] Run CI-required offline, race, static-analysis, and formatting checks.
- [ ] Run the armed Stage 4 live gate unchanged.
- [ ] Publish docs/STAGE5_CLOSEOUT_HANDOFF.md with the accepted command matrix,
  evidence, deliberate omissions, residual risks, and the exact Stage 6
  boundary.

## Deliberate omissions

These are not missing Stage 5 functionality while their rationale stays true:

- cache, because DMTX has no safe cache to clear;
- serve inside an interactive surface, because it starts that surface;
- a native Wails/desktop shell, which is additive to the served WebUI;
- any remote-bind flag, unless separately approved and designed.
