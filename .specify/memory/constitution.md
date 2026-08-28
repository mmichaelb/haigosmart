<!--
Sync Impact Report
Version change: 1.0.0 → 2.0.0 (MAJOR: a principle was removed)
Modified principles: n/a
Added sections: n/a
Removed sections: Core Principle IV (Performance Requirements) — removed 2026-08-28 as not
  matching this project's approach. Performance is no longer a merge gate: no benchmark is
  required in a PR, and no CI job enforces one. Responsiveness that users actually feel is
  covered by Principle III (User Experience Consistency); correctness of concurrent code
  remains covered by Principle II (Testing Standards, race detection).
Downstream updates applied: specs/001-local-bulb-server/{plan,tasks,research,data-model}.md,
  docs/performance.md, .github/workflows/ci.yml (benchmark job removed).
Deferred/TODO placeholders: none.
-->

# haigosmart Constitution

## Core Principles

### I. Code Quality
Code MUST pass `gofmt`/`go vet` and lint checks before merge; CI enforces this, not
reviewer memory. Every exported function, type, and package MUST carry a doc comment
stating purpose (Go doc-comment convention). Cyclomatic complexity and duplication are
reviewed at PR time: extract only when a concrete second use exists, not speculatively.
Errors MUST be handled or explicitly wrapped with context (`fmt.Errorf("...: %w", err)`);
silent discards (`_ = err`) require an inline comment justifying why.
**Rationale**: Consistent, reviewable code is cheaper to maintain than code that merely
works; automated gates remove subjective back-and-forth in review.

### II. Testing Standards (NON-NEGOTIABLE)
Every new package or exported behavior ships with table-driven unit tests covering the
happy path and its edge cases. Bug fixes MUST include a regression test that fails
without the fix and passes with it. Integration tests are REQUIRED for any change
crossing a service boundary, external API, or persistence layer. CI MUST run `go test
./...` with race detection (`-race`) on every PR; a red build blocks merge, no exceptions
via force-merge.
**Rationale**: Tests are the only durable proof a behavior works; skipping them shifts
the cost to whoever hits the bug in production.

### III. User Experience Consistency
User-facing surfaces (CLI output, API responses, error messages) MUST follow one
consistent vocabulary, format, and error-shape across the whole project — a new command
or endpoint reuses existing patterns rather than inventing its own. Breaking changes to
any user-facing contract (CLI flags, API schema, output format) REQUIRE a documented
migration note and a version bump. Error messages MUST state what failed and, where
possible, how to fix it — not just an internal error code.
**Rationale**: Inconsistent surfaces force users to relearn the tool feature by feature,
eroding trust and increasing support burden.

## Additional Constraints

Target Go version is pinned in `go.mod` (currently 1.27); do not introduce dependencies
requiring a newer toolchain without updating `go.mod` explicitly. New third-party
dependencies require a one-line justification in the PR description (what stdlib/existing
dependency was insufficient).

## Development Workflow

All changes land via PR review; at least one other reviewer MUST approve before merge.
Reviewers verify compliance with the four principles above as part of review, not as a
separate gate. CI (build, vet, lint, race-enabled tests) MUST be green before merge.

## Governance

This constitution supersedes ad hoc conventions. Amendments require a PR to this file
describing the change and rationale; merge approval doubles as amendment approval.
Version bumps follow semantic versioning: MAJOR for removed/redefined principles, MINOR
for new principles or materially expanded guidance, PATCH for wording/clarity fixes.
Every PR description SHOULD note any deliberate deviation from a principle and why.

**Version**: 2.0.0 | **Ratified**: 2026-08-25 | **Last Amended**: 2026-08-28
