---

description: "Task list for 002-homeassistant-integration"
---

# Tasks: Home Assistant Integration

**Input**: Design documents from `/specs/002-homeassistant-integration/`

**Prerequisites**: Feature 001 complete and running with at least one adopted lamp.

**Tests**: Included and non-optional — the constitution marks testing standards
NON-NEGOTIABLE (Principle II). Note the lesson from feature 001's hardware bring-up: a test
double built from assumptions agrees with those assumptions. The stub broker here
deliberately implements only the MQTT wire protocol and knows nothing about Home
Assistant's semantics, so assertions are made against Home Assistant's published discovery
format rather than against a helper of ours that could be wrong in the same direction.

**Organization**: Grouped by user story. Task IDs restart at T001; this is a separate file
from feature 001's.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 / US2 / US3 from spec.md
- Exact file paths in every task

## Path Conventions

Single Go module, unchanged layout. New packages: `internal/lights`, `internal/mqtt`,
`internal/hass`. Changed: `internal/control` (becomes an adapter), `cmd/haigosmartd`
(wiring). Untouched: `internal/protocol` except for promoting client-side encoders,
`internal/bulb`, `internal/server`, `internal/registry`, `internal/events`, `internal/tui`.

---

## Phase 1: Setup

**Purpose**: Configuration surface and the codec pieces the client needs. No behaviour
changes.

- [X] T001 [P] Promote the client-side MQTT encoders out of `internal/bulb/fakebulb/fakebulb.go` into `internal/protocol/mqtt.go`: `EncodeConnect` (with optional will topic, will payload, will retain, username and password), `EncodeSubscribe`, and `EncodeDisconnect`. `fakebulb` then calls them instead of hand-rolling packets, so the device double and the broker client share one encoder
- [X] T002 [P] Add `DecodeConnack` and `DecodeSuback` to `internal/protocol/mqtt.go`, returning the return code so a rejected connection reports why rather than looking like a network error
- [X] T003 Write `internal/protocol/mqtt_client_test.go`: table-driven round-trip tests for the new encoders and decoders, including a CONNECT carrying a will and credentials, and a CONNACK with a non-zero return code
- [X] T004 [P] Add the MQTT and Kelvin flags to `cmd/haigosmartd/main.go` per `contracts/configuration.md`, defaulting `-mqtt-broker` to empty so the integration stays off unless asked for, and refusing startup when `-ct-min-kelvin >= -ct-max-kelvin` with a message naming both values
- [X] T005 [P] Extend `configs/haigosmart.example.json` with the new settings and a note that the broker is the owner's to run

**Checkpoint**: builds, existing suite green, integration not yet wired to anything.

---

## Phase 2: Foundational — the abstraction and the client

**Purpose**: Everything both user-facing surfaces stand on. **Blocks all user stories.**

### The abstraction (Gate G1)

- [X] T006 Create `internal/lights/errors.go` with the typed errors from `contracts/lights-service.md`: `ErrUnknownBulb`, `ErrNotAdopted`, `ErrNotConnected`, `ErrOutOfRange`, each wrapped with context so callers use `errors.Is` and still get a usable message. Re-use `bulb.ErrUnsupported` and `bulb.ErrUnconfirmed` rather than redefining them
- [X] T007 Create `internal/lights/service.go` with `Service`, `Change`, and the operations in `contracts/lights-service.md`. Move validation, capability checks, timeout handling and driver dispatch here verbatim from `internal/control/execute.go` — this is a move, not a redesign
- [X] T008 Enforce the layering in `internal/lights/service.go`'s package doc and add `internal/lights/layering_test.go` asserting via `go list` that `internal/lights` imports no front-end package (`control`, `tui`, `hass`, bubbletea). A headless server must never pull in a terminal UI
- [X] T009 [P] Write `internal/lights/service_test.go` against `fakebulb`: each operation's happy path, `ErrOutOfRange` at both bounds, `bulb.ErrUnsupported` for a capability a known bulb lacks, `bulb.ErrUnconfirmed` from a silent bulb, and a no-op change sending nothing
- [X] T010 [P] Write `internal/lights/change_test.go`: a `Change` with nil fields leaves those attributes untouched; a partial change sends only what differs; ordering matches feature 001 (power-on first, power-off last and alone)
- [X] T011 Rewrite `internal/control/execute.go` as an adapter: parse, resolve the target by name or prefix, call `lights.Service`, and render the typed error into the three documented shapes. Target resolution and all `Result` formatting stay here; nothing else does
- [X] T012 Adapt `internal/control/control_test.go` to the moved API. **G1 discipline** (plan.md): update call sites and match on `errors.Is` instead of error strings, but do not loosen an assertion, drop a case, or change an expected value. Feature 001's `contracts/tui-commands.md` is the reference for what must still come out
- [X] T013 Verify Gate G1: `go test ./... -race` green and every error string in feature 001's `contracts/tui-commands.md` still produced verbatim by the terminal. If a string changed, the refactor is wrong, not the contract

**Checkpoint G1**: the terminal behaves exactly as before, over a service a second surface can also use.

### The MQTT client (Gate G2)

- [X] T014 [P] Create `internal/mqtt/mqtttest/broker.go`: an in-process stub broker over the `internal/protocol` codec. Accepts CONNECT, tracks subscriptions, delivers publishes, honours retained messages, and can be told to drop connections and to publish a registered will. Deliberately knows nothing about Home Assistant
- [X] T015 Create `internal/mqtt/client.go`: connect with optional credentials and last will, publish at QoS 0 and 1 with retain, subscribe with a per-topic handler, keep-alive pings, and clean shutdown. Define the small interface `internal/hass` will depend on, so swapping in `paho` later stays contained (research.md §2)
- [X] T016 Add reconnect with capped exponential backoff to `internal/mqtt/client.go`, re-establishing subscriptions and firing an on-connect callback so callers can republish retained state
- [X] T017 [P] Write `internal/mqtt/client_test.go` against the stub broker: connect and publish; subscribe and receive; QoS 1 acknowledgement; credentials passed through; a rejected CONNACK reported with its reason, not as a generic failure
- [X] T018 [P] Write `internal/mqtt/reconnect_test.go`: the broker drops the connection and the client returns on its own, re-subscribes, and fires the on-connect callback; backoff is bounded; a broker that is never reachable retries without spinning
- [X] T019 [P] Write `internal/mqtt/will_test.go`: the will registered at connect is published by the broker when the connection dies without a DISCONNECT — the only mechanism that reports a crash or a pulled cable
- [X] T020 Verify Gate G2 by running `go test ./internal/mqtt/... -race`: the client survives a broker restart and delivers its will, entirely against the stub in `internal/mqtt/mqtttest/broker.go`

**Checkpoint G2**: a dependable client. User story work can begin.

---

## Phase 3: User Story 1 — Lamps appear in Home Assistant and can be controlled (P1) 🎯 MVP

**Goal**: Every adopted lamp shows up as a Home Assistant device with a working light
entity, with no YAML edited by hand.

**Independent Test**: Adopt a lamp in the terminal, open Home Assistant, confirm it appears
without touching configuration, and toggle it from a dashboard card while watching the
physical lamp.

**Depends on**: Phase 2 complete.

### Tests for User Story 1

- [X] T021 [P] [US1] Write `internal/hass/discovery_test.go`: the config payload for an adopted lamp carries `schema: "json"`, a `unique_id` derived from the device id, the state, command and availability topics, and a `device` block — asserted field by field against `contracts/mqtt-discovery.md`, not against a builder of ours
- [X] T022 [P] [US1] Write `internal/hass/command_test.go`: a command payload turns into the right `lights.Change`; a payload setting only brightness leaves power untouched; a malformed payload is ignored without dropping the broker connection or affecting another lamp
- [X] T023 [US1] Write `internal/hass/bridge_test.go` end to end over the stub broker: an adopted lamp produces a retained discovery config; a command on its set topic reaches `fakebulb`; the lamp's report produces a retained state publish

### Implementation for User Story 1

- [X] T024 [US1] Create `internal/hass/topics.go`: the topic layout from `contracts/mqtt-discovery.md`, with the discovery and base prefixes configurable
- [X] T025 [US1] Create `internal/hass/discovery.go` building the config payload from a `bulb.Bulb`. Advertise `["brightness"]` for now — exact capabilities are User Story 2, and shipping a conservative claim beats shipping a wrong one
- [X] T026 [US1] Create `internal/hass/state.go` publishing the retained state payload from a lamp's reported state, converting brightness from the lamp's 0–100 to Home Assistant's 0–255
- [X] T027 [US1] Create `internal/hass/command.go` parsing an inbound command into a `lights.Change` and calling `lights.Service`. Publish nothing optimistically: the lamp changes, the lamp reports, the report is published
- [X] T028 [US1] Create `internal/hass/bridge.go` tying it together: connect, subscribe to every adopted lamp's set topic, publish discovery, and subscribe to the event bus so reported changes publish state. Treat `bulb.ErrUnconfirmed` as success — it is not a failure, and feature 001 learned that the hard way
- [X] T029 [US1] In `internal/hass/bridge.go`, publish an empty retained payload on the config topic when a lamp leaves the registry, and skip publishing entirely for lamps that are discovered but not adopted (FR-015, FR-016)
- [X] T030 [US1] Wire the bridge into `cmd/haigosmartd/main.go`, started only when `-mqtt-broker` is set, cancelled with the server's context, and never able to block server startup

**Checkpoint**: quickstart scenarios 1, 2, 10 and 12 pass. Gate G3 is reachable.

---

## Phase 4: User Story 2 — The interface shows only what the lamp can do (P2)

**Goal**: A white-only lamp presents brightness and warmth and no colour wheel, because the
entity never claims a colour channel.

**Independent Test**: Open the lamp's more-info dialog in Home Assistant and confirm the
controls match the hardware — warmth present, colour absent — and that every control shown
changes the lamp.

**Depends on**: Phase 2, plus US1's discovery publication (T025).

### Tests for User Story 2

- [X] T031 [P] [US2] Write `internal/hass/capabilities_test.go` covering every row of the capability mapping in data-model.md: CCT-only yields `["color_temp"]`, colour+CCT yields both, colour-only yields `["rgb"]`, neither yields `["brightness"]`, and **`Known == false` yields `["brightness"]`** — claiming nothing unproven (FR-008)
- [X] T032 [P] [US2] Write `internal/hass/kelvin_test.go`: percent 0 maps to 2700 K and percent 100 to 6500 K; the mapping round-trips stably so a Kelvin request does not drift the lamp's value; out-of-range Kelvin clamps to the ends rather than wrapping
- [X] T033 [P] [US2] Write `internal/hass/brightness_test.go`: the 0–100 to 0–255 conversion round-trips stably in both directions, so a Home Assistant slider at 128 does not oscillate through 50 back to 127
- [X] T034 [US2] Extend `internal/hass/discovery_test.go`: a CCT lamp's payload contains `min_kelvin` 2700, `max_kelvin` 6500 and no colour mode, and a brightness-only lamp carries neither Kelvin field

### Implementation for User Story 2

- [X] T035 [P] [US2] Create `internal/hass/capabilities.go` mapping `bulb.Capabilities` to `supported_color_modes` per data-model.md. The `Known == false` branch is the point of the whole thing: feature 001 distinguishes "no colour" from "never found out", and only the first may be advertised as a fact
- [X] T036 [P] [US2] Create `internal/hass/convert.go` with the Kelvin and brightness conversions, both round-trip stable, using the configured Kelvin range
- [X] T037 [US2] Use the capability mapping and conversions in `internal/hass/discovery.go`, replacing US1's conservative `["brightness"]` placeholder, and emit `min_kelvin`/`max_kelvin` only for lamps with warmth
- [X] T038 [US2] Include `color_mode` and `color_temp_kelvin` in the state payload in `internal/hass/state.go`, omitting both for lamps without warmth
- [X] T039 [US2] Accept `color_temp_kelvin` in `internal/hass/command.go`, converting to the lamp's percent scale, and reject a warmth command for a lamp whose capabilities are `Known` and lack it

**Checkpoint**: quickstart scenario 3 passes — the one no automated test can substitute for, because whether Home Assistant *renders* a warmth slider is a fact about Home Assistant.

---

## Phase 5: User Story 3 — Reported state and honest availability (P3)

**Goal**: What Home Assistant shows is what the lamp reported, and a lamp that is not
answering says so rather than showing a stale value.

**Independent Test**: Unplug the lamp, watch it become unavailable, plug it back in, and
confirm Home Assistant reflects the state the lamp reports on startup with no reload.

**Depends on**: Phase 2, plus US1's bridge and state publication (T026, T028).

### Tests for User Story 3

- [X] T040 [P] [US3] Write `internal/hass/availability_test.go`: a lamp publishes `online` when connected and `offline` when it disconnects; the bridge publishes `online` on connect; the will payload is `offline` on the bridge topic and retained
- [X] T041 [P] [US3] Write `internal/hass/restore_test.go`: with a registry holding persisted state, starting the bridge publishes **no** state for a lamp that has not connected, and the lamp stays unavailable until it reports. This is the requirement most integrations get wrong (FR-010, research.md §7)
- [X] T042 [US3] Extend `internal/hass/bridge_test.go`: after a simulated broker reconnect, every discovery config, state and availability message is republished, so a Home Assistant restart recovers with no action
- [X] T043 [P] [US3] Write `internal/hass/rename_test.go`: renaming a lamp republishes its config with the new `name` and an unchanged `unique_id`, so no duplicate device appears and history survives (FR-013, FR-014)

### Implementation for User Story 3

- [X] T044 [US3] Add per-lamp availability publication to `internal/hass/state.go`, following `bulb.Status == Connected`, driven by the `Connected` and `Disconnected` events already on the bus
- [X] T045 [US3] Add the bridge availability topic and register the last will in `internal/hass/bridge.go`, publishing `online` on connect. The will is what reports a crash; a shutdown handler cannot
- [X] T046 [US3] Emit `availability_mode: "all"` with both topics in the discovery payload in `internal/hass/discovery.go`, so a lamp is available only when the server is up and the lamp is connected
- [X] T047 [US3] Enforce the no-restore rule in `internal/hass/bridge.go`: never publish persisted state, and never mark a lamp available before it has reported. An entity showing last week's brightness is worse than one showing nothing
- [X] T048 [US3] In `internal/hass/bridge.go`, republish all discovery, state and availability on every broker reconnect via the client's on-connect callback (FR-022)
- [X] T049 [US3] In `internal/hass/bridge.go`, republish a lamp's discovery config when it is renamed in the terminal, keeping `unique_id` fixed so Home Assistant treats it as the same device and a name set inside Home Assistant is not overwritten

**Checkpoint**: quickstart scenarios 4, 5, 6, 7, 8, 9 and 11 pass.

---

## Phase 6: Polish

- [X] T050 [P] Write `docs/homeassistant.md`: installing an MQTT broker is the owner's job, what to configure here, what appears in Home Assistant, and how to diagnose a lamp visible in the terminal but not in Home Assistant
- [X] T051 [P] Add doc comments to every exported symbol in `internal/lights`, `internal/mqtt` and `internal/hass` (Constitution I)
- [X] T052 [P] Update `README.md`: the lamps now work from Home Assistant, with the broker named as a prerequisite
- [X] T053 Confirm via `cmd/haigosmartd` that an unset `-mqtt-broker` leaves feature 001 behaviour untouched, and that stopping the broker leaves the terminal fully working; record the result in `docs/homeassistant.md` (FR-019, FR-021, SC-010)
- [X] T054 Audit every user-facing string added in this feature against feature 001's `contracts/tui-commands.md` vocabulary, so the two surfaces do not drift (Constitution III)
- [X] T055 Run the full quickstart.md validation against a real Home Assistant — **Gate G3 PASSED 2026-08-28**. The lamps appear, are controllable, and show only the controls the hardware has

---

## Dependencies & Execution Order

### Phase dependencies

- **Phase 1 (Setup)**: no dependencies
- **Phase 2 (Foundational)**: needs Phase 1. **Blocks everything.** Its two halves are
  independent of each other: the `lights` extraction (T006–T013) and the MQTT client
  (T014–T020) touch no common files and can proceed in parallel
- **Phase 3 (US1)**: needs both halves of Phase 2
- **Phase 4 (US2)**: needs Phase 2 and T025
- **Phase 5 (US3)**: needs Phase 2, T026 and T028
- **Phase 6 (Polish)**: needs the stories you intend to ship

### Critical path

```text
T001..T005 ──┬──► T006..T013 (G1: abstraction) ──┐
             │                                    ├──► T024..T030 (US1) ──► T031..T039 (US2) ──► T040..T049 (US3) ──► T055 (G3)
             └──► T014..T020 (G2: mqtt client) ──┘
```

The two Phase 2 halves in parallel are the whole reason this feature is not sequential.

### Parallel opportunities

- T001, T002, T004, T005 all parallel
- T009, T010 parallel; T014 parallel with the whole `lights` extraction
- T017, T018, T019 parallel
- T021, T022 parallel; T031, T032, T033 parallel; T035, T036 parallel
- T040, T041, T043 parallel
- T050, T051, T052 parallel

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1: setup
2. Phase 2: both halves, in parallel if there are two of you
3. Phase 3: US1
4. **STOP and VALIDATE**: quickstart scenarios 1, 2, 10, 12. The lamps are in Home Assistant
   and controllable, which is the ask. US2 and US3 make it correct and honest, and both are
   short
5. Live with it for a day before continuing — the same advice that surfaced four real bugs
   in feature 001

### Incremental delivery

1. Setup + Foundational → the terminal is unchanged and a client exists
2. US1 → lamps in Home Assistant → **MVP**
3. US2 → controls match the hardware
4. US3 → state and availability are honest
5. Polish → docs, audit, full validation

---

## Hardware bring-up (2026-08-28)

**Gate G3 passed. The integration works against a real Home Assistant.** An adopted
`headlamp` appears as a light entity with brightness and warmth and no colour wheel, under
a `haigosmart` server device.

Two defects surfaced only against the real thing, both in the device-registry relationship
rather than in anything the payload tests could see:

1. **"Unnamed device".** Every lamp declared `via_device: "haigosmart"`, which Home
   Assistant resolves against a device's `identifiers` — and nothing had ever declared
   them, so it invented a placeholder to hang the lamps off. MQTT discovery creates a
   device only as a side effect of an entity, so the server now publishes a **Status**
   connectivity sensor of its own. That sensor deliberately carries no availability topic:
   an entity whose job is reporting the server being gone must not vanish at that moment.
   Worth noting the shape of this bug — it is invisible in any single payload and only
   appears when the lamp's `via_device` and the server's `identifiers` are compared, which
   is why no unit test caught it and why one now does.

2. **Devices labelled by firmware token.** Home Assistant shows the model prominently, and
   `aigo_light_cct` was passed through raw. Models are now product names ("Tunable White
   Smart Bulb", "Bulb Server") with the raw firmware string kept as `sw_version`.

Also confirmed as intended rather than a fault: Home Assistant lists the server and each
lamp as **separate devices**. `via_device` draws a "connected via" relationship on the
lamp's page; it does not nest entries in the device list. The operator chose to keep both,
so the Status sensor remains the one thing that shows whether the server itself is running.

## Notes

- **G1 is the risky one, not the MQTT work.** The client is new code with no users; the
  extraction touches a terminal that currently works. Tests may be adapted to the moved API
  but not weakened — see plan.md, and check the terminal's output against feature 001's
  `contracts/tui-commands.md` rather than against the tests you just edited
- No new dependencies. If the in-house client turns into a source of real bugs rather than
  a source of small ones, `paho.mqtt.golang` goes behind the interface from T015 and the
  change is contained
- `bulb.ErrUnconfirmed` is not a failure anywhere, including in Home Assistant
- Nothing in this feature lets a lamp talk to the broker — only the server does, so no
  broker problem can put a lamp back on the vendor's cloud
- Commit after each task or logical group
