# DMTX operator guide

This guide covers installation, upgrade, operation, and recovery for the DMTX
5.x compatibility line. The reconstruction contract remains authoritative for
behavior; this document is the release-oriented runbook.

## Install and verify

Release artifacts contain one self-contained executable for:

- macOS on Intel and ARM64;
- Linux on x86-64 and ARM64; and
- Windows on x86-64.

Download the archive for the host and the accompanying `SHA256SUMS` file. From
the directory containing both, verify before extracting:

```sh
sha256sum --check SHA256SUMS
```

On macOS, use `shasum -a 256 <archive>` and compare the complete digest with
the matching `SHA256SUMS` entry. On Windows PowerShell, use:

```powershell
Get-FileHash .\dmtx_VERSION_windows_amd64.zip -Algorithm SHA256
```

After extraction, make the Unix binary executable if needed and verify the
embedded release version:

```sh
chmod 0755 ./dmtx
./dmtx --version
```

Do not install an artifact whose digest or reported version differs from the
release being deployed.

## Prepare configuration and credentials

Start from `dmtx init --config migration.yaml`, or use `dmtx setup` for guided
SQLite/PostgreSQL setup. Keep configuration and state on durable storage.
Place secrets in environment variables or the owner-only secrets file created
by `dmtx init-secrets`; never commit expanded credentials.

Before changing data, run:

```sh
dmtx validate --config migration.yaml
dmtx preflight --config migration.yaml
dmtx analyze --config migration.yaml
```

Preflight verifies the admitted engine version, connectivity, catalog access,
source-read and target-write prerequisites, strict-consistency prerequisites,
and destructive-operation policy before migration mutation. Do not bypass a
failed check merely to make a release proceed. A selective skip is an explicit
operator exception and remains visible in the report.

Database accounts should have only the privileges required by the configured
route. Source accounts require catalog discovery and reads. Target accounts
require catalog discovery and the DDL/DML used by the selected target mode.
Strict consistency and native bulk paths may require additional engine-specific
privileges; preflight is the authoritative check for the selected route.

## Run and observe

For a terminal or orchestrator:

```sh
dmtx run --config migration.yaml
```

For the authenticated browser console, start `dmtx serve` on loopback and use
the printed launch URL. For remote operation, forward the loopback port over
SSH. Do not expose the listener directly or weaken Host, Origin, token, cookie,
or session controls.

Capture stdout and stderr in the service manager or job orchestrator. DMTX
retains migration state and audit history, not an unbounded operator-log
archive. Configure retention outside DMTX. Metrics, OTLP traces, and Slack
summaries are best effort and cannot change migration success or failure.

## Stop, resume, and recover

SIGINT or SIGTERM requests bounded cancellation and a truthful checkpoint. A
hard process or host failure may leave the latest attempt resumable. Inspect
before acting:

```sh
dmtx status --state migration.yaml.state.db --detailed
dmtx history --state migration.yaml.state.db
dmtx diagnose --state migration.yaml.state.db
```

Resume with the same data-plane configuration and state path:

```sh
dmtx resume --config migration.yaml --state migration.yaml.state.db
```

Do not delete or edit state to force progress. DMTX rejects configuration
drift, missing ownership evidence, newer checkpoint formats, and ambiguous
legacy incomplete identities rather than guessing. If diagnosis says the run
is non-resumable, correct the underlying cause and start a new run. Use
`resume --abandon --reason "..."` only for an explicit audited decision to
supersede an otherwise resumable attempt.

For a failed required state write, lease loss, uncertain database commit, or
validation failure, preserve the state file, configuration, DMTX version,
audit file, and externally captured logs before remediation. Never retry by
manually changing target rows unless the recovery plan has independently
proved the checkpoint boundary.

## Upgrade and rollback

Back up the executable, configuration, secrets, state, and audit files before
an upgrade. Verify the new artifact and run `validate`, `preflight`, `status`,
and `diagnose` against copies or read-only access before resuming production
work.

DMTX 5.x reads supported historical state and rewrites YAML state to the
current format on the next mutation. Completed history is preserved. An
ambiguous incomplete legacy task remains non-resumable, and a binary refuses a
newer state format it cannot understand.

Rollback is safe only when the older binary is documented to understand every
state record written by the newer one. If that is not explicitly stated in the
release notes, restore the pre-upgrade state backup together with the older
binary; never point the older binary at state already mutated by the newer
release. A database target changed after the backup requires a forward recovery
assessment rather than a blind state rollback.

Stable configuration fields, commands, aliases, exit codes, and structured
fields follow Semantic Versioning. Deprecated v5 names remain accepted with a
warning and identify version 6 as their removal boundary. When old and new
names coexist, the new name wins.

## Security and incident handling

Keep configuration, secrets, profiles, state, TLS keys, and exported logs
owner-readable only. Use verified TLS on network database routes. Treat any
credential-shaped value in output as an incident: stop distribution of the
output, rotate the credential, preserve sanitized evidence, and report it by
the private process in `docs/SECURITY.md`.

Do not publish suspected vulnerabilities or production data in a public issue.
Release verification includes reachable dependency vulnerability scanning;
exceptions require a committed owner, mitigation, and short expiry.

## Supported boundaries

DMTX does not claim tables without primary keys, CDC readers, cross-engine
delete reconciliation, ClickHouse upsert or relational constraints, MySQL or
SQLite migration-scoped strict consistency, multi-user WebUI authorization,
or full-row validation mode. Unsupported capabilities fail before target
mutation. See the normative non-goals in `RECREATE_DMT.md` for the complete
list.
