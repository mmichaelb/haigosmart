# Implementation Plan: Local Replacement Server for Aigo Smart Bulbs

**Branch**: `001-local-bulb-server` | **Date**: 2026-08-27 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-local-bulb-server/spec.md`

## Summary

Build a Go daemon that impersonates the Aigo cloud endpoint on the LAN so unmodified
bulbs connect to it instead of the vendor, plus a terminal UI for control and a live
event feed. The bulbs' wire protocol is not documented anywhere, so the plan opens with a
**capture-and-identify spike** (Phase 0) driven by the operator: redirect the bulb and the
phone app through mitmproxy, run a scripted sequence of app interactions, and drop the
resulting dumps in `captures/`. Everything downstream — framing, crypto, command payloads —
is derived from that capture, not guessed.

Stack: Go 1.27 standard library for networking, persistence, and logging; Bubble Tea for
the TUI (the only dependency group). Layout follows golang-standards/project-layout.

**Honest risk, stated up front**: if the bulbs pin certificates or use a per-device key
negotiated out of band, TLS interception yields opaque bytes and the protocol layer needs a
different attack (firmware dump or key extraction from the app). Phase 0 has an explicit
decision gate for this; Phases 2+ are only schedulable once the gate passes. Every
non-protocol part of the system (registry, TUI, event bus, persistence) is designed against
a narrow interface and is buildable and testable in parallel with the spike using a fake
bulb driver.

## Technical Context

**Language/Version**: Go 1.27 (pinned in `go.mod`)

**Primary Dependencies**:
- `github.com/charmbracelet/bubbletea` + `bubbles/textinput` + `lipgloss` — TUI only.
  Justification: the spec requires a scrolling live event feed and a command prompt on
  screen simultaneously (FR-013, FR-015). Doing that on a raw terminal means writing
  raw-mode handling, redraw, and resize logic by hand. Bubble Tea is the smallest
  well-maintained option; see `research.md` for the zero-dependency alternative kept in
  reserve.
- Nothing else. Networking, TLS, JSON, crypto, logging, and testing are stdlib.

**Storage**: One JSON file (`registry.json`) under the OS config dir, written atomically
(temp file + `os.Rename`). No database — the entity count is bounded by the number of bulbs
in a house.

**Testing**: `go test ./... -race`, table-driven. A `fakebulb` test double implements the
bulb-side of the protocol so server, registry, and TUI logic are testable with no hardware.

**Target Platform**: Linux and macOS, single self-contained binary, runs on a
LAN-resident always-on machine.

**Project Type**: Single-binary daemon with an interactive terminal UI.

**Behavioural expectations** (not performance gates — the constitution no longer has a
performance principle): 30 concurrent bulb connections (FR-020); a command visibly takes
effect and a reported change reaches the feed without the operator waiting on them (SC-003,
SC-004, and Principle III's responsiveness); the TUI and the network path never block each
other. That last one is a **correctness** property, not a speed target: a stalled terminal
must not stall a bulb's read loop, and it is enforced by race-detected tests under
Principle II, not by a stopwatch.

**Constraints**: No outbound internet connection during normal operation (FR-002/SC-001).
Bulbs are unmodified — the server adapts to them, never the reverse. It should run
comfortably on a Raspberry Pi, which is a sizing sanity check rather than a budget anyone
has to defend in review.

**Scale/Scope**: ~30 bulbs, one operator, one machine. Roughly 8 internal packages.

## Constitution Check

*GATE: checked before Phase 0 research and re-checked after Phase 1 design.*

| Principle | How this plan complies | Verdict |
|---|---|---|
| I. Code Quality | `gofmt`/`go vet` in CI; doc comments on every exported symbol; errors wrapped with `%w`; no speculative extraction — the protocol codec gets one implementation until a second bulb family actually appears | PASS |
| II. Testing Standards | Table-driven unit tests per package; `fakebulb` double enables integration tests across the connection boundary with no hardware; codec tests are driven by real byte sequences extracted from `captures/`; `-race` mandatory (the TUI and network layer are concurrent by construction) | PASS |
| III. UX Consistency | One command grammar for the whole TUI (`contracts/tui-commands.md`); one error shape (what failed + how to fix); event feed lines share one format | PASS |
| Dependency constraint | Exactly one dependency group (Bubble Tea), justified above and isolated behind `internal/tui` so nothing else imports it | PASS |

**Post-Phase-1 re-check**: PASS. The design added no dependencies and no abstraction with a
single implementation, except `bulb.Driver`, which has two implementations from day one
(real protocol + `fakebulb`) and therefore earns its place.

**Post-`/speckit-analyze` re-check (2026-08-27)**: two constitution gaps were found and closed in `tasks.md`.
1. Principle IV's benchmark clause had been read as conditional when performance was a stated goal of this feature; benchmarks became T060/T060a with a CI job in T004.
2. The project was not a git repository, so the mandated PR-review and green-CI gates could not exist at all. Repository initialization is now T000, ahead of every other task.

**Constitution amendment (2026-08-28)**: Principle IV (Performance Requirements) was removed;
the constitution is now version 2.0.0 with three principles. Item 1 above is history, not a
live obligation. **Performance is no longer a merge gate** — no PR needs a benchmark and CI no
longer runs one. The benchmarks written under the old principle are kept as reference
measurements in `docs/performance.md`: they cost nothing to keep and are useful when a change
feels slow, but nothing fails because of them. The table above re-checks clean against the
three remaining principles.

## Project Structure

### Documentation (this feature)

```text
specs/001-local-bulb-server/
├── plan.md              # This file
├── research.md          # Phase 0: capture instructions, protocol identification, tech decisions
├── data-model.md        # Phase 1: entities, state transitions, persisted schema
├── quickstart.md        # Phase 1: how to run and validate end to end
├── contracts/
│   ├── tui-commands.md      # Operator command grammar and output shapes
│   ├── bulb-protocol.md     # Protocol contract — TEMPLATE, filled from the capture
│   └── registry-file.md     # On-disk registry schema
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
cmd/
└── haigosmartd/          # main: flag parsing, wiring, signal handling, TUI startup

internal/
├── protocol/             # wire codec: framing, crypto, message types (from the capture)
│   └── testdata/         # byte fixtures extracted from captures/
├── bulb/                 # Bulb entity, Driver interface, connection state machine
│   └── fakebulb/         # test double speaking the bulb side of the protocol
├── server/               # TCP/TLS listener, per-connection lifecycle, keep-alive
├── registry/             # in-memory bulb registry + atomic JSON persistence
├── events/               # event types and a fan-out bus (drop-oldest, never blocks)
├── control/              # command layer: name/on/off/brightness/color, target resolution
└── tui/                  # Bubble Tea model: event feed + command prompt (only place importing bubbletea)

captures/                 # operator-supplied mitmproxy dumps (gitignored, not committed)
docs/                     # operator-facing setup notes
configs/                  # example config file
```

No `pkg/` directory: nothing here is intended for import by other projects, and
golang-standards/project-layout explicitly says not to create one without that need.
No `api/` directory: the only external interface is the bulb wire protocol, documented
in `contracts/`.

**Structure Decision**: Single Go module, `internal/`-heavy, one binary in
`cmd/haigosmartd`. The TUI is a thin consumer of `control` and `events`; `control` and
`registry` never import `tui`, so the interface is replaceable (see the fallback in
`research.md`) without touching business logic.

## Phase Gates

| Gate | Condition to pass | Blocks |
|---|---|---|
| **G0 — Capture obtained** | `captures/` contains a bulb-side dump and an app-side dump covering the scripted interaction sequence in `research.md` §2 | All protocol work |
| **G1 — Protocol identified** | Transport, framing, and payload encoding are documented in `contracts/bulb-protocol.md`, with byte fixtures in `internal/protocol/testdata/` and at least one decoded state-change message | Protocol implementation |
| **G2 — Bulb connects** | A physical bulb completes its handshake against the server and stays connected across two keep-alive intervals | TUI control work against real hardware (fakebulb work proceeds regardless) |

If G1 fails because the payload is encrypted with an unrecoverable key, stop and re-plan;
`research.md` §4 lists the fallback routes in the order they are worth trying.

## Complexity Tracking

No constitution violations. Table intentionally omitted.
