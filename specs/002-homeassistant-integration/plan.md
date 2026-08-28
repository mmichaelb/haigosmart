# Implementation Plan: Home Assistant Integration

**Branch**: `002-homeassistant-integration` | **Date**: 2026-08-28 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-homeassistant-integration/spec.md`

## Summary

Extract the "read a lamp's state / change a lamp's state" logic out of the TUI command
layer into a surface-neutral service, then add a second consumer of that service which
publishes the lamps to an MQTT broker using Home Assistant's discovery convention. The TUI
keeps working exactly as it does today — it becomes one adapter over the service instead of
the owner of the logic.

Three pieces of work, in dependency order:

1. **`internal/lights`** — the abstraction. Typed operations (`SetPower`, `SetBrightness`,
   `SetColorTemp`, `Snapshot`, `Subscribe`) returning typed errors, with validation and
   capability checks that today live tangled with the TUI's output formatting.
2. **`internal/mqtt`** — a minimal MQTT 3.1.1 *client*, built on the codec feature 001
   already has. No new dependency.
3. **`internal/hass`** — discovery publication, state publication, availability, and
   command subscription, all expressed through `internal/lights`.

Adoption stays in the TUI, as instructed. The broker is the owner's to run.

**Colour temperature mapping**: the lamps express white warmth as a percentage (0 =
warmest, 100 = coolest) and Home Assistant expresses it in Kelvin. The hardware range is
**2700 K – 6500 K**, confirmed by the operator, so the mapping is a known linear conversion
rather than a guess. The two flags remain for a future lamp model with different endpoints —
see research.md §5.

## Technical Context

**Language/Version**: Go 1.27, unchanged.

**Primary Dependencies**: **None added.** Bubble Tea remains the only dependency group, and
still only `internal/tui` imports it. The MQTT client is written against the codec in
`internal/protocol`, which already encodes and decodes every packet type needed — the
client-side encoders exist in `internal/bulb/fakebulb` today and get promoted rather than
invented. See research.md §2 for why this beats taking on `paho.mqtt.golang`.

**Storage**: Unchanged — the same `registry.json`. The integration adds no persistent state
of its own; broker settings live in flags/config, and Home Assistant holds its own device
registry.

**Testing**: `go test ./... -race`, table-driven. A stub broker in `internal/mqtt/mqtttest`
(built from the same codec, mirroring what `fakebulb` does for the device side) lets the
whole integration be tested with no broker and no Home Assistant installed.

**Target Platform**: Unchanged — one binary, Linux and macOS, on the household LAN.

**Project Type**: Single binary, now with two front-ends over one core.

**Behavioural expectations**: A lamp appears in Home Assistant within a minute of adoption
(SC-001). A broker outage leaves the lamps fully controllable from the TUI (SC-010, FR-021).
Confirmation latency is inherited from feature 001 and is uneven by nature — Home Assistant
will briefly show a commanded value before the lamp's own report settles it.

**Constraints**: No outbound internet. The lamps never talk to the broker; only the server
does, so nothing about this feature can put a lamp back on the vendor's cloud. The TUI's
behaviour must not change.

**Scale/Scope**: One household, a handful of lamps, one broker. Roughly three new packages
and one substantial refactor.

## Constitution Check

*GATE: checked before Phase 0 research and re-checked after Phase 1 design.*
Constitution v2.0.0, three principles.

| Principle | How this plan complies | Verdict |
|---|---|---|
| I. Code Quality | `gofmt`/`go vet`/CI unchanged; doc comments on every new exported symbol; errors wrapped with `%w`. The refactor **removes** duplication rather than adding it: validation currently expressed as display strings becomes typed errors that both surfaces render their own way | PASS |
| II. Testing Standards | Every new package ships table-driven tests. `mqtttest` stub broker enables integration tests across the broker boundary with nothing installed, exactly as `fakebulb` did for the device boundary. The refactor is behaviour-preserving and the existing TUI suite is the proof — it must pass unchanged | PASS |
| III. UX Consistency | Two surfaces now render the same underlying outcomes. The typed errors from `internal/lights` are the single source of truth for what went wrong; `internal/control` keeps rendering them in the three documented shapes, and `internal/hass` maps them to Home Assistant's conventions. Neither invents its own vocabulary | PASS |
| Dependency constraint | Zero new dependencies. Justification for writing an MQTT client rather than importing one is in research.md §2, and the fallback if that proves wrong is named there | PASS |

**Post-Phase-1 re-check**: PASS. The design adds one interface (`lights.Service`) with one
implementation, which the constitution's "no speculative abstraction" rule would normally
question — but it exists to serve two concrete consumers that both exist in this feature,
which is the case the rule exempts. No other abstraction was introduced.

## Project Structure

### Documentation (this feature)

```text
specs/002-homeassistant-integration/
├── plan.md              # This file
├── research.md          # Phase 0: the abstraction shape, MQTT client decision, HA conventions, Kelvin mapping
├── data-model.md        # Phase 1: service types, discovery payloads, topic layout
├── quickstart.md        # Phase 1: how to run and validate against a real Home Assistant
├── contracts/
│   ├── lights-service.md    # The internal abstraction both surfaces consume
│   ├── mqtt-discovery.md    # Topics and payloads Home Assistant consumes
│   └── configuration.md     # Broker settings and the Kelvin range
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
cmd/
└── haigosmartd/          # wires server + lights + tui + hass together

internal/
├── protocol/             # UNCHANGED except promoting client-side encoders out of fakebulb
├── bulb/                 # UNCHANGED
├── server/               # UNCHANGED
├── registry/             # UNCHANGED
├── events/               # UNCHANGED — already surface-neutral, hass subscribes alongside tui
│
├── lights/               # NEW: the abstraction
│   ├── service.go        #   typed operations over registry + drivers
│   └── errors.go         #   typed errors both surfaces render themselves
│
├── mqtt/                 # NEW: minimal MQTT 3.1.1 client
│   ├── client.go         #   connect, subscribe, publish, keep-alive, reconnect, last will
│   └── mqtttest/         #   stub broker for tests
│
├── hass/                 # NEW: Home Assistant integration
│   ├── bridge.go         #   lifecycle: connect, publish discovery, subscribe commands
│   ├── discovery.go      #   capability-accurate discovery payloads
│   ├── state.go          #   state and availability publication
│   └── command.go        #   inbound commands from Home Assistant
│
├── control/              # CHANGED: becomes a thin TUI adapter over internal/lights
└── tui/                  # UNCHANGED
```

**Structure Decision**: `internal/lights` sits below both front-ends and above the registry
and drivers. `internal/control` and `internal/hass` are siblings that each depend on it and
never on each other. `internal/events` already had the right shape for a second listener —
subscribing `hass` alongside `tui` needs no change to it at all, which is the clearest
evidence the original split was drawn in the right place.

## Phase Gates

| Gate | Condition to pass | Blocks |
|---|---|---|
| **G1 — Refactor preserves behaviour** | `internal/lights` extracted, `internal/control` reduced to an adapter, and every pre-existing terminal behaviour still holds. Tests may be adapted where the API moved — see the discipline below | All integration work |
| **G2 — Broker round trip** | The in-house client connects, publishes, subscribes, survives a broker restart, and delivers its last will, all against `mqtttest` | `internal/hass` |
| **G3 — Home Assistant sees the lamp** | A real Home Assistant instance shows an adopted lamp with brightness and colour-temperature controls and no colour control, and toggling it changes the physical lamp | Feature complete |

**G1 discipline** (relaxed by the operator on 2026-08-28 — tests may be rewritten where
required, kept simple): adapting a test to a moved API is fine; changing what a test asserts
is not. Concretely, allowed is updating a call site from `ctrl.Run(...)` to
`svc.SetBrightness(...)`, or matching on `errors.Is(err, lights.ErrOutOfRange)` instead of
on an error string. Not allowed is loosening an assertion, deleting a case, or changing an
expected value so a test goes green.

That distinction matters because relaxing G1 removes the safety net the strict version
provided. The replacement is the terminal's own contract: `contracts/tui-commands.md` from
feature 001 pins the exact command grammar and the exact error strings, and those strings
must still come out unchanged. If a rewritten test no longer proves that, the contract file
does — check the diff against it rather than against the old test.

## Complexity Tracking

No constitution violations. Table intentionally omitted.
