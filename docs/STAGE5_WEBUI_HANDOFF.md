# Stage 5 WebUI handoff

This is an implementation handoff, not Stage 5 completion evidence.

## Direction

Recreate DMT's operational TUI workflow in the DMTX WebUI: a console REPL,
slash-command grammar, aliases, history, completion, prompts, progress, and
cancellation. Add Codex-style command discovery with a searchable palette,
descriptions, categories, keyboard focus, and accessible status.

The target is a modern web console, not a pixel clone. Preserve operational
meaning and safety while adapting terminal affordances to a browser.

## Branch context

- Branch: `codex/webui-command-palette-metadata`
- Base: `main` at `c319dec`
- Current implementation commit before this document: `7b901dc`
- No prior Stage 5 branch commits are included.

## Groundwork already present

The current commit adds canonical `Description` and `Category` metadata to
the command registry and exposes both through `GET /api/v1/commands`.
Existing `TUI` and `WebUI` dispositions are unchanged: planned and omitted
commands must remain visibly non-executable until their acceptance is complete.

## Server boundaries to retain

The browser is a renderer and interaction client. It must not reimplement
application decisions or call migration internals.

- `POST /api/v1/parse` turns the typed line into the resolved `app.Request`
  or the same non-dispatched `app.Outcome` the CLI receives.
- `GET /api/v1/commands` is the canonical palette/help source, including
  aliases, descriptions, categories, and WebUI disposition.
- `GET /api/v1/complete` supplies root-confined `@path` completion.
- `POST /api/v1/jobs` starts work; `GET /api/v1/jobs` and
  `GET /api/v1/jobs/{id}` recover server-owned state.
- `GET /api/v1/jobs/{id}/events` is the SSE stream for started, progress, and
  finished events. Preserve sequence IDs and reconnect with `?from=N`.
- `POST /api/v1/jobs/{id}/cancel` requests cooperative cancellation.
- `POST /api/v1/execute` remains the synchronous `app.Request` seam.
- Session and setup flows retain their existing endpoints and state machines;
  masked setup input must never enter browser history, transcript, or storage.

## Ordered implementation slices

1. **Console shell.** Add slash-first normalization and aliases; browser-only
   `/help`, `/clear`, and `/quit`; palette filtering by canonical metadata;
   keyboard completion, history, focus, and accessible status.
2. **Transcript renderer.** Use bounded per-command blocks and
   `textContent`-only rendering for plain, boxed, and progress outcomes.
3. **Job lifecycle.** Add per-job output, cancel controls, SSE resume, reload
   recovery, and explicit finished, expired, unauthorized, and server-gone
   states without retrying a job-start POST.
4. **Operator panels.** Add setup/config/profile panels only through their
   existing API seams, preserving masking and the support matrix.
5. **Advisory and acceptance.** Integrate AI advisory only when its registry
   disposition and end-to-end acceptance permit it; then perform real Edge
   validation and update the Stage 5 checklist.

## Non-goals and guardrails

- Do not move command, safety, timeout, or provider decisions into browser
  JavaScript; the server and `app.Request`/`app.Outcome` remain authoritative.
- Do not use `innerHTML`, render raw unbounded data, show raw database rows, or
  send SQL through the console.
- Do not place secrets in command history, transcript blocks, local storage,
  URLs, logs, screenshots, or document examples.
- Do not add cloud calls, row sampling, auto-apply, or durable AI archives.
- Do not promote a `Planned` or `Omitted` registry entry to supported before
  its API, WebUI, and browser acceptance are complete.

## Definition of done

The WebUI preserves DMT's operational workflow while remaining recognizably a
modern web console: slash commands and aliases work through the server parser;
palette discovery, completion, history, prompts, output, progress, cancel, and
reconnect are keyboard-usable and accessible; and all supported commands have
an accepted API-backed path. Unsupported commands are labeled honestly.

## Real Edge validation

Automated Go tests are necessary but do not prove browser fidelity. Before
Stage 5 closeout, use a real Microsoft Edge session against the authenticated
loopback server and record the result:

- discover and filter commands, insert an alias, use `/help`, `/clear`, and
  browser-only `/quit`;
- verify keyboard focus, Tab/Enter/Escape, ArrowUp/ArrowDown history, command
  completion, `@path` completion, mobile layout, and screen-reader status;
- submit quoted arguments through `/api/v1/parse`, inspect plain/boxed/progress
  blocks, cancel a safe running job, reload, and confirm SSE recovery;
- confirm planned/omitted commands are visibly non-executable and that no
  secret, SQL, raw row data, or unbounded payload appears in the UI, URL,
  browser storage, console, or network response used by the test.

## Mac continuation

1. Fetch and switch to `codex/webui-command-palette-metadata`; verify the branch
   contains only the metadata commit plus this document and is based on `main`.
2. Read this handoff, `docs/STAGE5_DESIGN.md`, and `docs/CONSOLE_DESIGN.md`;
   implement the slices in order with small reviewable commits.
3. Keep the browser on the API seams above, preserve existing dispositions, and
   add focused tests before browser acceptance.
4. Run formatting and the full Go suite, serve only through the existing
   authenticated loopback flow, complete the real Edge checklist, then update
   Stage 5 acceptance evidence before requesting review.
