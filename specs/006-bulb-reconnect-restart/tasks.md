---

description: "Task list for Automatic Bulb Reconnect After Server Restart"
---

# Tasks: Automatic Bulb Reconnect After Server Restart

**Input**: Design documents from `/specs/006-bulb-reconnect-restart/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/shutdown-close-behavior.md, quickstart.md

**Tests**: Included — Constitution Principle II (NON-NEGOTIABLE) requires a regression test for every bug fix; scope is the testable mechanism only (see research.md Testability note).

**Organization**: Tasks are grouped by user story. No Setup or Foundational phase is needed: this feature adds no dependency, no new package, and no shared scaffolding — both stories build directly on the existing `internal/server` package.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2)

---

## Phase 1: User Story 1 - Bulbs come back after a routine server restart (Priority: P1) 🎯 MVP

**Goal**: On server shutdown, close each bulb's connection with a hard reset (RST) instead of a graceful close, so the bulb's existing reconnect logic has the best chance of triggering the way it does after a power cut.

**Independent Test**: Connect a bulb (or `internal/bulb/fakebulb` test double) to a running server, trigger shutdown, and verify — via `go test` for the mechanism, or manually per `quickstart.md` §2 for real firmware — that the connection is closed with a reset rather than a graceful FIN, and that the bulb's existing registry entry survives unchanged.

### Tests for User Story 1

- [X] T001 [US1] Add a failing table-driven test in `internal/server/session_test.go` that connects a client over a real TCP loopback listener, calls `session.Close()`, and asserts the client's next `Read` returns a reset-style error (e.g. wraps `syscall.ECONNRESET`) rather than `io.EOF` — per `contracts/shutdown-close-behavior.md` and `research.md`'s Testability note. Confirm it fails against the current code (graceful close still produces `io.EOF`).

### Implementation for User Story 1

- [X] T002 [US1] In `internal/server/session.go`, change `session.Close()` (lines 59-68) to set `SO_LINGER=0` on the underlying connection before calling `Close()`, when the connection is a `*net.TCPConn` (skip harmlessly for any other `net.Conn`, e.g. `fakebulb`'s in-memory pipe). Update the method's doc comment to state it now performs a hard reset. Do not add a parameter or wrapper distinguishing shutdown from takeover close — per `contracts/shutdown-close-behavior.md`, the same behavior is correct (and harmless) for both existing callers (`internal/server/session.go:158` shutdown path, `internal/server/session.go:271` takeover path).
- [X] T003 [US1] Run `go test ./internal/server/... -race -v` and confirm T001's test now passes.
- [X] T004 [US1] Run `go test ./... -race` and confirm no regression, in particular `TestReconnectRejoinsSameEntry` and `TestPowerCycleReconnectIsNotADuplicate` in `internal/server/server_test.go` still pass (spec.md FR-002, FR-005 — reconnect keeps the same identity and no state is lost purely from the close-style change).

**Checkpoint**: User Story 1 is fully implemented and independently testable — the shutdown path now hard-resets every bulb connection, provably at the mechanism level.

---

## Phase 2: User Story 2 - Operator can tell whether the fix is working (Priority: P2)

**Goal**: Confirm that an operator can already see, per bulb, whether and when it reconnected after a restart — using the existing event feed, unchanged by User Story 1's close-behavior change — so the unresolved firmware-behavior question (research.md Decision 3) can be answered by observation rather than left invisible.

**Independent Test**: Restart a server with a connected bulb and, using only the existing event feed/log (no code change from this story), confirm a `Disconnected` entry (detail `"server shutting down"`) appears at shutdown and, if the bulb reconnects, a `Connected`/`Discovered` entry appears afterward for the same device id — per `quickstart.md` §2 and `data-model.md`.

### Tests for User Story 2

- [X] T005 [P] [US2] Add a test in `internal/server/session_test.go` (or extend an existing shutdown-related test) asserting that `reportDisconnect` still classifies a shutdown-triggered close as detail `"server shutting down"` after T002's change — i.e., `SetLinger(0)` does not alter the local-side error classification in `internal/server/session.go:305-323` (`errors.Is(err, net.ErrClosed)` / `errors.Is(err, context.Canceled)` still match). This guards data-model.md's claim that the existing event vocabulary needs no change.

### Implementation for User Story 2

- [ ] T006 [US2] No production code change required (confirmed by research.md Decision 2 and data-model.md): the existing `events.Bus` `Disconnected`/`Connected`/`Discovered` sequence already satisfies FR-003 and FR-004. This task is to manually verify the sequence end-to-end per `quickstart.md` §2 against at least one real bulb, and record the observation (bulb model/firmware if known, elapsed reconnect time or "did not reconnect") in the PR description — this is the production-observability substitute for the pre-ship validation the assessment could not perform (decision.md).

**Checkpoint**: Both user stories are complete. The shutdown-close mechanism is proven at the code level (US1), and the operator has confirmed visibility into whether it actually helped for real hardware (US2).

---

## Phase 3: Polish & Cross-Cutting Concerns

- [X] T007 [P] Run `gofmt -l .` and `go vet ./...` across the repo per Constitution Principle I; fix any findings introduced by T002.
- [X] T008 Update `docs/deploying.md`'s Shutdown section (referenced in research.md and problem.md) to note that bulb connections are now closed with a hard reset on shutdown, so the existing "the server stops accepting connections... and exits" description stays accurate.
- [ ] T009 Run the full `quickstart.md` (§1 automated, §2 manual) end-to-end and record results in the PR description, including the real-hardware observation from T006.

---

## Dependencies & Execution Order

### Phase Dependencies

- **User Story 1 (Phase 1)**: No dependencies — start immediately. This is the only phase with production code changes.
- **User Story 2 (Phase 2)**: Depends on User Story 1's T002 being in place (its test in T005 verifies behavior *after* the `SetLinger(0)` change) — otherwise independent; adds no new production code.
- **Polish (Phase 3)**: Depends on both user stories being complete.

### Within Each User Story

- T001 (failing test) before T002 (implementation) before T003/T004 (verify), per constitution's regression-test requirement.
- T005 depends on T002 (asserts behavior of the changed `Close()`).
- T006 (manual verification) can run any time after T002 lands in a deployable build.

### Parallel Opportunities

- T005 and T007 are marked [P] — different files/concerns from the rest of their phase, no blocking dependency on each other.
- T001–T004 are inherently sequential (test-first, single file, single method).

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (T001–T004) — this alone ships the fix's mechanism.
2. **STOP and VALIDATE**: confirm the reset-close test passes and no existing test regresses.
3. This is deployable on its own; User Story 2 adds visibility, not behavior.

### Incremental Delivery

1. Phase 1 → mechanism shipped, testable in CI.
2. Phase 2 → confirms (via existing, unmodified event feed) that the operator can see the outcome; supplies the real-world observation the assessment deferred.
3. Phase 3 → docs and full validation pass.

## Notes

- No Setup or Foundational phase: this feature has no new dependency, package, or shared scaffolding to stand up first.
- Total production code change is one method (`session.Close()`, `internal/server/session.go`) plus one doc-comment update — everything else is test or documentation.
- Commit after each task or logical group, per repository convention.
