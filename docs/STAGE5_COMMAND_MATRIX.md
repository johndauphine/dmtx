# Stage 5 command matrix

## Authority

This is the authoritative working matrix for Stage 5 command-contract closeout.
It freezes the current surface, states what evidence is required to change a
disposition, and records the decisions still needed. It does not change
application behavior, the normative contract, or Stage 6 scope.

At formal closeout, every substantive registered command must be **Supported**
in the WebUI or **Omitted** with an approved operator-facing rationale. A
substantive command may not remain **Planned**. An underlying route or handler
is not support on its own: the command must be discoverable and proven to use
the same application decision path as the CLI/API seam.

## Registered command freeze

| Command | Current CLI evidence | Current WebUI evidence | Closeout target and exact acceptance | Dependency / risk |
| --- | --- | --- | --- | --- |
| `run` | Supported: parser/dispatch plus `Run` wiring to a sanitized JSON progress sink on stderr; final outcome/payload remains on stdout. | Supported: registry, authenticated parse, and job flow. | **Supported:** equivalent outcomes, cancellation, progress/reconnect, and browser discovery/invocation proof. | Browser must not decide migration behavior. |
| `resume` | Supported: parser enforces state/origin rules; shared dispatch. | Supported: registry and job console. | **Supported:** same origin validation, cancellation/reconnect, and browser proof. | Preserve explicit `--state` for profile origins. |
| `status` | Supported: state reporting dispatch. | Supported: registry and console job flow. | **Supported:** discoverable invocation and equivalent outcome/error proof. | Paths/defaults remain authenticated and redacted. |
| `history` | Supported: state reporting dispatch. | Supported: registry; input recall is separate UI chrome. | **Supported:** distinguish run history from input recall and prove the application outcome. | Durable operational logging/history is intentionally owned by the external orchestrator; DMTX history is only browser-local recall. |
| `validate` | Supported: parser/dispatch and config-origin tests. | Supported: registry and parse/job path. | **Supported:** CLI/API/browser error and profile-origin parity. | Failure paths must not leak secrets. |
| `diagnose` | Supported: durable-state diagnosis dispatch. | Supported: registry and parse/job path. | **Supported:** discoverable invocation and offline/error parity. | Must remain read-only and app-owned. |
| `preflight` (`health-check`) | Supported: alias resolves canonically. | Supported: registry exposes alias. | **Supported:** canonical-and-alias discovery and equivalent outcomes. | Preserve alias parity and application ownership. |
| `analyze` | Supported: offline effective-plan report. | Supported: registry and parse/job path. | **Supported:** browser parity for report and failures. | Source-derived tuning advice is an intentional reduction. |
| `profile` | Supported: encrypted save/list/delete and `--profile` origin loading. `export` refuses. | Supported: registry and parse/job path; prior Edge pass recorded interaction. | **Supported** for save/list/delete and origin selection: prove browser/API parity, encryption, permissions, and redaction. | Export/import is a deferred sub-action, not completion evidence. |
| `init` | Supported: parser and app dispatch. | Supported: registry and parse/job path. | **Supported:** discoverable invocation plus generation/error parity. | Preserve overwrite safeguards. |
| `init-secrets` | Supported: parser plus protected creation/loader tests. | Supported: registry and parse/job path. | **Supported:** browser invocation, permissions, redaction, and refusal-path evidence. | No secret may reach console history, output, or job state. |
| `setup` | **Planned:** no CLI application command. | **Planned in registry**, despite authenticated SQLite/PostgreSQL guided flows and focused API tests. | **Direction: Support, not accepted yet.** Change registry only after browser proof of SQLite/PostgreSQL completion, cancellation/no write, overwrite protection, timeout, protected password origins, and redaction. | Routes alone are insufficient; masked password input may use only setup flow. |
| `ai` | **Planned:** no parser or app dispatch. | **Planned:** no supported behavior. | **Decision required:** implement an explicit advisory contract and mark Supported, or approve an operator-facing Omitted rationale and amend scope. It cannot remain Planned. | High policy, cost, configuration, and redaction risk; core operations must not require AI. |
| `config` | Supported: report dispatch with config/profile origins. | Supported: registry and parse/job path. | **Supported:** browser discovery and equivalent output/error proof. | Report origins without protected values. |
| `cache` | Omitted. | Omitted: registry says DMTX has no safe cache to clear. | **Omitted:** retain this rationale unless a real cache is introduced. | An empty clear action would mislead operators. |
| `serve` (`webui`, `gui`) | Supported as launcher, outside shared app-command seam. | Omitted because it starts the WebUI. | **Omitted inside interactive surfaces:** retain launch/auth/loopback/handoff/idle/no-browser evidence. | Console must not start a competing console; remote bind is out of scope. |

## Related closeout items

| Item | Current evidence | Required closeout disposition | Decision / dependency |
| --- | --- | --- | --- |
| `profile export` / import | Parser accepts export, but application deliberately refuses until passphrase-protected export/import ship together. | **Deferred and not exposed as Supported.** If shipped later, both directions require encryption, permissions, round-trip, and redaction proof. | No new decision is needed to keep it unavailable. |
| Sanitized progress/outcome stream | API jobs expose started/progress/finished SSE with replay and one-hour in-memory job retention; console renders progress and terminal outcomes. CLI `Run` wires `ExecuteWithProgress` to the sanitized stderr JSON sink; final outcomes remain on stdout. | **Supported stream contract:** CLI and API/console emit sanitized progress records while work runs, with terminal outcomes through the shared seam. | No DMTX durable log archive; orchestration software such as Airflow captures/persists the stream. Progress payloads contain command/table/status facts only; credentials/secrets are excluded. |
| Browser-local command recall | The console stores a bounded, de-duplicated history in each browser profile and never sends it to DMTX. Durable operational logging belongs to orchestration software such as Airflow. | **Supported as local recall:** top-level submitted commands only, Arrow-Up recall only (Arrow-Down is reserved for native completion), blank restoration, setup-answer/secret exclusion, and bounded storage tests. | This is not a server archive; no migration data, SQL, credentials, secrets, paths from wizard answers, or full logs are retained. |
| TUI omission | Registry omits TUI; normative criteria now specify CLI/WebUI parity. | **Omitted and recorded in closeout.** | Closeout must state the SSH-forwarded WebUI model. |
| PWA and browser parity | Console exists; no manifest, service worker, installable assets, or in-repo browser parity runner was found. | Required evidence for every WebUI Supported row, not a command disposition. | Blocks formal acceptance until browser proof is recorded. |

## Closeout gate

The matrix is final only when:

1. `ai` and `setup` have recorded final dispositions and no substantive command is Planned.
2. Browser-local command recall is tested for top-level-only retention, Arrow-Up recall only, blank restoration, bounded storage, and setup/secret exclusion.
2. Every Supported row has application/API coverage and browser
   discoverability/invocation proof through the same decision seam.
3. Deliberate omissions and deferred export/import appear in
   `STAGE5_CLOSEOUT_HANDOFF.md` with rationale and residual risk.
4. Delivery, redaction, CI/race/static-analysis, and armed Stage 4 evidence
   required by `STAGE5_ACCEPTANCE_CHECKLIST.md` are recorded separately. This
   matrix does not waive them.
