# Stage 5 WebUI operations

## Launch and connect

Start the loopback-only console with:

```sh
dmtx serve --config migration.yaml
```

It listens only on localhost. The printed one-time URL contains a launch token;
opening it exchanges that token for an HttpOnly, SameSite=Strict session cookie
and redirects to `/`, removing the token from the address bar. A token is
single-use: if it is consumed or expires, run `dmtx serve` again (or use its
single-instance handoff). The browser session has an eight-hour absolute
lifetime. A handoff during that window does not extend it; after expiry, the
next valid one-time launch rotates the session credential.

Use `--no-browser` on a jump host or where opening a local browser is unwanted:

```sh
dmtx serve --no-browser --config migration.yaml
```

Then forward the port and open the printed URL locally:

```sh
ssh -L 8484:localhost:8484 dbhost
```

The local and remote ports may differ, for example
`ssh -L 8848:localhost:8484 dbhost`. In that case, replace the printed URL's
port with the local forwarded port (`8848`) while preserving its one-time token.

Do not add an unaudited remote bind. Loopback plus SSH forwarding and the
session authentication are the supported remote-operator model. The server
accepts a literal `localhost`, `127.0.0.1`, or `[::1]` `Host` with any valid
explicit port, which permits an SSH forward's local and remote ports to differ.
It rejects arbitrary DNS names, non-loopback addresses, missing ports, and
malformed ports before routing. Repeated failed credential attempts from one
socket IP are temporarily rate-limited. Forwarded-address headers are not
trusted for either decision.

## Server lifetime and jobs

`--idle-timeout` defaults to 30 minutes; `--idle-timeout 0` disables idle
shutdown. A running job counts as activity, so the server will not terminate a
migration because its browser tab disconnected. The idle clock restarts after
work finishes.

Browser disconnect, reload, and network loss do not cancel a job. The console
reconnects to its SSE presentation stream and recovers current retained job
state. Sequence IDs can have gaps after event trimming. Progress reflects
durable migration checkpoints, but SSE events are retained presentation state,
not a durable restart-surviving event archive. Cancel only with the explicit
console control; cancellation is observable through the job's terminal result.

`/history` reports durable migration/run history from DMTX's selected state
database. The Up-arrow command recall is a separate, bounded browser-local
list. `/logs` saves a browser-session transcript; durable full logs and
transcripts should be captured and retained by the orchestration system.

## Console safety

`@` completion is authenticated and restricted to the configured completion
root. It resolves symlinks, returns only regular files/directories under that
root, and fails closed if the root cannot be resolved. Completion is not a
general server filesystem browser.

Sensitive data must not be entered into normal command history. Setup answers
are excluded from browser recall. Portable profile export/import requires an
encrypted format and a private passphrase file; never put a passphrase in a URL,
shell argument, or transcript.

`/setup [postgres|sqlserver] [CONFIG | @CONFIG | --config CONFIG | --profile NAME]`
starts a PostgreSQL or SQL Server source-to-target configuration flow; omitting
the engine starts SQLite setup. `mssql`, `sqlserver`, and `sql-server` are
accepted SQL Server spellings. PostgreSQL and SQL Server setup store passwords
only in protected file origins, require encrypted TLS, verify source and target
connections before writing the configuration, and refuse to overwrite an
existing configuration. PostgreSQL defaults to `ssl_mode=require`; SQL Server
can validate the peer with a supplied CA file. Setup does not run analysis: run `preflight`,
`analyze`, and a dry run afterward.

`/profile export NAME --passphrase-file PATH` writes `NAME.dmtx-profile.json`
by default (using a safe derived filename for unusual names), never
`config.yaml`. The default is an encrypted portable-profile envelope, not a
migration configuration. Supplying `OUTPUT` deliberately replaces an existing
regular file atomically; symlinks, non-regular destinations, and passphrase-file
aliases are refused.

The PWA shell can be installed through the browser's normal “Install app”
action. This creates a chromeless app-style window; it does not create a native
desktop shell or change the loopback/authentication model. The automated
browser evidence checks the install prerequisites and cache isolation; it does
not claim that an environment-specific install prompt was clicked.

## Operator verification

On a workstation with Chrome, Chromium, or Edge, run the real-browser console
acceptance check:

```sh
go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1
```

Set `DMTX_BROWSER_BINARY` to an explicit executable when automatic discovery
does not find one. The fixture is local and controlled: it needs no Docker or
external migration database. It creates a temporary protected profile store,
seeds one encrypted profile, and executes only read-only `/profile list` against
it. It never uses operator profile data and does not browser-execute profile
save/delete/export/import.

To verify the SQL Server guided setup against the disposable TLS fixture, start
the repository fixtures, source their environment, then run:

```sh
source test/fixtures/env.sh
DMTX_STAGE4_LIVE_REQUIRED=1 go test ./internal/app -run TestMSSQLSetupLiveTLS -count=1 -v
```

The test verifies both SQL Server endpoints and writes only a temporary
configuration plus owner-only password-file origins; it does not modify either
fixture database.
