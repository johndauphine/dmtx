# Security policy

## Reporting

Do not open a public issue for a suspected vulnerability or include secrets,
credentials, database contents, or exploit details in ordinary logs. Report it
privately to the repository owner through GitHub's private vulnerability
reporting facility. Include the affected version, impact, and the smallest
reproduction that does not contain production data.

## Dependency scanning

Every verification run executes the official Go vulnerability scanner against
all reachable packages. The scanner version is pinned in
`.github/workflows/verify.yml`; upgrades are reviewed changes rather than an
implicit change in release policy.

A reachable vulnerability fails the gate. An exception is allowed only when
all of the following are committed together:

1. the vulnerability identifier and affected dependency;
2. evidence that the vulnerable path is unreachable or a documented temporary
   mitigation;
3. an owner and an expiry date no more than 30 days away; and
4. a regression test when the mitigation is enforceable in code.

Expired exceptions fail the release. Module-only findings that are not
reachable are recorded for dependency maintenance but do not override the
scanner's reachability-based exit result.

## Supported versions

Until the first production release is published, only the current main branch
receives security fixes. The Stage 6 closeout and release notes must replace
this statement with the supported release line and upgrade guidance.
