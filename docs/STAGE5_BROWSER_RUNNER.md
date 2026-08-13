# Stage 5 browser-console runner

The browser acceptance runner is an opt-in Go test that drives a real local
Chromium-family browser through the authenticated loopback console. It uses a
temporary working directory and test job seam, so it needs neither Docker nor
an external migration database.

Run it on a workstation with Chrome, Chromium, or Edge installed:

```sh
go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1
```

Set `DMTX_BROWSER_BINARY` when automatic browser discovery does not find the
desired binary:

```sh
DMTX_BROWSER_BINARY=/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
  go test -tags=browser ./internal/api -run TestBrowserConsoleControls -count=1
```

The runner authenticates through the one-time launch URL, then verifies command
discovery (including every Supported registry spelling), slash and `@` completion,
parse-and-submit, browser-local history recall, setup-answer exclusion, progress
rendering, explicit cancellation, reload/recovery, and PWA manifest/service-worker
restrictions. Chrome must report an active root-scoped service-worker registration;
the runner then asks Chrome for its parsed linked-manifest representation and
also fetches the authenticated manifest response, checking standalone display and
the declared 192px and 512px PNG install icons. The Go UI asset tests decode the
actual PNG dimensions. The browser runner also asserts the exact fixed-asset
entry list in Cache Storage, performs authenticated root and API fetches, and
proves that exact cache entry set remains unchanged. This proves install
prerequisites, not a browser-specific install-prompt click. Its controlled
`/status` job emits public progress and waits for cancellation; it never invokes
a migration adapter. A separate real `/profile list` browser job reads one
encrypted profile
seeded into a temporary HOME, secrets directory, and SQLite store; it never
touches operator profile data or performs a profile mutation.

The runner proves the browser can discover every currently Supported command.
For every server-backed Supported command and alias it sends a safe, valid line
through the authenticated browser `/parse` seam and compares the returned
request/outcome with `app.ParseRequest` plus command defaults. That comparison
is parse parity for commands whose execution could migrate, initialize, mutate
profiles, or contact an AI provider. The controlled `/status` job separately
proves submit/progress/reconnect/cancel behavior. Browser-owned help is
exercised locally and setup uses its real UI state machine. A real read-only
`/profile list` executes and renders from the temporary encrypted store;
profile save/delete/export/import remain outside this browser fixture.

The latest local run on 2026-08-12 passed with Google Chrome. The required
hosted browser step also passed for immutable implementation commit `b09c540`
in [workflow run 31658869628](https://github.com/johndauphine/dmtx/actions/runs/31658869628).

This is intentionally a real-browser test, not a replacement for API and
handler tests. It is skipped unless built with `-tags=browser`. On a developer
workstation, absence of a browser skips the test. In CI (`CI` is set), absence
of Chrome, Chromium, or Edge is a failure before browser invocation; install a
browser or set `DMTX_BROWSER_BINARY` to its executable.
