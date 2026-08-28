---

description: "Task list for 003-headless-deployment"
---

# Tasks: Headless Deployment

**Input**: Design documents from `/specs/003-headless-deployment/`

**Prerequisites**: Features 001 and 002 complete, validated on hardware. At least one adopted
lamp, and its device id to hand (`list` in the terminal shows it under `ID`).

**Tests**: Included and non-optional — Principle II. Two lessons from the previous features
shape what is tested here. First: *a double built from assumptions agrees with those
assumptions* — so admission control is tested through `fakebulb`, which performs a real
CONNECT and expects a real CONNACK, rather than by calling the predicate directly. Second:
*a defect can be invisible in every individual payload and only appear when two are compared*
— so the redaction test greps a whole captured run for the password rather than asserting
one record's shape, and the environment-name test walks `flag.VisitAll` to assert every
setting has a variable, rather than checking the ones someone remembered.

**Organization**: Grouped by user story. Task IDs restart at T001; this is a separate file
from features 001 and 002.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 / US2 / US3 / US4 from spec.md
- Exact file paths in every task

## Path Conventions

Single Go module, unchanged layout, **no new `go.mod` entry** (research.md §1). New packages:
`internal/config`, `internal/logging`. Changed: `cmd/haigosmartd`, `internal/server`,
`internal/events`, `internal/registry`, `internal/protocol`, `internal/control` (one
export). Untouched: `internal/lights`, `internal/hass`, `internal/mqtt`, `internal/tui`,
`internal/bulb`.

---

## Phase 1: Setup

**Purpose**: Small mechanical additions the later phases need. No behaviour changes; the
existing suite must stay green after each one.

- [X] T001 [P] Add `ConnackNotAuthorized` (return code `0x05`) alongside `ConnackAccepted` in `internal/protocol/mqtt.go`, with a doc comment citing MQTT 3.1.1 §3.2.2.3, and extend `ConnackReason` to name it
- [X] T002 [P] Add the `Rejected` kind to `internal/events/event.go`: constant, `String()` → `"rejected"`, and `Text()` → `"rejected: not in the configured lamp set"` per `data-model.md`
- [X] T003 [P] Add `OnError func(error)` to `Store` in `internal/registry/persist.go` and call it from the discarded background save (`_ = s.Flush()`), so a read-only registry becomes visible instead of silent (research.md §7)
- [X] T004 [P] Export the terminal verb check from `internal/control/parse.go` as `IsVerb(string) bool` (currently unexported `isVerb`), so configuration can refuse a lamp name the terminal could not address

**Checkpoint**: `go test ./... -race` green, nothing observable changed.

---

## Phase 2: Foundational — the record vocabulary

**Purpose**: The record shape both US1 and US3 emit. **Blocks US1 and US3.**

- [X] T005 Rework `(*Bus).log` in `internal/events/bus.go` to emit a stable `msg` per kind with variable text moved into fields, exactly per the table in `contracts/log-records.md` — `{"msg":"bulb disconnected","detail":"no keep-alive for 180s"}`, never `{"msg":"disconnected (no keep-alive for 180s)"}`. `Event.Text()` is untouched: the terminal renders from the event, not from the record
- [X] T006 Set the level per kind in the same function per the contract table, adding `Rejected` at WARN alongside the existing `ProtocolError` and `DuplicateID`
- [X] T007 Write `internal/events/record_test.go`: a table over every `Kind` asserting its `msg`, its level, and that `msg` contains no value from the event — construct each kind with a distinctive `Detail` and assert the detail appears only in the field. A new kind added without a mapping must fail this test, not default silently

**Checkpoint**: record vocabulary fixed and enforced by test. User stories can begin.

---

## Phase 3: User Story 1 — Run unattended with machine-readable logs (P1) 🎯 MVP

**Goal**: `-headless` produces JSON lines on standard output, with a compact timestamp and
the elapsed time on every record, and nothing else on that stream.

**Independent Test**: Start with no terminal, power a known lamp on and off, confirm every
line parses as JSON and carries `time`, `since`, `level`, `msg` — quickstart scenario 4.

### Tests for User Story 1

- [X] T008 [P] [US1] Write `internal/logging/logging_test.go` for the time format: a fixed instant renders as `2006-01-02 15:04:05.000`, and `since` renders from a fixed start as `1m12.345s`, truncated to milliseconds. Assert on the JSON output, not on a helper's return value
- [X] T009 [P] [US1] Add a test to `internal/logging/logging_test.go` asserting every record carries `since` regardless of which goroutine logs it: log from several goroutines under `-race`, then assert every captured line has the field and that values never decrease
- [X] T010 [P] [US1] Add a test asserting a failing writer terminates: inject a writer that always errors and a fake exit function, then assert the exit function was called once with a non-zero status and that the message reached the error stream

### Implementation for User Story 1

- [X] T011 [US1] Create `internal/logging/logging.go` with `New(w io.Writer, level slog.Level, start time.Time) *slog.Logger`: a `JSONHandler` whose `ReplaceAttr` rewrites the `time` attribute to the compact layout, wrapped by a small handler that adds `since` to every record (research.md §3 — computed per record, so it cannot be a `With` attribute)
- [X] T012 [US1] Add the fatal writer to `internal/logging/logging.go`: a wrapper that on any write error prints one line to `os.Stderr` and exits non-zero, per `contracts/log-records.md`. Keep the exit function injectable so T010 can test it
- [X] T013 [US1] Replace `newLogger` in `cmd/haigosmartd/main.go` with `logging.New`, sending headless output to `os.Stdout` through the fatal writer and keeping the interactive path writing to a file. Delete `filepathJoinTemp` if `logging` absorbs it
- [X] T014 [US1] Emit the shutdown record in `cmd/haigosmartd/main.go` on signal — `msg:"shutting down"` with the signal — and make the headless path flush the registry before returning, so FR-007's "persist what it knows" is a code path and not an accident of timing
- [X] T015 [US1] Audit `cmd/haigosmartd/main.go` for anything that writes to standard output outside the record stream (`fmt.Print*`) and route it to standard error instead (FR-005). The startup failure path in `main` already uses stderr; confirm rather than assume

**Checkpoint**: US1 complete. `./haigosmartd -headless | jq .` runs clean with no configuration
work done yet — quickstart scenario 4 passes. **This is the MVP.**

---

## Phase 4: User Story 2 — Configure everything from the environment (P1)

**Goal**: Every setting reachable as `HAIGOSMART_*`, flags still win, invalid settings refused
before any socket opens, credentials unprintable.

**Independent Test**: Start with an empty command line and only environment variables; the
instance runs exactly as configured — quickstart scenarios 1–3.

### Tests for User Story 2

- [X] T016 [P] [US2] Write `internal/config/config_test.go` asserting **every** flag has a working environment variable, by walking the flag set rather than listing names: for each flag, set its derived variable to a valid value and assert the loaded `Config` carries it. A setting added later without a variable fails here
- [X] T017 [P] [US2] Add a precedence test: variable set and flag given → flag wins, and exactly one override record naming the setting; variable only → variable wins; neither → today's default. Assert the override record does **not** contain the value
- [X] T018 [P] [US2] Add a redaction test: load a config with a password, render it through `slog` at every level into a buffer, and assert the password string appears nowhere — including in the error produced when another setting is invalid (SC-006)
- [X] T019 [P] [US2] Write `internal/config/lamps_test.go` as a table over every row of the lamp-set table in `contracts/configuration.md`, asserting the exact error text for each malformed input. These strings are the contract; the test is the enforcement
- [X] T020 [P] [US2] Add a validation test covering each rule in `data-model.md`: inverted Kelvin, non-positive timeout, unparseable listen address, unparseable duration. Each must name the setting and the received value

### Implementation for User Story 2

- [X] T021 [US2] Create `internal/config/config.go`: the `Config` struct from `data-model.md`, a `newFlagSet` declaring every setting once with its default and help text, and `Load(args []string, env func(string) string) (Config, error)` implementing defaults → environment → flags exactly as research.md §1 step 1–4 describes
- [X] T022 [US2] Add `envName(flagName string) string` to `internal/config/config.go` — `HAIGOSMART_` + uppercase + `-`→`_` — as the single derivation, used by both `Load` and the documentation generator in T038
- [X] T023 [US2] Create `internal/config/lamps.go`: parse `deviceID=name` pairs, trimming whitespace, reporting position and content on every malformed entry, and rejecting duplicate ids, duplicate names, and names for which `control.IsVerb` is true
- [X] T024 [US2] Add `(Config).Validate() error` to `internal/config/config.go` per the `data-model.md` rules, and `(Config).LogValue() slog.Value` rendering the password as `(set)`/`(unset)` — redaction by construction, so no future caller can print it
- [X] T025 [US2] Replace the flag block in `cmd/haigosmartd/main.go` with `config.Load(os.Args[1:], os.Getenv)`, validating before the record destination is opened and before the listener, per the startup order in `contracts/configuration.md`
- [X] T026 [US2] Emit the startup record in `cmd/haigosmartd/main.go` — `msg:"starting"` with the whole `Config` as one attribute, which is redacted by T024 (FR-015)
- [X] T027 [US2] Wire `Store.OnError` (T003) in `cmd/haigosmartd/main.go` to report the first failure at WARN and later ones at DEBUG, using `sync.Once` for the first

**Checkpoint**: US1 + US2 give a fully configurable unattended instance. Quickstart scenarios
1–4 pass. Lamp membership is not yet enforced.

---

## Phase 5: User Story 3 — Refuse unknown lamps while unattended (P2)

**Goal**: The configured lamp set is authoritative; anything else is refused, recorded, and
leaves nothing behind.

**Independent Test**: One configured lamp and one unknown one both connect; the first works
end to end, the second is refused and absent from the registry and from Home Assistant —
quickstart scenarios 5–7.

### Tests for User Story 3

- [X] T028 [P] [US3] Write `internal/server/admit_test.go` driving `fakebulb` through a real CONNECT: an admitted id receives CONNACK `0x00` and proceeds; a rejected id receives CONNACK `0x05` and the connection closes. Assert through the wire, not by calling the predicate
- [X] T029 [P] [US3] Add to `internal/server/admit_test.go`: after a rejection, the registry contains no entry for that device id, and neither does the persisted file after a flush (FR-017)
- [X] T030 [P] [US3] Add a rate-limiting test using the server's swappable clock: fifty rejections from one device id inside five minutes produce exactly one record beyond the first sighting, and the repeat record carries the suppressed count (FR-017a)
- [X] T031 [P] [US3] Write `internal/registry/declare_test.go`: `Declare` creates a disconnected entry when absent, renames when the stored name differs, is idempotent when both agree, and never clears state or capabilities it already had
- [X] T032 [P] [US3] Add a startup test in `cmd/haigosmartd` or `internal/config` covering FR-019: headless with an empty lamp set is refused, with a message saying an unattended instance must be told which lamps to serve

### Implementation for User Story 3

- [X] T033 [US3] Add `Admit func(deviceID string) bool` to `Server` in `internal/server/server.go`, documented as nil-means-admit-all so interactive behaviour is preserved by construction rather than by a branch someone must remember
- [X] T034 [US3] Consult `Admit` in `internal/server/session.go` immediately after `IdentityFromConnect` succeeds and **before** the CONNACK and before `register`, sending `ConnackNotAuthorized` and returning when refused (research.md §5)
- [X] T035 [US3] Add `noteRejection(deviceID string, now time.Time) (report bool, suppressed int)` to `internal/server/server.go`, mirroring the existing `noteTakeover` shape: first sighting reported, then at most one per five minutes carrying the count since the last report
- [X] T036 [US3] Publish the rejection as an `events.Rejected` event from `internal/server/session.go`, with the remote address in `Detail` and, on a suppressed-then-reported repeat, the attempt count and the time of the first suppressed attempt
- [X] T037 [US3] Add `Declare(deviceID, name string) (created bool, renamed bool)` to `internal/registry/registry.go`, creating a `Disconnected` entry when absent and applying the configured name when it differs (FR-021, FR-022)
- [X] T038 [US3] In `cmd/haigosmartd/main.go`, after loading the registry and before opening the listener: `Declare` every configured lamp with a `lamp configured` record each, report every registry entry absent from the configuration once at WARN as `registry lamp not configured`, and set `srv.Admit` to the configured set when headless and leave it nil otherwise
- [X] T039 [US3] Add the FR-019 rule to `(Config).Validate()` in `internal/config/config.go`: headless with no lamps is refused, naming `HAIGOSMART_LAMPS` and explaining that an instance which would reject every connection is a configuration mistake

**Checkpoint**: all three runtime stories complete. Quickstart scenarios 1–7 pass.

---

## Phase 6: User Story 4 — Document the path (P2)

**Goal**: One ordered document from a lamp on the vendor's cloud to an unattended instance.

**Independent Test**: Someone who has not seen the project follows it end to end and reaches a
running unattended instance controlling a real lamp, without asking a question (SC-007).

- [X] T040 [P] [US4] Write `docs/deploying.md` part one — the once-only path: redirect the lamp (linking `docs/capture-setup.md`), run interactively, adopt and name the lamp, read its device id from `list`, verify control. Mark plainly that this part needs a terminal and happens once per lamp
- [X] T041 [US4] Write `docs/deploying.md` part two — the repeatable path: turn the adopted lamps into `HAIGOSMART_LAMPS`, choose the remaining settings, start unattended, and confirm from the records that lamps are served. Include the exact commands, with a worked example carrying real-looking device ids
- [X] T042 [US4] Add the settings table to `docs/deploying.md` from `contracts/configuration.md` — every setting with flag, variable, default, and whether it is required (FR-025) — plus how credentials are supplied without landing in a committed file or a record (FR-026)
- [X] T043 [US4] Add a "reading the records" section to `docs/deploying.md`: the record shape, what a healthy startup sequence looks like line by line, and the three records that mean something is wrong — `bulb rejected`, `registry lamp not configured`, `saving the registry failed` (FR-027, SC-009)
- [X] T044 [P] [US4] Add `configs/haigosmart.env.example` listing every variable with its default, commented, so the starting point is a file to copy rather than a table to retype
- [X] T045 [P] [US4] Update `docs/operating.md` and `README.md` to point at `docs/deploying.md` for the unattended path, and note in `docs/homeassistant.md` that an unattended instance publishes its configured lamps at startup, before they connect

**Checkpoint**: G3 ready to attempt.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T046 Add the migration note required by Principle III for the record format change (T005): a short section in `docs/deploying.md` stating that `msg` no longer carries interpolated values and that anything grepping the old log file text must move to the `detail` field
- [X] T047 [P] Run `gofmt -l .`, `go vet ./...`, and `go test ./... -race`; all must be clean
- [X] T048 [P] Confirm `go.mod` is unchanged by this feature — the dependency claim in plan.md is testable, so test it with `git diff --exit-code go.mod go.sum`
- [X] T049 Walk quickstart scenarios 1–4 (**G1**) and record the result in `quickstart.md`
- [ ] T050 Walk quickstart scenarios 5–9 on real hardware, two lamps, one configured and one not (**G2**) and record the result in `quickstart.md`
- [X] T051 Walk quickstart scenario 10 — interactive mode unchanged, existing suites passing without modification (SC-010)
- [ ] T052 **G3**: hand `docs/deploying.md` to someone who has not seen the project and have them reach a running unattended instance controlling a real lamp from Home Assistant, then fix whatever they had to ask about

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies; all four tasks parallel
- **Foundational (Phase 2)**: needs T002 from Setup; **blocks US1 and US3**
- **US1 (Phase 3)**: needs Phase 2
- **US2 (Phase 4)**: needs Phase 1 only — independent of US1, though they meet in `main.go`
- **US3 (Phase 5)**: needs Phase 2, plus T021–T024 from US2 for the configured set, and T004 for the verb check
- **US4 (Phase 6)**: needs US1–US3 done, since it documents their behaviour
- **Polish (Phase 7)**: last

### User Story Dependencies

- **US1 (P1)**: independent. Delivers a usable unattended instance on its own — the MVP
- **US2 (P1)**: independent of US1. Both touch `cmd/haigosmartd/main.go`, so if worked in
  parallel, one of them rebases
- **US3 (P2)**: depends on US2 for the lamp set. Not independently deliverable, and the spec
  says so — it is the story that makes an unattended instance predictable, not the one that
  makes it run
- **US4 (P2)**: depends on all three

### Within Each User Story

Tests first and failing, then implementation. In US2 and US3 the tests encode contract
strings verbatim; if implementation and test disagree, the contract decides which one is
wrong.

### Parallel Opportunities

- All of Phase 1 (T001–T004): four different files
- Within US1: T008–T010 together, then T011–T012 in one file, then main.go
- Within US2: T016–T020 together — five test files or five independent tables
- Within US3: T028–T032 together
- Within US4: T040 and T044–T045 together; T041–T043 share `docs/deploying.md`
- **US1 and US2 in parallel** if two people are working, up to the `main.go` tasks (T013–T015
  and T025–T027), which must serialize

---

## Parallel Example: User Story 2

```bash
# The five test tasks, all independent:
Task: "Every flag has a working environment variable, by walking the flag set — T016"
Task: "Precedence: flag beats variable beats default, one override record — T017"
Task: "Password appears nowhere at any level, including in errors — T018"
Task: "Lamp-set parse table, exact error strings from the contract — T019"
Task: "Validation rules each name the setting and the received value — T020"
```

---

## Implementation Strategy

### MVP first (US1 only)

1. Phase 1 → Phase 2 → Phase 3.
2. **Stop and validate**: `./haigosmartd -headless | jq .` on a real lamp.
3. At this point the server runs unattended and is observable. It is still configured by
   flags and still adopts whatever connects — deployable for a trusted network, and honestly
   described as such.

### Incremental delivery

1. Setup + Foundational → record vocabulary fixed
2. + US1 → unattended and observable (**MVP**)
3. + US2 → configurable from the environment; the pair is the minimum viable deployment
4. + US3 → predictable: the lamp set is the configuration and cannot drift
5. + US4 → someone other than the author can do all of it

### Notes

- No new dependency. T048 makes that claim a test rather than an intention
- Interactive mode is a regression surface in every phase: T051 exists because SC-010 is a
  promise about code nobody in this feature is deliberately touching
- The two `main.go` clusters (T013–T015, T025–T027) are the only real serialization point
- Commit after each task or logical group

---

## Hardware bring-up (2026-08-28)

One defect surfaced against a real Home Assistant during scenario 8, invisible to the whole
green suite:

**A lamp whose state survived a restart unchanged never became available in Home
Assistant.** The bridge treats a state report as its proof that a lamp is really there —
deliberately, since feature 002's FR-010 says persisted state is a memory, not a report. But
`handlePropertyPost` published `StateChanged` only when a value actually differed. Once
lamps are declared from a persisted registry, the normal reconnect reports exactly what the
registry already holds, so nothing was published and the entity stayed unavailable forever.

Why every test missed it: feature 002 was validated by adopting a lamp live in the terminal,
which publishes `Renamed` and takes a different path entirely, and against a registry whose
stored state differed from what the lamp then reported. The no-change reconnect is the case
this feature *created* and the only one that triggers it.

Fixed in `internal/server/session.go`: the first report of each connection is published even
when it changes nothing — a bulb confirming what it is, which is genuinely different
information from a value remembered across a restart. Regression test
`TestReconnectReportsStateEvenWhenNothingChanged`, verified to fail without the fix.

## Status (2026-08-28)

50 of 52 done, plus one hardware defect found and fixed (see above). The two open tasks both need something this session cannot supply:

- **T050 (G2)** — needs a lamp, and specifically a *second* lamp deliberately left out of
  the configuration, to prove the refusal path against real hardware rather than against
  `fakebulb`. Everything below it is green: the software half of scenario 7 was exercised
  with a hand-written registry file, and admission is covered by a real CONNECT/CONNACK
  exchange in `internal/server/admit_test.go`.
- **T052 (G3)** — needs a person who has not seen this project to walk `docs/deploying.md`.
  The value is in what they have to ask about; the author cannot run it.

Both are recorded in `quickstart.md` rather than assumed passed. The lesson from feature
001 applies unchanged: `fakebulb` agrees with the assumptions it was built from, so a green
suite is not a hardware result.
