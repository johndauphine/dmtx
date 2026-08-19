# Stage 5 command matrix

## Authority and final disposition

This is the final Stage 5 CLI/WebUI command-contract matrix. The TUI is omitted
for every command. Every registered command is either **Supported** in the
WebUI or **Omitted** with an operator-facing reason; no command remains Planned.
Supported domain commands use the shared `internal/app` request/outcome seam.
Browser-shell commands are UI chrome and are not manufactured as application
jobs.

The scope amendments in `STAGE5_DESIGN.md` were approved on 2026-08-16 and are
normative through the corresponding changes in `RECREATE_DMT.md`.

## Domain and configuration commands

| Command | CLI disposition | WebUI disposition | Acceptance evidence / boundary |
| --- | --- | --- | --- |
| `run` | **Supported** | **Supported** | Shared parser/dispatch; sanitized progress; one active migration slot; real SQLite Edge run, SSE recovery, reload, and explicit cancel. |
| `resume` | **Supported** | **Supported** | Shared parser/dispatch and origin/state rules; browser invocation and focused resume tests. |
| `status` | **Supported** | **Supported** | State report through shared outcome; browser invocation. |
| `history` | **Supported** | **Supported** | Reads migration run records from the state backend. This is distinct from browser-local input recall and from an external operator log archive. |
| `validate` | **Supported** | **Supported** | Deterministic validation through the shared seam; no AI augmentation. |
| `diagnose` | **Supported** | **Supported** | Read-only deterministic failure/state facts through the shared seam. |
| `preflight` (`health-check`) | **Supported** | **Supported** | Alias resolves canonically; registry discovery and Edge alias completion. |
| `analyze` | **Supported** | **Supported** | Offline deterministic effective-plan report; no source sampling, auto-apply, or AI explanation. |
| `profile save/list/delete` | **Supported** | **Supported** | Encrypted profile store, permissions/redaction tests, and real Edge save/list/delete in a temporary home. |
| `ai config-review` | **Supported** | **Supported** | Optional display-only advisory from sanitized facts; strict schema, input/output credential screening, timeout, and unavailable-provider behavior. No core action requires AI. |
| `init` | **Supported** | **Supported** | Template generation and overwrite protection through the shared seam; browser invocation. |
| `init-secrets` | **Supported** | **Supported** | Protected template creation; browser invocation; excluded from transcript recall. |
| `setup` | CLI directs to `dmtx serve` | **Supported** | WebUI-owned SQLite/PostgreSQL conversation. Edge proves SQLite write, cancel/no-write, overwrite refusal, and masked generic PostgreSQL connection failure. |
| `config` | **Supported** | **Supported** | Sanitized resolved-configuration report through the shared seam; browser invocation. |

## Browser-shell commands

| Command | WebUI disposition | Rationale / evidence |
| --- | --- | --- |
| `session` | **Supported** | Authenticated get/set/clear of a closed set of project defaults; browser-local shell action backed by the session API. |
| `logs` | **Supported** | Downloads the bounded, text-only console transcript; it is not a durable server archive. |
| `about` | **Supported** | Version/features rendered by the authenticated console. |
| `help` | **Supported** | Registry/application help with slash discovery. |
| `clear` | **Supported** | Clears the bounded browser transcript. |
| `quit` (`exit`) | **Supported** | Closes the console page where permitted and otherwise explains manual close; isolated Edge assertion. |

## Deliberate omissions and deferred sub-actions

| Command or sub-action | Disposition | Approved reason |
| --- | --- | --- |
| TUI | **Omitted** | CLI plus the authenticated, loopback-only WebUI are the supported surfaces. Remote operators use SSH forwarding. |
| `profile export` / import | **Deferred and refused** | Plaintext export is unsafe. Portable export and import must ship together with passphrase protection and round-trip/redaction evidence. Save/list/delete remain supported. |
| AI runbook, eval, and archive actions | **Omitted** | Stage 5 supports only bounded, display-only `config-review`; deterministic migration behavior cannot depend on AI. |
| `wizard` | **Omitted** | Guided setup exists, but DMTX has no separate in-place editor and setup never overwrites a configuration. |
| `verbosity` | **Omitted** | DMTX has no runtime log-level subsystem for this shell command to control. |
| `explore` | **Omitted** | Runtime tuning is deterministic safety control, not DMT exploratory policy. |
| `cache` | **Omitted** | DMTX has no type-mapping cache to clear; lease coordination state is durable. |
| `serve` (`webui`, `gui`) inside the console | **Omitted** | It launches the WebUI, so exposing it from inside that UI would start a competing surface. |
| Global Prometheus/OTLP/Slack CLI flags | **Omitted** | Approved YAML-only sink configuration; delivery settings do not alter migration or resume hashes. |
| Durable DMTX operator-log archive and retention setting | **Omitted** | External orchestration owns log capture/retention. DMTX still retains migration state/history and bounded browser-local command recall. |

## Cross-surface acceptance

- `internal/contract` rejects missing dispositions and aliases resolve through
  the same registry.
- Direct JSON jobs admit only WebUI-supported domain commands and finite action
  vocabularies; browser-shell commands remain on their dedicated routes.
- The armed Edge test discovers every WebUI-supported command and exercises the
  shipped parse/job/application seam. Only its second `run` is held open by the
  test fixture to make reload/cancellation deterministic; the first migration
  is real SQLite work.
- Browser recall is bounded and excludes setup answers, `init-secrets`, AI
  request text, abandonment reasons, and credential-shaped input.
- Operational sinks are best-effort and cannot change migration decisions.

Overall Stage 5 completion additionally depends on the release evidence in
`STAGE5_ACCEPTANCE_CHECKLIST.md` and `STAGE5_CLOSEOUT_HANDOFF.md`; this matrix
does not waive race, lint, armed-live, or delivery gates.
