# Agent routing

Minimize model usage and cost while preserving the full acceptance bar. Route
each task to the least expensive capable model, avoid duplicate reviews, and
stop delegated work once the requested evidence is collected.

Use Terra for substantive implementation work, especially changes that span
multiple files or require architectural judgment. Give Terra concrete scope,
acceptance criteria, and owned files before it starts.

Use Luna for bounded, low-risk tasks, focused verification, test execution,
evidence collection, and triage. Prefer Luna for mechanical documentation or
test-only changes that do not require product or architectural judgment.

The coordinator or supervisor plans and decomposes the work, retains scope
control, coordinates agents, independently validates the integrated result,
and makes final completion decisions. The coordinator should not duplicate
implementation or test work already assigned to Terra or Luna.

Do not silently substitute a more expensive frontier model when a requested
route is unavailable. Record the unavailable route and use the lowest-cost
available fallback only with coordinator approval.

Model selection never reduces the applicable checklist, safety constraints, or
required verification. Every change must meet the same documented acceptance
criteria and evidence standard.
