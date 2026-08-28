# Feature Specification: Local Replacement Server for Aigo Smart Bulbs

**Feature Branch**: `001-local-bulb-server`

**Created**: 2026-08-27

**Status**: Draft

**Input**: User description: "Build an application that can act as a replacement server for my local Aigo Smart Bulbs. The Aigo Smart bulbs would then only communicate internally with the replacement server and not with the remote servers. The server should also have a simple tui that accepts commands to change the state of each registered light bulb. It should also emit events in the tui on registered state events by registered light bulbs."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Bulbs connect to the local server instead of the vendor cloud (Priority: P1)

A household owner runs the replacement server on a machine on their LAN and redirects
the bulbs' cloud destination to it (via local DNS override or router rule). Each bulb
connects, completes its normal startup handshake, and is listed by the server as a
registered device. No bulb traffic leaves the local network.

**Why this priority**: Without bulbs successfully connecting and staying connected, no
other capability exists. This alone delivers value: the bulbs keep working while their
cloud dependency is removed.

**Independent Test**: Point one bulb at the server, power-cycle it, and confirm it
appears in the registered device list, stays connected across at least one keep-alive
cycle, and that a network capture shows no outbound connections from the bulb to any
non-local address.

**Acceptance Scenarios**:

1. **Given** the server is running and a bulb's cloud hostname resolves to the server,
   **When** the bulb powers on, **Then** the bulb completes its handshake and is listed
   as a registered device with a stable identifier.
2. **Given** a bulb is connected, **When** the vendor's remote servers are unreachable
   from the network, **Then** the bulb remains connected and controllable.
3. **Given** a bulb loses power or network, **When** it comes back, **Then** it
   re-registers automatically under the same identifier without operator action.
4. **Given** an unknown device attempts to connect, **When** the handshake completes,
   **Then** the server records it as a newly discovered device and surfaces it to the
   operator rather than silently accepting or dropping it.

---

### User Story 2 - Operator controls bulbs from the TUI (Priority: P2)

The operator uses the server's terminal interface to list registered bulbs and issue
commands changing their state: on/off, brightness, and color, targeting one bulb by
name or identifier.

**Why this priority**: Control is the primary day-to-day reason to run the server, but
it depends on registration (P1) existing first.

**Independent Test**: With one registered bulb, issue an on, off, brightness, and color
command from the TUI and visually confirm the physical bulb changes each time, plus
confirm the TUI reports success or a clear failure per command.

**Acceptance Scenarios**:

1. **Given** a registered bulb, **When** the operator issues an "on" command for it,
   **Then** the bulb turns on and the TUI confirms the command was accepted.
2. **Given** a registered bulb, **When** the operator sets brightness to a value inside
   the supported range, **Then** the bulb changes brightness to that level.
3. **Given** a registered bulb, **When** the operator sets a color, **Then** the bulb
   changes to that color.
4. **Given** a command naming an unknown or disconnected bulb, **When** it is submitted,
   **Then** the TUI shows an error stating which bulb was not reachable and what to do,
   and no other bulb is affected.
5. **Given** an unrecognized or malformed command, **When** it is submitted, **Then**
   the TUI shows the accepted command syntax and changes no bulb state.

---

### User Story 3 - Operator sees live state events in the TUI (Priority: P3)

State changes reported by registered bulbs — whether caused by the operator, a physical
wall switch, or a bulb's own power-on default — appear as timestamped events in the TUI
event feed, identifying the bulb and what changed.

**Why this priority**: Observability makes the system trustworthy and debuggable, but
the system is already useful for control without it.

**Independent Test**: Power-cycle a registered bulb at the wall and confirm a
timestamped event naming that bulb and its new state appears in the TUI feed without
the operator issuing any command.

**Acceptance Scenarios**:

1. **Given** a registered bulb, **When** it reports a state change, **Then** a
   timestamped event naming the bulb and the changed attributes appears in the feed.
2. **Given** the operator changed a bulb via the TUI, **When** the bulb reports the
   resulting state, **Then** the event feed reflects the confirmed new state.
3. **Given** a bulb connects or disconnects, **When** that happens, **Then** a
   connection or disconnection event for that bulb appears in the feed.
4. **Given** many events arrive rapidly, **When** the feed fills, **Then** the newest
   events remain visible and older ones scroll away without the interface freezing or
   losing the ability to accept commands.

---

### Edge Cases

- A bulb connects but never completes its handshake, or sends malformed data: the server
  drops that connection, logs the reason, and stays available to all other bulbs.
- Two bulbs report the same identifier: the server keeps them distinguishable and warns
  the operator rather than merging or overwriting one silently.
- A command is issued while a bulb is mid-reconnect: the operator gets a clear "not
  currently connected" result rather than a silent no-op or an indefinite hang.
- The server restarts: previously registered bulbs are still known by name after restart,
  and reconnect without needing to be re-added by the operator.
- A bulb's reported state disagrees with the last commanded state (e.g. someone used the
  wall switch): the bulb's reported state wins and the discrepancy is visible in the feed.
- The operator's terminal window is resized or is very small: the interface stays usable
  and does not corrupt its display.
- Multiple bulbs report events simultaneously: every event is recorded in the feed, none
  are dropped or attributed to the wrong bulb.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST accept and complete inbound connections from Aigo smart bulbs
  using the same protocol exchange the bulbs already perform with the vendor's servers,
  so that no firmware change or bulb modification is required.
- **FR-002**: System MUST operate entirely on the local network, requiring no outbound
  connection to any vendor or third-party service during normal operation.
- **FR-003**: System MUST maintain a registry of bulbs, each with a stable identifier, an
  operator-assignable display name, connection status, and last known state.
- **FR-004**: System MUST persist the bulb registry (identifiers, names, last known state)
  across server restarts.
- **FR-005**: System MUST automatically re-register a returning bulb to its existing
  registry entry rather than creating a duplicate.
- **FR-006**: System MUST keep bulb connections alive across idle periods, responding to
  whatever keep-alive the bulbs expect so they do not disconnect or fall back to a
  remote server.
- **FR-007**: Operators MUST be able to list all registered bulbs with their status and
  current state from the terminal interface.
- **FR-008**: Operators MUST be able to turn a specified bulb on or off from the terminal
  interface.
- **FR-009**: Operators MUST be able to set a specified bulb's brightness from the
  terminal interface.
- **FR-010**: Operators MUST be able to set a specified bulb's color from the terminal
  interface.
- **FR-011**: Operators MUST be able to assign a human-readable name to a registered bulb
  and target commands by that name.
- **FR-012**: System MUST report, per command, whether it was accepted by the target bulb
  or failed, including a reason on failure.
- **FR-013**: System MUST display a live, timestamped event feed of state changes reported
  by registered bulbs, identifying the bulb and the changed attributes.
- **FR-014**: System MUST emit connection and disconnection events for registered bulbs
  into the same feed.
- **FR-015**: System MUST keep the terminal interface responsive to operator input while
  events are arriving and commands are in flight.
- **FR-016**: System MUST reject malformed commands and malformed bulb messages without
  terminating the server or affecting other bulbs, and MUST state what was wrong.
- **FR-017**: System MUST surface newly discovered (not-yet-registered) bulbs to the
  operator so they can be named and controlled.
- **FR-018**: System MUST record server-side logs of connections, commands, and protocol
  errors sufficient to diagnose a misbehaving bulb after the fact.
- **FR-019**: System MUST treat state reported by a bulb as authoritative over the last
  commanded state when the two differ.
- **FR-020**: System MUST support at least 30 concurrently connected bulbs on a typical
  home network without operator-visible degradation. *Scope note (2026-08-28): verified by
  the automated soak test; the real deployment is far smaller and this figure exists to
  ensure the design does not assume a single bulb.*

### Key Entities

- **Bulb**: A physical Aigo smart bulb known to the server. Attributes: stable device
  identifier, operator-assigned display name, connection status (connected /
  disconnected / newly discovered), last known power state, brightness, color, and time
  of last contact.
- **Command**: An operator-issued intent to change one bulb's state. Attributes: target
  bulb, requested change (power / brightness / color), issue time, and outcome
  (accepted / failed with reason).
- **State Event**: A record of something a bulb reported or a connection transition.
  Attributes: timestamp, source bulb, event kind (state change / connected /
  disconnected / error), and the changed attributes or error detail.
- **Registry**: The persisted collection of known bulbs and their names, surviving
  server restarts.

## Success Criteria *(mandatory)*

### Measurable Outcomes

> These are observable outcomes an operator checks by hand against real bulbs
> (see `quickstart.md`). They are not benchmark thresholds: the project
> constitution has no performance principle as of 2026-08-28, so nothing here
> gates a merge. The timings below describe what "working" looks like from the
> operator's chair, which is a user-experience question.

- **SC-001**: With the server running, a network capture over a 24-hour period shows zero
  successful connections from any registered bulb to a non-local address.
- **SC-002**: A powered-on bulb becomes visible and controllable in the interface within
  30 seconds of power-up, without operator action.
- **SC-003**: A bulb visibly responds to an operator command within 1 second of the
  command being submitted, on a typical home LAN.
- **SC-004**: A state change made outside the interface (e.g. wall switch) appears in the
  event feed within 5 seconds.
- **SC-005**: The server sustains 30 connected bulbs with zero unexplained disconnections
  attributable to the server and no operator intervention. *Scope note (2026-08-28): the
  actual deployment is one household with a handful of bulbs, so this is a headroom claim
  rather than an operational requirement. It is verified by the automated soak test rather
  than by a week-long hardware run.*
- **SC-006**: After a server restart, all previously registered bulbs reappear with their
  assigned names and reconnect within 60 seconds, with no re-naming required.
- **SC-007**: A first-time operator can list bulbs and change one bulb's state within
  2 minutes of opening the interface, using only its on-screen help.
- **SC-008**: 100% of state events reported by bulbs during a test run are recorded and
  attributed to the correct bulb, and are retrievable afterwards. The visible feed shows
  the most recent events; when events arrive faster than the feed can display them, the
  interface states how many were not shown rather than dropping them silently, and none of
  those events are lost from the record.
- **SC-009**: Under a sustained burst of state events, the operator can still type and
  submit a command without perceptible delay — a busy feed never blocks control.

## Assumptions

- The bulbs' cloud traffic can be redirected to the replacement server by the operator
  using local network controls (DNS override, hosts entry, or router rule); changing bulb
  firmware is out of scope.
- The bulbs' protocol (transport, message framing, and any handshake or encryption) can be
  determined by observing the bulbs' own traffic; protocol discovery is part of the work,
  and the server must interoperate with unmodified bulbs.
- The operator runs the server on a machine on the same LAN as the bulbs, which stays
  powered on.
- A single trusted operator uses the system on a trusted home network; no multi-user
  accounts, roles, or authentication of the operator are required for v1.
- Scope for v1 is on/off, brightness, and color control plus a live event feed. Scheduling,
  scenes, groups, automation rules, mobile apps, voice-assistant integration, and
  smart-home hub integrations are out of scope.
- The vendor's mobile app is not expected to keep working once bulbs are redirected; the
  terminal interface is the control surface.
- Bulbs that do not support color (e.g. white-only models) accept power and brightness
  commands, and the interface reports color commands to them as unsupported rather than
  failing silently.
- Persistence is local to the server machine (a file or embedded store); no external
  database service is assumed.
