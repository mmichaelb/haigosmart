---

description: "Task list for 001-local-bulb-server"
---

# Tasks: Local Replacement Server for Aigo Smart Bulbs

**Input**: Design documents from `/specs/001-local-bulb-server/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included and non-optional. The project constitution marks testing standards
NON-NEGOTIABLE (Principle II), so every package ships table-driven tests and `-race` is
mandatory in CI. This is a constitution requirement, not a stylistic preference.

**Organization**: Grouped by user story so each is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 / US2 / US3 from spec.md
- Exact file paths in every task

## Path Conventions

Single Go module at repository root, golang-standards/project-layout:
`cmd/haigosmartd/`, `internal/{protocol,bulb,server,registry,events,control,tui}/`,
`captures/` (gitignored), `docs/`, `configs/`. Go convention keeps tests beside the code
as `*_test.go`, so there is no top-level `tests/` directory.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project skeleton and gates. Nothing here depends on the protocol.

- [X] T000 Initialize the git repository at the repository root: `git init`, a `main` branch, an initial commit of the existing `go.mod` and `specs/`, and a remote. Constitution mandates PR review and green CI on every change; neither gate can exist without a repository, and T004 writes a workflow file that is inert until this lands
- [X] T001 Create the directory skeleton from plan.md in the repository root: `cmd/haigosmartd/`, `internal/{protocol,bulb,server,registry,events,control,tui}/`, `internal/protocol/testdata/`, `internal/bulb/fakebulb/`, `docs/`, `configs/`, each with a `doc.go` stating the package purpose (Constitution I requires package doc comments)
- [X] T002 Add a root `.gitignore` covering `captures/*` (keeping `captures/.gitignore`), the `haigosmartd` binary, and `*.mitm`/`*.pcap`/`*.jsonl` anywhere
- [X] T003 [P] Add `Makefile` at repository root with `fmt`, `vet`, `test` (`go test ./... -race`), `build`, and `run` targets so the constitution's gates are one command each
- [X] T004 [P] Add `.github/workflows/ci.yml` running `gofmt -l .` (fail if non-empty), `go vet ./...`, and `go test ./... -race` on push and PR (Constitution: CI enforces quality, not reviewer memory). Depends on T000. **Amended 2026-08-28**: the benchmark job was removed with Principle IV; benchmarks stay runnable via `make bench` but gate nothing
- [X] T005 [P] Write `docs/capture-setup.md` from research.md §1–2: the mitmdump invocations, why `connection_strategy=lazy` and `tcp_hosts` are required, the 17-step app interaction sequence, and the dump commands — so the operator can reproduce the capture without re-reading the plan
- [X] T006 [P] Add `configs/haigosmart.example.json` documenting every `-flag` from `contracts/tui-commands.md` with its default

**Checkpoint**: Skeleton builds (`go build ./...` succeeds on empty packages), CI is green.

---

## Phase 2: Protocol Discovery (Gate G0 → G1) 🚧 BLOCKING

**Purpose**: Turn the bulbs' undocumented protocol into a written contract and byte
fixtures. **Every task in `internal/protocol` and `internal/server` is blocked until T012
passes.** Phase 3 and the non-protocol parts of Phases 4–6 are NOT blocked and may proceed
in parallel with this phase.

**⚠️ This phase is operator-driven. It cannot be completed by writing code.**

- [X] T007 Operator: run the two mitmdump captures per `docs/capture-setup.md`, plus the `tcpdump` insurance capture, producing `captures/bulb.mitm`, `captures/app.mitm`, `captures/bulb.pcap`
- [X] T008 Operator: execute the 17-step interaction sequence (research.md §2) in order with the stated pauses, recording wall-clock time per step in `captures/steplog.txt`; repeat steps 1–3 with a second bulb if one is available
- [X] T009 Generate readable dumps with `scripts/dump_flows.py` into `captures/bulb.jsonl` and `captures/app.jsonl`, then verify non-emptiness and that message bursts align with the step log — **Gate G0 passes here**
- [X] T010 Identify the platform family from the destination hostnames and ports in `captures/bulb.jsonl` against the table in research.md §3; record the verdict and evidence at the top of `specs/001-local-bulb-server/contracts/bulb-protocol.md`
- [X] T011 Fill sections 1–9 of `specs/001-local-bulb-server/contracts/bulb-protocol.md` from observed bytes: transport, framing, encryption, message types, handshake, keep-alive interval (from step 14), state report layout, and the command encodings (by diffing steps 4–13, each of which changes exactly one thing). **Including §5a**: how the bulb reports model and capabilities — capture step 3 (device detail page) is the richest source, because the app renders exactly the controls the device supports
- [X] T012 Extract byte fixtures into `internal/protocol/testdata/` as `<direction>_<messagetype>_step<N>.hex`, scrubbing WiFi credentials, account tokens, and cloud keys with clearly marked placeholders — **Gate G1 passes here; protocol implementation unblocks**

**If T010–T012 stall because the payload is encrypted with an unrecoverable key**: stop and
work research.md §4 in order. Do not begin protocol implementation against a guess.

**Checkpoint**: `contracts/bulb-protocol.md` has no unfilled checkboxes; fixtures exist.

---

## Phase 3: Foundational (Blocking Prerequisites)

**Purpose**: Shared types and infrastructure every user story needs. Protocol-agnostic, so
this phase runs in parallel with Phase 2.

- [X] T013 [P] Define `LightState`, `RGB`, `Mode`, and `Capabilities` in `internal/bulb/state.go` per data-model.md, with normalised units (brightness 0–100, colour RGB 0–255, Kelvin) and a `Diff(other LightState) []events.FieldChange` method
- [X] T014 [P] Define `Bulb` and the `Status` enum (`Discovered`/`Connected`/`Disconnected`) in `internal/bulb/bulb.go` per data-model.md
- [X] T015 [P] Define the `Event`, `Kind`, and `FieldChange` types in `internal/events/event.go` per data-model.md, including the display formatting rules from `contracts/tui-commands.md` §"Event feed lines"
- [X] T016 [P] Define the `Driver` interface in `internal/bulb/driver.go` — the seam every protocol implementation and the test double satisfy: `Apply(ctx, LightState) error`, `State() LightState`, `DeviceID() string`, `Close() error`
- [X] T017 Implement the event bus in `internal/events/bus.go`: buffered per-subscriber channels, drop-oldest on overflow with a monotonic dropped counter, `Publish` never blocks the caller — a stalled terminal must not stall a bulb's read loop. This is a correctness property proven by race-detected tests (Constitution II), not a speed target
- [X] T018 [P] Write `internal/events/bus_test.go`: table-driven coverage of fan-out to multiple subscribers, drop-oldest under a deliberately stalled subscriber, dropped-counter accuracy, and `Publish` non-blocking under `-race`
- [X] T019 [P] Write `internal/bulb/state_test.go`: table-driven `Diff` coverage including no-change, single-field, all-fields, and unsupported-capability cases
- [X] T020 Configure `log/slog` in `internal/events/log.go` so every published event is logged unconditionally regardless of display drops (FR-018, and the SC-008 distinction recorded in research.md §8); JSON handler to file, text handler to stderr in `-headless` mode
- [X] T021 Wire `cmd/haigosmartd/main.go` skeleton: flag parsing per `contracts/tui-commands.md` §"CLI flags", `context.Context` with SIGINT/SIGTERM cancellation, and clean shutdown ordering (stop listener → close connections → flush registry)

**Checkpoint**: Foundation compiles and its tests pass under `-race`. User story work can begin.

---

## Phase 4: User Story 1 - Bulbs connect to the local server (Priority: P1) 🎯 MVP

**Goal**: Unmodified bulbs complete their handshake against this server, register, stay
connected across keep-alives, reconnect after power loss, and send zero traffic to the
vendor.

**Independent Test**: Point one bulb at the server, power-cycle it, confirm it appears
registered, survives a keep-alive cycle, and that a packet capture shows no outbound
connection to any non-local address.

**Depends on**: Phase 3 complete; T012 (Gate G1) for every protocol task below.

### Tests for User Story 1

- [X] T022 [P] [US1] Write `internal/protocol/codec_test.go`: table-driven decode/encode over every fixture in `internal/protocol/testdata/`, asserting round-trip stability and exact byte equality against the real captured bytes
- [X] T023 [P] [US1] Write `internal/protocol/codec_malformed_test.go`: truncated frames, bad checksums, oversized length fields, and garbage bytes all return a wrapped error and never panic (FR-016)
- [X] T023a [P] [US1] Write `internal/protocol/capabilities_test.go`: a colour bulb reports `Color=true, Known=true`; a white-only bulb reports `Color=false, Known=true`; a device whose metadata is absent from the fixtures yields `Known=false`; and the `Known=false` path lets a colour command through to the bulb rather than pre-refusing it (data-model.md)
- [X] T024 [P] [US1] Write `internal/registry/registry_test.go`: add, lookup by ID, rename, duplicate-name rejection, duplicate-DeviceID handling that keeps both entries and raises `DuplicateID` (data-model.md edge case), concurrent access under `-race`
- [X] T025 [P] [US1] Write `internal/registry/persist_test.go`: round-trip save/load, missing file yields an empty registry without error, corrupt file yields a startup error that leaves the file untouched, unknown `version` is a named error, atomic replacement survives a simulated mid-write failure (contracts/registry-file.md)
- [X] T026 [US1] Write `internal/server/server_test.go` integration tests driven by `fakebulb`: handshake completes and the bulb registers; reconnect with the same DeviceID rejoins the existing entry rather than creating a duplicate (FR-005); missing keep-alives transition the bulb to `Disconnected`; a malformed handshake drops only that connection and leaves other bulbs connected (FR-016)

### Implementation for User Story 1

- [X] T027 [US1] Implement framing and crypto in `internal/protocol/codec.go` exactly as documented in `contracts/bulb-protocol.md` §2–3, converting device units to the normalised model units at this boundary so nothing above it deals in wire encodings
- [X] T028 [US1] Implement message types and their marshal/unmarshal in `internal/protocol/messages.go` per `contracts/bulb-protocol.md` §4, §7, §8
- [X] T028a [US1] Implement capability extraction in `internal/protocol/capabilities.go` per `contracts/bulb-protocol.md` §5a: populate `Capabilities` from the device-metadata message, or fall back to inferring it from which fields the state report contains. Set `Known=false` when neither route yields an answer — `Color: false` must never be indistinguishable from "never determined" (FR-010, data-model.md)
- [X] T029 [US1] Implement the bulb-side test double in `internal/bulb/fakebulb/fakebulb.go` using the fixtures — handshake, keep-alive, command ack, unsolicited state report, and an injectable malformed-output mode. Written from the fixtures, never from assumptions, so the suite cannot agree with a misreading of the protocol
- [X] T030 [P] [US1] Implement the in-memory registry in `internal/registry/registry.go`: `sync.RWMutex`-guarded map keyed by DeviceID, name uniqueness, target resolution (exact name → exact ID → unique case-insensitive prefix, ambiguity is an error listing candidates) per data-model.md
- [X] T031 [P] [US1] Implement persistence in `internal/registry/persist.go`: the `contracts/registry-file.md` schema, atomic write (temp file in the same directory → `fsync` → `os.Rename`), ~2 s debounced coalescing writes, unconditional flush on shutdown; status is never persisted — everything loads as `Disconnected`
- [X] T032 [US1] Implement the listener and per-connection lifecycle in `internal/server/server.go`: goroutine per connection, TLS termination if `contracts/bulb-protocol.md` §1 requires it, read loop bounded by the frame size from §2, context-driven shutdown
- [X] T033 [US1] Implement registration and the connection state machine in `internal/server/registration.go`: unknown DeviceID → `Discovered` and surfaced to the operator (FR-017), known DeviceID → `Connected` on the existing entry (FR-005), populating `Bulb.Capabilities` from T028a at registration and persisting it, and publishing `Connected`/`Disconnected`/`Discovered` events
- [X] T034 [US1] Implement keep-alive handling in `internal/server/keepalive.go` using the interval and byte sequences measured in capture step 14, plus the missed-beat threshold that marks a bulb `Disconnected` (FR-006)
- [X] T035 [US1] Implement state-report ingestion in `internal/server/state.go`: reported state overwrites `Bulb.State` and always wins over `Desired` (FR-019), publishing a `StateChanged` event carrying only the changed fields
- [X] T036 [US1] Wire the server, registry, and event bus together in `cmd/haigosmartd/main.go` behind `-headless` so US1 is demonstrable and soak-testable with no TUI at all

**Checkpoint**: A physical bulb connects, registers, survives keep-alives, and reconnects
after a power cut — **Gate G2**. Quickstart scenarios 1, 2, 6, and 7 pass.

---

## Phase 5: User Story 2 - Operator controls bulbs from the TUI (Priority: P2)

**Goal**: List registered bulbs and change power, brightness, colour, and names from a
terminal interface, with a clear result per command.

**Independent Test**: With one registered bulb, issue on, off, brightness, and colour
commands and confirm the physical bulb changes each time and the TUI reports success or a
clear failure.

**Depends on**: Phase 3; US1's registry and driver plumbing (T030, T032). The command layer
and its tests are written against `fakebulb`, so they do not wait on hardware.

### Tests for User Story 2

- [X] T037 [P] [US2] Write `internal/control/parse_test.go`: table-driven parsing of every command in `contracts/tui-commands.md`, including each documented error string verbatim — unknown command, unknown bulb, ambiguous prefix, out-of-range brightness, malformed colour. The contract's example outputs are the expected values (Constitution III)
- [X] T038 [P] [US2] Write `internal/control/validate_test.go`: every rule in the data-model.md validation table, including colour rejected on a white-only bulb with `ErrUnsupported` rather than a silent drop, colour *allowed* through when `Capabilities.Known` is false, name collisions, and every non-`name` action refused on a `Discovered` bulb with the documented adoption error
- [X] T038a [P] [US2] Write `internal/control/adopt_test.go`: naming a discovered bulb adopts it and it survives a registry reload; naming an adopted bulb renames without re-adopting; adopting with a name already in use is refused; a discovered bulb answers `list` and `info` but nothing else
- [X] T039 [US2] Write `internal/control/command_test.go` against `fakebulb`: successful round trip sets state and marks the command `Accepted`; a disconnected target fails immediately rather than hanging; a non-responding bulb fails at the 3 s timeout (spec edge case); a rejected command leaves no other bulb affected
- [X] T040 [P] [US2] Write `internal/tui/model_test.go`: Bubble Tea `Update` transitions for submit, command history navigation, feed scrolling, and window resize, asserting the view does not corrupt at small terminal sizes (spec edge case)

### Implementation for User Story 2

- [X] T041 [P] [US2] Implement the command parser in `internal/control/parse.go` for the full grammar in `contracts/tui-commands.md`, including CSS basic colour names and `#RRGGBB`
- [X] T042 [P] [US2] Implement validation and target resolution in `internal/control/validate.go` per the data-model.md table, producing errors in the single documented shape: what failed, why, how to fix it. **Includes the adoption gate**: every action except `name` is refused on a `Discovered` bulb with `not adopted yet. run \`name <id> <a-name>\` first`
- [X] T042a [US2] Implement adoption in `internal/control/adopt.go`: `name` on a `Discovered` bulb registers it, persists it via the registry, moves it to `Connected`, and reports `ok  <name>: adopted (was <id>)`; `name` on an adopted bulb is an ordinary rename reporting `renamed from <old>`. One verb, per `contracts/tui-commands.md` §Adoption (FR-011, FR-017)
- [X] T043 [US2] Implement command dispatch in `internal/control/command.go`: resolve target → validate → `Driver.Apply` with a 3 s timeout → set `Outcome` → publish a `CommandResult` event on failure. `Desired` is recorded only to detect divergence and is never displayed as though it were the truth
- [X] T044 [US2] Implement the Bubble Tea model in `internal/tui/model.go`: status bar, scrollable event feed, command prompt, and the keybindings from `contracts/tui-commands.md`. This is the only package in the module permitted to import `bubbletea`
- [X] T045 [US2] Implement rendering in `internal/tui/view.go`: the three output shapes (`ok`/`error`/`info`), the `list` table, `info` detail including any commanded-versus-reported divergence, and reflow on resize
- [X] T046 [US2] Implement `help` in `internal/tui/help.go` complete enough that a first-time operator can list bulbs and change one within two minutes using on-screen text alone (SC-007)
- [X] T047 [US2] Wire the TUI into `cmd/haigosmartd/main.go` as the default mode, with `-headless` retaining the US1 log-only path

**Checkpoint**: Quickstart scenarios 3, 5, 11, and 12 pass. US1 still passes.

---

## Phase 6: User Story 3 - Live state events in the TUI (Priority: P3)

**Goal**: Every state change a bulb reports — including changes made at the wall switch —
appears as a timestamped, attributed event in the feed, without freezing the interface.

**Independent Test**: Power-cycle a registered bulb at the wall and confirm a timestamped
event naming that bulb and its new state appears with no operator command issued.

**Depends on**: Phase 3 (T015, T017); US1's state ingestion (T035); US2's TUI shell (T044).

### Tests for User Story 3

- [X] T048 [P] [US3] Write `internal/tui/feed_test.go`: newest-at-bottom ordering, ring-buffer eviction under sustained load, the dropped-count indicator appearing in the status bar, and the prompt remaining responsive while events stream (FR-015)
- [X] T049 [US3] Write `internal/events/completeness_test.go`: drive 500 state changes through `fakebulb` and assert all 500 reach the structured log correctly attributed, even when the display buffer drops some, and that the reported not-shown count exactly equals the number missing from the feed (amended SC-008, quickstart scenario 9)
- [X] T049a [US3] Write `internal/tui/responsiveness_test.go`: under a sustained burst of events, keystrokes are still accepted and a submitted command dispatches without perceptible delay (SC-009 — the reason the feed drops rather than blocks)
- [X] T050 [P] [US3] Write `internal/events/format_test.go`: every `Kind` renders in the exact line format from `contracts/tui-commands.md`, including multi-field `StateChanged` and the `DuplicateID` warning

### Implementation for User Story 3

- [X] T051 [US3] Implement the feed component in `internal/tui/feed.go`: fixed-capacity ring buffer, newest at bottom, `PgUp`/`PgDn` scrolling, dropped-count surfaced in the status bar rather than hidden
- [X] T052 [US3] Bridge the event bus into Bubble Tea in `internal/tui/subscribe.go` as a `tea.Cmd` feeding `tea.Msg`, so a paused or slow UI cannot back-pressure a bulb's read loop
- [X] T053 [US3] Implement event formatting in `internal/events/format.go` per `contracts/tui-commands.md`, emitting only fields that actually changed
- [X] T054 [US3] Emit `Discovered` events with the "name it to control it" affordance and `DuplicateID` warnings into the feed (FR-017, data-model.md edge case)

**Checkpoint**: Quickstart scenarios 4, 9, and 9a pass. All three stories work independently.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T055 [P] Write `docs/operating.md`: pointing bulb hostnames at the server, running under systemd, where the registry and logs live, and how to recover from a corrupt registry file
- [X] T056 [P] Add doc comments to every exported symbol across `internal/` and `cmd/`, verified with `go doc` (Constitution I)
- [X] T057 Implement the 30-connection soak test in `internal/server/soak_test.go` behind a `-short` skip: 30 `fakebulb` instances, assert zero unexplained disconnections and stable memory (SC-005, FR-020, quickstart scenario 8)
- [~] T058 **POSTPONED (2026-08-28)** — verify zero outbound traffic with the `tcpdump` check from quickstart scenario 2 and record it in `docs/operating.md` (FR-002, SC-001). Not a performance task despite its original "under load" wording, which was a leftover from the removed Principle IV: it is a binary behavioural check that belongs to the feature's core promise. Deferred by the operator once the TUI goal was met; pick it up when convenient
- [X] T059 [P] Audit every user-facing string against `contracts/tui-commands.md` for one vocabulary and one error shape, fixing drift (Constitution III)
- [X] T060 ~~Mandatory benchmarks~~ **no longer required** (Principle IV removed 2026-08-28). `internal/protocol/bench_test.go` and `internal/events/bench_test.go` were written under the old principle and are kept as reference measurements, runnable with `make bench`. No PR needs them and no CI job runs them
- [X] T060a Record the benchmark and soak numbers in `docs/performance.md`. **Amended 2026-08-28**: kept as a reference snapshot for anyone investigating a change that feels slow, not as a baseline a PR must defend
- [X] T060b ~~Profile the read loop and event path under the soak test~~ **dropped** (Principle IV removed 2026-08-28). Profile if and when something actually feels slow; there is no obligation to do it speculatively, which was the point of removing the principle
- [~] T061 **DONE for this deployment (2026-08-28)** — scenarios 1, 3, 4, 5, 6, 7 and 11 passed against the physical lamp. Scenario 2 is deferred with T058. Scenarios 8 and up are **not applicable**: they measure scale and multi-user properties (thirty-bulb week-long soak, 500-event count, burst responsiveness, cold-usability trial, two-model capability comparison) that a single-household adoption does not depend on. The equivalent ground is covered by the automated suite on every run — see quickstart.md

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: T000 first (nothing else can be committed or CI-gated without it), then T001–T006
- **Phase 2 (Protocol Discovery)**: Needs T005 (`docs/capture-setup.md`) and physical bulbs. **Blocks all protocol code** (T027, T028, T029, T032–T035) but nothing else
- **Phase 3 (Foundational)**: Needs Phase 1. **Runs in parallel with Phase 2** — this is the whole point of the split
- **Phase 4 (US1)**: Needs Phase 3 and Gate G1 (T012)
- **Phase 5 (US2)**: Needs Phase 3, plus T030 and T032 from US1
- **Phase 6 (US3)**: Needs Phase 3, plus T035 (US1) and T044 (US2)
- **Phase 7 (Polish)**: Needs the stories you intend to ship

### The critical path

```text
T000 ──► T001 ──► T005 ──► T007 ──► T008 ──► T009(G0) ──► T010 ──► T011 ──► T012(G1) ──► T027 ──► T032 ──► T036(G2)
        │
        └──► T013..T021 (Phase 3, parallel with the capture work)
```

The capture is the long pole and it is gated on operator availability and hardware, not on
engineering effort. Start T007 the same day Phase 1 lands.

### Within each user story

- Tests are written before the implementation they cover and must fail first
- Types before services; services before the TUI
- `fakebulb` (T029) before any test that drives a connection

### Parallel opportunities

- T003–T006 all parallel (after T000)
- T013–T016, T018, T019 all parallel (distinct files)
- T022–T025 all parallel; T030 and T031 parallel
- T037, T038, T040 parallel; T041 and T042 parallel
- T048 and T050 parallel
- Phase 2 (operator) and Phase 3 (engineering) run concurrently by different people

---

## Parallel Example: Phase 3 Foundational

```bash
Task: "Define LightState, RGB, Mode, Capabilities in internal/bulb/state.go"
Task: "Define Bulb and Status enum in internal/bulb/bulb.go"
Task: "Define Event, Kind, FieldChange in internal/events/event.go"
Task: "Define the Driver interface in internal/bulb/driver.go"
```

## Parallel Example: User Story 1 tests

```bash
Task: "Codec fixture tests in internal/protocol/codec_test.go"
Task: "Malformed-frame tests in internal/protocol/codec_malformed_test.go"
Task: "Registry tests in internal/registry/registry_test.go"
Task: "Persistence tests in internal/registry/persist_test.go"
```

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1: Setup (T000 first — the constitution's review and CI gates depend on it)
2. Phase 2 and Phase 3 concurrently — operator captures while engineering builds the foundation
3. Phase 4: US1
4. **STOP and VALIDATE**: quickstart scenarios 1, 2, 6, 7. At this point the bulbs are off
   the vendor cloud and controllable via `-headless` — which is the entire point of the
   feature. The TUI is comfort on top of that
5. Run it for a few days before building more

### Incremental delivery

1. Setup + Foundational + Protocol Discovery → foundation ready
2. US1 → bulbs are local-only → **MVP**
3. US2 → interactive control
4. US3 → live observability
5. Polish → soak, docs, profiling

### Parallel team strategy

- One person runs the capture (Phase 2); it is mostly waiting, not typing
- Another builds Phase 3 in parallel — it shares no files with Phase 2
- After G1: one on `internal/protocol` + `internal/server`, one on `internal/control` +
  `internal/tui` against `fakebulb`

---

## Implementation record (2026-08-27)

Gates **G0**, **G1** and **G2** all passed. The capture showed the bulbs speak
**MQTT 3.1.1 on port 1883**, in the clear that evening, carrying Alibaba Cloud
IoT ("Alink") JSON — close to the best case in research.md §3. No
reverse-engineered framing, no key extraction; research.md §4 was never needed.
(Live hardware later turned out to use TLS 1.2 on the same port; see the
bring-up section at the end of this file.)

Three things the capture changed, none of which the plan could have known:

1. **The hardware is white-only.** Firmware `aigo_light_cct_v4.0.0`: `cct` means
   correlated colour temperature. There is no colour channel and no RGB property
   anywhere in the capture. FR-010's colour control is therefore answered with
   the spec's own documented "does not support colour" path, and the capability
   detection has an `_rgb` branch ready for a model that has one.
2. **Colour temperature is a 0-100 percentage, not Kelvin.** `data-model.md`
   said `ColorTempK uint16`; the wire says 0 = warmest, 100 = coolest. The code
   follows the hardware, and `contracts/bulb-protocol.md` §8 records it.
3. **`Registry` hands out copies, not pointers.** The original design returned
   `*bulb.Bulb`, which made every read outside the lock a data race — caught by
   `-race` during T026. Reads now return snapshots and mutations go by device id.

Remaining tasks: T058 and T061 need physical hardware and are the operator's to do. T060b
(speculative profiling) was dropped on 2026-08-28 along with constitution Principle IV.

## Hardware bring-up (2026-08-28)

**The primary goal — a working TUI driving real bulbs — is met.** A physical
`aigo_light_cct_v4.0.0` lamp connected, was adopted as `headlamp`, and answered `on`,
`off`, `bri` and `temp` from the terminal, with its state changes appearing in the feed.

Four things the capture could not have told us, each found only by pointing the server at
real hardware. Every one was a case of the test double agreeing with a wrong assumption,
which is exactly the risk flagged when `fakebulb` was written:

1. **The bulb speaks TLS.** The capture was genuinely cleartext (`securemode=2`,
   `tls:false` in the flow file), but the field device opens TLS 1.2 on the same port
   1883, with SNI `public.iot-as-mqtt.eu-central-1.aliyuncs.com`. The listener now sniffs
   the first byte and serves both.
2. **It offers only RSA-key-exchange CBC suites** (`0x003D 0x0035 0x003C 0x002F`), which
   modern Go disables by default *and* refuses to select against an ECDSA certificate. The
   self-signed certificate is RSA-2048 and the suites are listed explicitly. The bulb does
   **not** validate the certificate — that is what makes the whole project possible.
3. **Commands carry exactly one property.** The app never bundles, and the hardware
   silently ignores a bundled command: no ack, no state report, just a timeout. Only
   changed properties are sent now, one message each.
4. **The hardware ramps.** Dimming 100→1 fades and reports only when the fade finishes,
   measured at ~4 s. The 3 s timeout was reporting failure for commands that then visibly
   succeeded. It is 10 s now, and commands run off the TUI update loop so a slow one no
   longer freezes the interface (a latent FR-015 violation that a 3 s freeze had hidden).

## Notes

- **T012 is the single riskiest task in this list.** If the protocol turns out to be
  encrypted with an unrecoverable key, work research.md §4 and re-plan. Everything in
  Phases 1, 3, 5, and 6 still holds; only the transport underneath changes
- `[P]` means different files with no incomplete dependency
- `go test ./... -race` must be green before every commit — the concurrency here is real,
  not incidental
- Benchmarks are **not** a requirement (Principle IV was removed on 2026-08-28). `make bench`
  exists and the tests still pass; use them when a change feels slow, ignore them otherwise
- Commit after each task or logical group
- Nothing from `captures/` is ever committed except scrubbed fixtures in
  `internal/protocol/testdata/`
