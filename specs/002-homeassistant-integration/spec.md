# Feature Specification: Home Assistant Integration

**Feature Branch**: `002-homeassistant-integration`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "I want to integrate the aigo smart lamps using this tool into my homeassistant application. I want to control the lamps from there (including having a default ui entry in homeassistant that only displays the exact capabilities of the lamp) and also, if the lamp starts up and the state changed since the last connection with the lamp, the initial state of the lamp should be reflect in home assistant."

## Context

Feature 001 delivered a local replacement server: the Aigo bulbs connect to it instead of
the vendor cloud, and an operator drives them from a terminal. This feature makes that
server a citizen of the household's Home Assistant installation, so the lamps can be used
alongside every other device — in dashboards, automations, scenes, and voice assistants —
rather than only from a terminal.

The terminal interface remains. It is the tool for adopting a new bulb and for diagnosing
one that misbehaves; Home Assistant becomes the everyday surface.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Lamps appear in Home Assistant and can be controlled (Priority: P1)

The household owner opens Home Assistant and finds each adopted lamp listed as a device,
with no manual configuration. Turning it on and off, and setting its brightness and white
warmth, works from a dashboard card, from an automation, and from a voice assistant.

**Why this priority**: This is the feature. Without lamps appearing and responding, none of
the rest has anywhere to happen. It alone delivers the value: the lamps stop being a
separate terminal-only system and join the house.

**Independent Test**: Adopt one lamp in the terminal, open Home Assistant, and confirm it
appears without editing any configuration file. Toggle it from a dashboard card and watch
the physical lamp respond.

**Acceptance Scenarios**:

1. **Given** a lamp adopted in the terminal, **When** Home Assistant is opened, **Then**
   the lamp appears as a device with a light entity, without manual configuration.
2. **Given** the lamp appears in Home Assistant, **When** the owner turns it on from a
   dashboard, **Then** the physical lamp turns on and the entity shows it as on.
3. **Given** the lamp is on, **When** the owner sets brightness from Home Assistant,
   **Then** the physical lamp changes to that brightness.
4. **Given** the lamp supports white warmth, **When** the owner changes colour temperature
   from Home Assistant, **Then** the physical lamp changes accordingly.
5. **Given** an automation that turns the lamp on at sunset, **When** sunset passes,
   **Then** the lamp turns on with no human present.
6. **Given** a lamp that has been renamed in the terminal, **When** Home Assistant next
   sees it, **Then** it is still the same device and its history and automations survive.

---

### User Story 2 - The interface shows only what the lamp can actually do (Priority: P2)

The lamp's entry in Home Assistant offers exactly the controls the hardware supports. A
white-only lamp shows brightness and warmth, and no colour wheel. The owner is never
presented with a control that does nothing.

**Why this priority**: Controls that silently do nothing are the most common complaint
about generic smart-home integrations, and this project already knows precisely what each
lamp supports. Delivering P1 without this would show a colour picker on a lamp with no
colour channel — worse than not integrating at all, because it teaches the owner to
distrust the interface.

**Independent Test**: Open the lamp's entity in Home Assistant and confirm the controls
match the lamp's real capabilities: brightness and warmth present, colour absent for a
white-only model. Every control shown produces a visible change on the lamp.

**Acceptance Scenarios**:

1. **Given** a white-only lamp, **When** its entity is opened, **Then** brightness and
   colour-temperature controls are offered and no colour control is.
2. **Given** a lamp whose warmth range is known, **When** the owner adjusts warmth,
   **Then** the offered range matches what the lamp accepts, with no dead zones at either
   end.
3. **Given** a lamp whose capabilities could not be determined, **When** its entity is
   opened, **Then** it offers the controls that are certain and the uncertainty is visible
   to the owner rather than guessed at.
4. **Given** a lamp with a brightness floor below which it switches off, **When** the owner
   drags brightness to minimum, **Then** the result matches what the lamp actually does.

---

### User Story 3 - State the lamp reports on reconnect is reflected (Priority: P3)

A lamp that was switched at the wall, or that was changed while the server was down, comes
back reporting its true state. Home Assistant shows that state, not the state it last
remembered. Availability is honest too: a lamp that is unplugged shows as unavailable
rather than as whatever it was last seen doing.

**Why this priority**: Wrong state is worse than no state — an automation reading "off" for
a lamp that is on will act incorrectly, and the owner cannot tell by looking at the app.
This depends on P1 existing, but the system is usable for manual control without it.

**Independent Test**: Turn the lamp off at the wall, wait for Home Assistant to show it
unavailable, switch it back on, and confirm Home Assistant reflects the state the lamp
reports on reconnect without anyone touching a control.

**Acceptance Scenarios**:

1. **Given** a lamp that was changed at the wall switch while connected, **When** it
   reports the change, **Then** Home Assistant reflects it without an operator command.
2. **Given** a lamp that was powered off and back on, **When** it reconnects and reports
   its startup state, **Then** Home Assistant shows that reported state even if it differs
   from what was last known.
3. **Given** a lamp that is unplugged, **When** it stops responding, **Then** its entity
   becomes unavailable rather than continuing to show a stale value.
4. **Given** Home Assistant is restarted, **When** it comes back, **Then** each lamp's
   state is what the lamp reports, not a value restored from before the restart.
5. **Given** the replacement server is stopped, **When** Home Assistant looks at the lamps,
   **Then** they show as unavailable, and they recover on their own when the server returns.

---

### Edge Cases

- A bulb is discovered but not yet adopted in the terminal: it does not appear in Home
  Assistant, because an unnamed device would clutter the house with entries the owner
  cannot identify. Adoption remains a deliberate act.
- A lamp is renamed in the terminal after Home Assistant already knows it: the device
  remains the same one, so history, dashboards, and automations are not broken. The name
  the owner set inside Home Assistant is not overwritten.
- A lamp is removed from the registry: its Home Assistant entry is removed too, rather than
  lingering forever as unavailable.
- A command is sent from Home Assistant to a lamp that has gone offline in the meantime:
  the failure is visible in Home Assistant, not silently swallowed.
- A command is sent while the lamp is still confirming a previous one: the lamp's own
  reported state remains the source of truth and the display settles on it.
- The lamp is changed from the terminal and from Home Assistant at nearly the same moment:
  the last change the lamp actually reports is what both surfaces show.
- The replacement server restarts while Home Assistant keeps running: entities recover
  without the owner re-adding anything.
- Two lamps are given the same name: they remain distinguishable in Home Assistant.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST make every adopted lamp available to Home Assistant automatically,
  with no per-lamp editing of Home Assistant configuration files.
- **FR-002**: Each lamp MUST appear as a single device with a light entity that Home
  Assistant recognises as a light, so that dashboards, automations, scenes and voice
  assistants treat it like any other light.
- **FR-003**: Owners MUST be able to turn a lamp on and off from Home Assistant.
- **FR-004**: Owners MUST be able to set a lamp's brightness from Home Assistant.
- **FR-005**: Owners MUST be able to set a lamp's white warmth from Home Assistant, on
  lamps that support it.
- **FR-006**: The entity MUST advertise only the capabilities the lamp actually has, so
  that Home Assistant renders controls for those and no others.
- **FR-007**: The advertised brightness and warmth ranges MUST match what the lamp accepts,
  including any minimum brightness below which the lamp switches off.
- **FR-008**: For a lamp whose capabilities could not be determined, the system MUST offer
  the capabilities that are certain and MUST NOT claim ones that are unverified.
- **FR-009**: System MUST reflect in Home Assistant any state a lamp reports, including
  changes made at a wall switch or by any other means, without an operator command.
- **FR-010**: On reconnect, the state a lamp reports MUST take precedence over any state
  Home Assistant previously held for it.
- **FR-011**: System MUST mark a lamp unavailable in Home Assistant when it is not
  connected, and available again when it returns, without owner intervention.
- **FR-012**: System MUST mark all lamps unavailable if the replacement server itself stops,
  and restore them when it starts again.
- **FR-013**: Each lamp MUST keep a stable identity across restarts of both the server and
  Home Assistant, so that history, dashboards and automations continue to work.
- **FR-014**: Renaming a lamp in the terminal MUST NOT create a duplicate device in Home
  Assistant, and MUST NOT overwrite a name the owner has set within Home Assistant.
- **FR-015**: Lamps that are discovered but not adopted MUST NOT appear in Home Assistant.
- **FR-016**: Removing a lamp from the registry MUST remove it from Home Assistant.
- **FR-017**: A command from Home Assistant that the lamp does not confirm MUST surface as
  a visible outcome rather than as silence, consistent with how the terminal reports the
  same situation.
- **FR-018**: The terminal interface MUST continue to work unchanged while Home Assistant is
  connected, and changes made in either surface MUST be visible in the other.
- **FR-019**: Integration MUST be configurable and disableable, so that running the server
  without Home Assistant remains supported.
- **FR-020**: System MUST log integration activity sufficiently to diagnose a lamp that
  appears in the terminal but not in Home Assistant.
- **FR-021**: A broker that is unreachable, or that goes away and comes back, MUST NOT
  affect the lamps themselves or the terminal interface: control from the terminal keeps
  working, and Home Assistant recovers on its own once the broker returns.
- **FR-022**: System MUST reconnect to the broker on its own after an outage, without the
  owner restarting anything.

### Key Entities

- **Exposed Lamp**: An adopted lamp published to Home Assistant. Attributes: stable
  identity, display name, declared capabilities, current state, availability.
- **Capability Declaration**: What a lamp advertises it can do — on/off, brightness with
  its range, white warmth with its range — derived from what the lamp itself reported, not
  from a model assumption.
- **Availability Signal**: Whether a lamp, and the server as a whole, is currently
  reachable, so that Home Assistant can distinguish "off" from "not answering".

## Success Criteria *(mandatory)*

### Measurable Outcomes

> These are outcomes an owner can observe in their own house. They are not benchmark
> thresholds; the project constitution has no performance principle.

- **SC-001**: An adopted lamp appears in Home Assistant within one minute of adoption, with
  no editing of Home Assistant configuration files by hand.
- **SC-002**: Turning a lamp on or off from a Home Assistant dashboard visibly changes the
  lamp, and the entity settles on the lamp's reported state without the owner refreshing.
- **SC-003**: A white-only lamp presents no colour control anywhere in Home Assistant, and
  every control it does present produces a visible change on the lamp.
- **SC-004**: A change made at the wall switch is reflected in Home Assistant without any
  action by the owner.
- **SC-005**: After a lamp is unplugged and plugged back in, Home Assistant shows the state
  the lamp reports on startup, with no manual reload.
- **SC-006**: After restarting Home Assistant, the server, or both, every lamp returns on
  its own with its history, dashboards and automations intact.
- **SC-007**: An unplugged lamp shows as unavailable rather than showing a stale state.
- **SC-008**: An owner already running Home Assistant can complete setup in under 15
  minutes using the project's documentation alone.
- **SC-009**: Changes made in the terminal appear in Home Assistant and vice versa, with
  neither surface showing a value the lamp has not reported.
- **SC-010**: With the broker stopped, the lamps remain fully controllable from the
  terminal; when the broker returns, Home Assistant recovers every lamp without the owner
  restarting anything.

## Assumptions

- The owner already runs a Home Assistant installation on the same local network as the
  replacement server, and the two can reach each other.
- Adoption stays in the terminal: a new bulb is named there once, and only then does it
  reach Home Assistant. Naming a device is a deliberate act, and doing it in the terminal
  keeps one place responsible for identity.
- The lamps' own reported state remains authoritative everywhere, as established in feature
  001. Home Assistant is a view onto that, not a competing source of truth.
- Home Assistant's own automation, scheduling, scene and voice features cover those needs;
  this project does not reimplement them. Scheduling and scenes remain out of scope, as
  they were in feature 001.
- The confirmation behaviour learned from real hardware carries over: lamps confirm changes
  after a delay that varies from a fraction of a second to tens of seconds, so Home
  Assistant will briefly show a commanded value before the lamp's own report settles it.
- Only the captured white-only lamp family is supported today. Colour lamps are covered by
  the capability requirements above, and will work when a colour model is captured.
- The deployment is a single household with a handful of lamps and one trusted operator on
  a trusted network, consistent with feature 001. No multi-user permissions are required.
- **The lamps are published to a message broker the household already runs, using Home
  Assistant's standard auto-discovery convention** (decided 2026-08-28). The owner runs the
  broker; this project publishes to it. Home Assistant then creates and removes the devices
  on its own, which is what makes FR-001's "no manual configuration" achievable without
  writing anything that has to be installed inside Home Assistant.
- The broker is on the same trusted local network and reachable by both Home Assistant and
  the replacement server. Broker credentials, if any, are the owner's to supply.
- Nothing needs installing inside Home Assistant beyond the integration for that broker,
  which an owner running one already has.

## Dependencies

- Feature 001 (local replacement server) must be running and have at least one adopted lamp.
- A Home Assistant installation the owner controls.
- **A message broker the owner runs**, reachable from both Home Assistant and the
  replacement server. This is a prerequisite the owner must have in place; the project does
  not provide one. If the broker is down, lamps show as unavailable in Home Assistant and
  recover when it returns — the terminal interface is unaffected either way, because the
  lamps themselves never talk to the broker.
