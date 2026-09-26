# Implementation Plan: Automatic Bulb Reconnect After Server Restart

**Branch**: `006-bulb-reconnect-restart` | **Date**: 2026-09-26 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/006-bulb-reconnect-restart/spec.md`

## Summary

On server shutdown, close each bulb's TCP connection with a hard reset (`SO_LINGER=0`, forcing RST) instead of today's graceful close, on the theory — carried from the assessment's accepted risk (`.specify/assessments/automatic-reconnect-on-server-restart/decision.md`) — that firmware already knows how to recover from an abrupt connection loss (as it does for a power cut) but may not treat a graceful close the same way. The mechanism is a small, localized change to `internal/server/session.Close()`; the operator-visible reconnect record required by FR-003/FR-004 is largely already provided by the existing `events` feed (`Disconnected` with detail `"server shutting down"`, followed by `Discovered`/`Connected` for the same device id) and needs no new persisted entity — only confirmation that the sequence is legible enough to answer "did it reconnect."

## Technical Context

**Language/Version**: Go 1.27 (pinned in `go.mod`)

**Primary Dependencies**: none new — stdlib `net` package only (`net.TCPConn.SetLinger`)

**Storage**: N/A — no new persisted state; reuses the existing in-memory `events.Bus` feed and the existing `registry` (unchanged)

**Testing**: `go test ./...` with `-race`, table-driven, per Constitution Principle II; regression test targets the mechanism (server-side connection close behavior), not real firmware response — see Constitution Check below for why that boundary is unavoidable here

**Target Platform**: all supported build targets (Windows, Linux, macOS; amd64/arm64 per `dist/`) — resolved by FR-006

**Project Type**: single Go project (existing structure); change lands entirely inside the existing `internal/server` package

**Performance Goals**: N/A — not a merge gate per Constitution (Principle IV removed 2026-08-28)

**Constraints**: must not affect other bulbs' connections when one session closes (existing FR-016, `specs/001-local-bulb-server`); must not lose or corrupt a bulb's last-known state (spec FR-005) — state is already persisted continuously via `registry.SetState` on every property report, independent of how the socket closes, so this is preserved by not touching that path

**Scale/Scope**: small — one method (`session.Close`) changes how it closes its `net.Conn`; no new package, no new persisted entity, no new CLI surface

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Code Quality**: PASS. Change is localized to `session.Close()`; existing doc comments will be extended, not replaced; no new exported surface beyond what's already exported.
- **II. Testing Standards (NON-NEGOTIABLE)**: PASS, with a documented boundary. The constitution requires a regression test that fails without the fix and passes with it. That is achievable for the *mechanism* — a test using a real TCP loopback listener can assert that, after the fix, closing a session causes the client side to observe a reset-style error rather than a clean EOF (today's behavior), which is exactly the fix in code terms. It is **not** achievable for the *outcome* (whether the closed-source Aigo firmware actually reconnects faster or at all) — the assessment (`decision.md`) already established this is unverifiable in this repo. This is not a constitution violation: the constitution asks for proof the *code* behaves as intended, which is testable; the firmware's response is an external, unverifiable dependency, explicitly called out in the spec's Assumptions and covered by the User Story 2 observability requirement instead of a pre-ship test.
- **III. User Experience Consistency**: PASS. No new CLI flags or output format. The existing `events.Kind` vocabulary (`Disconnected`, detail `"server shutting down"`; `Connected`/`Discovered`) already covers the reconnect-visibility requirement (FR-003/FR-004) without a new event kind or detail string — reusing it is required by this principle, not just convenient.

No violations. Complexity Tracking table omitted (nothing to justify).

## Project Structure

### Documentation (this feature)

```text
specs/006-bulb-reconnect-restart/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/server/
├── session.go            # session.Close() changes: SetLinger(0) before conn.Close() on shutdown
├── session_test.go       # new/extended table-driven test: shutdown close is a hard reset
└── server.go              # unchanged; shutdown path already calls session.Close() via context.AfterFunc

internal/events/           # unchanged — existing Disconnected/Connected/Discovered kinds are reused as-is
internal/registry/         # unchanged — state persistence already independent of close style
```

**Structure Decision**: Single existing Go project (`haigosmartd`), no new packages or directories. The entire change lives inside `internal/server`, which already owns bulb connection lifecycle (`session.go`) and the shutdown trigger path (`server.go`'s `context.AfterFunc` calling `session.Close()`, per `internal/server/session.go:157-159`). This is the smallest possible footprint consistent with the constitution's "extract only when a concrete second use exists" guidance — no abstraction is introduced for a one-call-site change.

## Complexity Tracking

*No violations — table omitted.*
