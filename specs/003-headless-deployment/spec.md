# Feature Specification: Headless Deployment

**Feature Branch**: `003-headless-deployment`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "I want to operationalize the deployment of the haigosmart monitor. For that, a few features are yet missing: non-TUI mode, where the logs are printed as json to stdout (i want to run the app as a container in the future, not a concern so for for implementation). option to configure the lamps as well as the home assistant target, port etc. using env vars or something else i can easily set using kubernetes standards. also when not being in TUI mode, the interactivity of the app is gone - it just runs from the config and no registry of new lamps is allowed. The initial setup path and then operationalization should be document as well."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run unattended with machine-readable logs (Priority: P1)

The operator runs the server on a machine they do not sit in front of — a home
server today, a scheduled workload later. There is no terminal to watch, so the
server must not try to draw one. Everything it has to say — a lamp connecting, a
state change, a command result, a broker that went away — goes to standard output
as one structured record per line, which a log collector can ingest without
parsing prose.

**Why this priority**: Without this the server cannot run anywhere except an
interactive terminal. Every other part of operationalizing depends on it.

**Independent Test**: Start the server with no terminal attached, power a known
lamp on and off, and confirm every event appears on standard output as a
self-contained structured record, with nothing written to the terminal display
and no interactive prompt.

**Acceptance Scenarios**:

1. **Given** the server is started in unattended mode, **When** it starts up, **Then** it writes its startup record to standard output as structured data and never draws a terminal interface.
2. **Given** the server is running unattended, **When** a known lamp reports a state change, **Then** one structured record describing that change appears on standard output.
3. **Given** the server is running unattended, **When** the operator sends the stop signal, **Then** the server records that it is shutting down, saves what it knows about the lamps, and exits reporting success.
4. **Given** the server is running unattended, **When** any record is written, **Then** it carries a timestamp, a severity, a message, and the fields that identify the lamp it concerns.

---

### User Story 2 - Configure everything without editing a command line (Priority: P1)

The operator describes the whole deployment — which port to accept lamps on,
where the Home Assistant broker lives and its credentials, the warmth range of
the lamps, and which lamps are expected — outside the program's invocation,
using the mechanisms their orchestration platform already provides. Restarting
with a changed setting requires changing that configuration, not rewriting a
start command.

**Why this priority**: An unattended server that can only be configured by
command-line arguments cannot be operated by a platform that expects to inject
configuration. This and Story 1 together are the minimum viable deployment.

**Independent Test**: Start the server with an empty command line, supplying every
setting through the configuration mechanism, and confirm the server listens where
told, connects to the named broker, and reports the expected lamps.

**Acceptance Scenarios**:

1. **Given** every setting is supplied through configuration and no command-line arguments are given, **When** the server starts, **Then** it runs with exactly those settings.
2. **Given** a setting is supplied both through configuration and on the command line, **When** the server starts, **Then** the command-line value wins and the server records that an override happened.
3. **Given** a setting holds a value the server cannot use — a port that is not a number, a warmest value above the coolest, an empty lamp identifier, **When** the server starts, **Then** it refuses to start and states which setting was wrong and what it expected.
4. **Given** the broker password is supplied through configuration, **When** the server writes any record, **Then** the password never appears in it.
5. **Given** no configuration is supplied at all, **When** the server starts, **Then** it uses the same defaults it uses today.

---

### User Story 3 - Refuse unknown lamps while unattended (Priority: P2)

Adopting a lamp — deciding it belongs to this installation and giving it a name —
is a deliberate act the operator performs from the terminal. When the server runs
unattended there is nobody to make that decision, so it makes no decision: it
serves exactly the lamps its configuration names, and a lamp it has never been
told about is not silently taken in, not named, and not offered to Home Assistant.

**Why this priority**: This is what makes an unattended deployment predictable —
its set of lamps is whatever the configuration says, and it cannot drift because
a neighbour's device found the port. It is P2 because Stories 1 and 2 already
deliver a working unattended server.

**Independent Test**: Run the server unattended against a configuration naming one
lamp, connect both that lamp and an unknown one, and confirm the known lamp works
end to end while the unknown one is rejected, recorded, and absent from Home
Assistant.

**Acceptance Scenarios**:

1. **Given** the server runs unattended, **When** a lamp not named in the configuration connects, **Then** the server refuses to serve it, records the rejection with the lamp's identifier, and Home Assistant gains no entry for it.
2. **Given** the server runs unattended, **When** a lamp named in the configuration connects, **Then** it is served normally and appears in Home Assistant under its configured name.
3. **Given** the server runs unattended, **When** it is running, **Then** it accepts no interactive input on any channel and offers no way to adopt or rename a lamp.
4. **Given** a lamp was rejected while the server ran unattended, **When** the operator later starts the server with a terminal, **Then** that lamp is discoverable and adoptable exactly as before.

---

### User Story 4 - Follow the path from first lamp to running deployment (Priority: P2)

A person setting this up for the first time reads one document that takes them
from a lamp still talking to the vendor's cloud through to a server running
unattended: redirect the lamp, adopt it at the terminal, capture what adoption
produced, turn that into configuration, and hand it to the platform. The document
says plainly which steps happen once at a terminal and which are repeatable.

**Why this priority**: The interactive and unattended modes only add up to a
deployment if the handover between them is written down; without it, each step
works and the path between them is guesswork.

**Independent Test**: A person who has not seen the project before follows the
document end to end on a fresh machine and reaches a running unattended server
controlling a real lamp from Home Assistant, without asking a question.

**Acceptance Scenarios**:

1. **Given** the setup document, **When** a new operator follows it in order, **Then** they reach an adopted, named lamp controllable from the terminal.
2. **Given** an adopted lamp, **When** the operator follows the operationalization section, **Then** they produce a configuration naming that lamp and start the server unattended with it.
3. **Given** the document, **When** the operator looks for a setting, **Then** every setting is listed with its name, meaning, default, and whether it is required.

---

### Edge Cases

- What happens when unattended mode is requested but the configuration names no lamps? The server MUST refuse to start rather than run as a server that will reject every lamp that connects — an empty lamp set is a configuration mistake, not a valid deployment.
- What happens when the configuration names a lamp that never connects? The server starts, reports the lamp as not connected, and keeps waiting; a lamp that is unplugged is not a startup failure.
- What happens when stored lamp knowledge holds a lamp the configuration does not name? It is not served — the configuration is the authority. The stored entry is left alone so an interactive session still sees it, and the omission is recorded once at startup so an operator who dropped a lamp by accident finds out from the log rather than from a dark room.
- What happens when the configured name of a lamp differs from the name already recorded for it? The configuration wins, the change is recorded, and Home Assistant is updated to the configured name.
- What happens when two configured lamps share an identifier or a name? The server refuses to start and names the collision.
- What happens when the broker is unreachable at startup? The server starts anyway and keeps serving lamps, exactly as it does today; the broker connection is retried.
- What happens when the record destination cannot be written — output redirected to a closed pipe? The server exits rather than continuing silently, because an unattended server nobody can hear is not running.
- What happens when the place lamp knowledge is stored is not writable, as it is when mounted read-only? The server starts and serves the configured lamps; it records once that it cannot save, and does not fail every time it would have.
- How does the system handle a stop signal arriving mid-command? The command in flight is abandoned, the lamp keeps whatever state it reached, and shutdown is recorded — the operator is never told a command succeeded that did not.

## Requirements *(mandatory)*

### Functional Requirements

#### Unattended operation

- **FR-001**: The system MUST offer a mode that runs with no terminal interface and no interactive input.
- **FR-002**: In unattended mode the system MUST write every operational record — startup, shutdown, lamp connections and disconnections, state reports, command outcomes, errors — to standard output.
- **FR-003**: Records written in unattended mode MUST be machine-parseable, one self-contained record per line, each carrying at minimum a timestamp, a severity, a message, and the identifying fields of whatever it concerns.
- **FR-004**: The system MUST allow the record severity threshold to be configured, so an operator can choose between routine and detailed output without a rebuild.
- **FR-005**: In unattended mode the system MUST NOT write anything to standard output that is not a record — no banners, no progress, no prompts.
- **FR-006**: The system MUST continue to offer the existing interactive terminal mode with unchanged behavior; unattended mode is an addition, not a replacement.
- **FR-007**: On receiving a stop signal the system MUST stop accepting lamp connections, record that it is shutting down, persist what it knows about the lamps if it can, and exit reporting success.

#### Configuration

- **FR-008**: The system MUST accept every operational setting from the process environment, so that a deployment is configured without changing how the program is invoked. Each setting MUST have one environment name, formed from the setting's name under a single project-wide prefix.
- **FR-009**: The settings covered MUST include: the address lamps connect to, whether to run unattended, the record severity threshold, where lamp knowledge is stored, how long to wait for a lamp to confirm a command, the Home Assistant broker address, its username, its password, the client identity and topic prefixes used with it, and the warmest and coolest colour values the lamps represent.
- **FR-010**: The system MUST allow the complete set of lamps it serves to be described in one environment value, as a list of entries each pairing a lamp's identifier with the name it is known by. The form MUST be writable by hand in a manifest, and a malformed entry MUST be reported by position and content, not silently skipped.
- **FR-011**: When a setting is given both in configuration and on the command line, the command line MUST win, and the system MUST record that the override occurred.
- **FR-012**: When no value is given for a setting anywhere, the system MUST use its documented default, and today's defaults MUST NOT change.
- **FR-013**: The system MUST validate all settings before it starts serving, and MUST refuse to start on an invalid one, naming the setting, the value it got, and what it expected.
- **FR-014**: The system MUST NOT write credentials into any record, at any severity, including when reporting a configuration error about them.
- **FR-015**: The system MUST record, once at startup, the settings it is running with — credentials redacted — so that what a running instance believes can be read from its output alone.

#### Lamp membership

- **FR-016**: In unattended mode the configured lamp set MUST be authoritative: the system serves exactly the lamps it names, and a lamp present only in stored lamp knowledge MUST NOT be served.
- **FR-017**: In unattended mode the system MUST close the connection of a lamp not in the configured set, once it has identified itself, and MUST NOT create any record of that lamp that would survive a restart.
- **FR-017a**: The system MUST record each rejection at a severity an operator would notice, carrying the lamp's identifier and network address — and MUST rate-limit repeats, because a rejected lamp reconnects indefinitely and MUST NOT be able to fill the output.
- **FR-018**: In unattended mode the system MUST NOT expose to Home Assistant any lamp it has not been configured with.
- **FR-019**: In unattended mode the system MUST refuse to start when it would serve no lamps at all.
- **FR-020**: In unattended mode the system MUST offer no means to adopt, name, or rename a lamp; adoption remains available only in interactive mode.
- **FR-021**: Lamps named in configuration MUST be known to the system from startup — reported as configured but not connected until they connect — so that a restart loses nothing even when no lamp knowledge was stored.
- **FR-022**: Where a lamp's configured name and its recorded name disagree, the configured name MUST win, and the change MUST be recorded.

#### Documentation

- **FR-023**: The documentation MUST describe one ordered path from an unmodified lamp to a running unattended deployment, marking which steps are performed once at a terminal and which are repeatable.
- **FR-024**: The documentation MUST explain how to turn the outcome of interactive adoption into the configuration the unattended mode needs.
- **FR-025**: The documentation MUST list every setting with its name, meaning, default, and whether it is required.
- **FR-026**: The documentation MUST state how credentials are supplied without appearing in configuration that gets committed or logged.
- **FR-027**: The documentation MUST describe how to tell a healthy running instance from an unhealthy one using its output alone.

### Key Entities

- **Deployment configuration**: The complete description of one running instance — where it listens, how it records, where its lamp knowledge lives, how it reaches Home Assistant, and which lamps it serves. Supplied entirely from outside the program; every field has a default except the lamp set in unattended mode.
- **Configured lamp**: A lamp the operator has declared this instance responsible for. Pairs the lamp's stable identifier with the name it is presented under. Identifiers and names are each unique within one configuration.
- **Operational record**: One machine-readable line describing one thing that happened, carrying a timestamp, a severity, a message, and the fields identifying its subject. The complete output of an unattended instance is a stream of these and nothing else.
- **Runtime mode**: Whether the instance is driven by a person at a terminal or runs unattended from configuration. Determines where records go, whether input is accepted, and whether unknown lamps may be adopted.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can start the server with an empty command line, every setting supplied through configuration, and it runs exactly as configured.
- **SC-002**: 100% of what an unattended instance emits parses as structured records; no line requires a human to read prose to know what happened.
- **SC-003**: Every operational event that the interactive mode shows on screen also appears in the unattended output — nothing observable is available only to someone watching a terminal.
- **SC-004**: An unattended instance given a configuration naming N lamps serves exactly those N lamps; a lamp not among them never appears in Home Assistant and leaves nothing behind after a restart.
- **SC-005**: An invalid setting is reported and refused within one second of startup, before any lamp connection is accepted, and the message names the setting and the expected form.
- **SC-006**: No credential appears anywhere in the output of a full run, at any severity level, including runs that fail on a configuration error.
- **SC-007**: A person who has not seen the project before reaches a running unattended deployment controlling a real lamp by following the documentation alone, without asking a question.
- **SC-008**: A restart of an unattended instance reaches the same served-lamp set with no operator action; the previous adoption work does not have to be redone.
- **SC-009**: An operator can determine whether a running instance is healthy — serving, connected to its broker, lamps reachable — from its output alone, with no terminal access.
- **SC-010**: The interactive mode's behavior is unchanged: every existing terminal command produces the same result and the same wording as before this feature.

## Assumptions

- The existing interactive terminal mode remains the only place a lamp is adopted; this feature does not add an unattended adoption path, per the user's instruction that adoption stays in the terminal.
- Lamp traffic redirection is already handled by the operator's network, unchanged from the previous features.
- The Home Assistant broker is operated by the user and is out of scope, unchanged from the previous feature.
- Container images, orchestration manifests, and health endpoints are out of scope for this feature; the user stated container packaging is a future concern. This feature makes the server configurable and observable enough that packaging it later is mechanical.
- Configuration is supplied through environment variables only (decided 2026-08-28). No configuration file format is introduced; secrets are supplied the same way as any other setting, which is what lets the platform inject them from its own secret store.
- The stored lamp knowledge is a cache, not a source of truth (decided 2026-08-28). An unattended instance that loses it starts up serving the same lamps; it only has to relearn their last reported state, which the lamps report on connecting anyway.
- Defaults are unchanged from today, so an operator who upgrades and changes nothing sees no difference.
- The colour warmth range remains 2700–6500 K by default, as established in feature 002.

