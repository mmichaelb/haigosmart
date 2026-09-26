# Phase 0 Research: Automatic Bulb Reconnect After Server Restart

No `NEEDS CLARIFICATION` markers remain in Technical Context — all defaults were derived directly from the existing codebase (`go.mod`, existing `internal/server` package, existing `events` feed). This document instead resolves the technical approach questions the assessment (`.specify/assessments/automatic-reconnect-on-server-restart/`) left open, plus the one genuine unknown the assessment could not close.

## Decision 1: How to force a hard TCP close in Go

- **Decision**: Call `SetLinger(0)` on the underlying `*net.TCPConn` immediately before `Close()`, inside `session.Close()`.
- **Rationale**: `SO_LINGER` with a zero timeout is the standard, portable way to make a TCP close send RST instead of the default graceful FIN/ACK exchange, across Go's `net` package on Linux, macOS, and Windows alike — matching FR-006 (all platforms). It requires no new dependency (stdlib only) and is a one-line change at the single call site that already owns the close (`session.Close`, `internal/server/session.go:59-68`).
- **Alternatives considered**:
  - *Sending an explicit protocol-level "goodbye" message before closing* — rejected: the bulb-facing protocol (a vendor MQTT variant) has no documented client-directed disconnect notice the firmware is known to act on; inventing one is unverifiable against closed-source firmware and adds protocol surface for no confirmed benefit.
  - *Not closing sessions at all on shutdown, relying on the OS to tear down sockets on process exit* — rejected: process exit already produces the same graceful close from the peer's point of view on most platforms (kernel still sends FIN), so it wouldn't change bulb-observable behavior, and it removes the deterministic timing the current `context.AfterFunc` shutdown path provides.
  - *Lowering the keep-alive/idle timeout so the bulb notices faster* — rejected: this changes when the *server* notices a dead bulb, not when the *bulb* notices a dead server; doesn't address the reported symptom at all (`research.md` in the assessment already distinguishes these).

## Decision 2: How to make reconnect status operator-visible (FR-003/FR-004)

- **Decision**: No new entity or event kind. Reuse the existing `events.Bus` feed: a `Disconnected` event with detail `"server shutting down"` already fires per bulb on shutdown (`internal/server/session.go:305-323`), and a subsequent `Connected`/`Discovered` event fires per bulb on reconnect (`internal/server/session.go:280-284`). An operator reading the event feed for a device id can already see both halves of the story in order.
- **Rationale**: Constitution Principle I says "extract only when a concrete second use exists, not speculatively" and Principle III requires reusing existing vocabulary rather than inventing new surfaces. The existing two-event sequence already satisfies "operator can tell, per bulb, whether and when it reconnected" without adding a field, a table, or a new `Kind`.
- **Alternatives considered**:
  - *A new persisted "restart reconnect" record keyed by restart epoch* — rejected as premature: no second consumer of this data exists yet (only the TUI/event feed renders it today), and the existing sequence already answers the question a human operator needs answered.

## Decision 3 (accepted, unresolved risk): does RST actually change firmware behavior?

- **Decision**: Ship the change and observe via the existing event feed (User Story 2), rather than block on pre-verification.
- **Rationale**: This is the evidence gap the assessment (`decision.md`) explicitly could not close — the bulb firmware is closed-source, and the user who made the go/no-go call confirmed reproducing the graceful-vs-abrupt-close comparison against real firmware isn't achievable from this side. The assessment's own risk mitigation was "ship small and reversible, observe in production" rather than "prove it first," and that's carried into this plan unchanged.
- **Alternatives considered**: none further — this was already decided at the assessment stage; re-litigating it here would contradict the recorded decision.

## Testability note

Because Decision 3's outcome is unverifiable in this repo, testing for this feature targets what *is* verifiable: that `session.Close()` now produces a hard reset rather than a graceful close, observable from a test client on a real loopback `net.Listener` (the client's next `Read` returns a reset-style error instead of `io.EOF`). This satisfies the constitution's regression-test requirement for the code change itself; it does not and cannot prove the firmware-level hypothesis, which User Story 2's observability instead exists to surface after deployment.
