# Phase 0 Research: Home Assistant Integration

**Feature**: 002-homeassistant-integration | **Date**: 2026-08-28

## 1. Where the abstraction boundary goes

**Decision**: A new `internal/lights` package holding a `Service` with typed operations and
typed errors. `internal/control` keeps parsing and formatting; `internal/hass` becomes a
second consumer. Neither front-end talks to the registry or a driver directly.

**Rationale**: The logic is already written — it is just fused to the TUI's output. Today
`control.Controller.Run` does five distinct things in one function: resolve a target,
validate the request against the bulb's capabilities, dispatch to the driver, interpret the
outcome, and render it as `ok`/`error`/`info` text. The first three are surface-neutral.
The last two are not.

The tell is in the current signature: everything returns `control.Result`, a display type
carrying pre-formatted strings. Home Assistant cannot use `"headlamp: brightness must be
0-100, got 150"` — it needs to know *that the value was out of range*, and say so in its own
way. So the split falls naturally between "what happened" and "how it reads".

What moves, what stays:

| Currently in `control` | Goes to | Why |
|---|---|---|
| Target resolution (name → id → prefix) | stays in `control` | Prefix matching is a typing affordance. Home Assistant addresses lamps by stable id and must never guess |
| Capability validation, range checks | `lights` | Both surfaces need the same answer; only the wording differs |
| Driver dispatch, timeout, `SetDesired` | `lights` | Pure coordination, no display concern |
| `ErrUnconfirmed` handling | `lights` returns it, both surfaces render it | The TUI says "watch the feed"; Home Assistant keeps the entity optimistic until the lamp reports |
| `Result` formatting, `list`, `info`, `help` | stays in `control` | Terminal presentation |

**Alternatives considered**:
- *Have `hass` call `control.Execute` with a synthesised command line.* Rejected as the
  worst of both worlds: parsing a string we just built, then scraping display text for an
  outcome. It also means every Home Assistant command is one typo away from a different
  action.
- *Put the service methods on `registry.Registry`.* Rejected: the registry is storage. It
  has no business knowing about command timeouts or capability policy, and it is already
  the right size.
- *Leave the logic where it is and let `hass` import `control`.* Rejected: it makes the TUI
  a dependency of a headless integration, so `-headless` would drag in Bubble Tea.

**Verification**: gate G1 — the whole existing test suite must pass unchanged. That is the
only thing separating a refactor from a rewrite with the same name.

## 2. The MQTT client: written here, not imported

**Decision**: Write a minimal MQTT 3.1.1 client in `internal/mqtt`, using the codec already
in `internal/protocol`.

**Rationale**: This is not the usual "write it ourselves" temptation, because most of it is
already written and tested. Feature 001 built a full MQTT codec to *serve* the bulbs, and
`fakebulb` already implements the client half — `CONNECT` encoding, `SUBSCRIBE` encoding,
`PINGREQ`, and publish handling — because it had to impersonate a device. The remaining
work is genuinely small:

| Piece | Status |
|---|---|
| Packet framing, varint lengths, `PUBLISH`, `PUBACK` | exists in `internal/protocol`, tested against real captured bytes |
| `CONNECT` and `SUBSCRIBE` encoding | exists in `internal/bulb/fakebulb`, to be promoted into `internal/protocol` |
| `CONNACK`/`SUBACK` decoding | ~15 lines each |
| Last-will fields in `CONNECT` | ~20 lines; needed for availability (FR-012) |
| Keep-alive, reconnect with backoff, QoS 1 retry | the actual new work |

Against that, `paho.mqtt.golang` brings a dependency tree, an API with known lifecycle
sharp edges, and the constitution's requirement to justify what the standard library and
existing code could not do. Here they can. The user's standing instruction for this project
is also explicit: standard library first, libraries only where necessary.

**Alternatives considered**:
- *`eclipse/paho.mqtt.golang`* — the obvious choice on a greenfield project and still the
  named fallback. If reconnect or QoS handling turns into a source of real bugs, swapping it
  in behind the same small interface is a contained change, because `internal/hass` will
  depend on an interface, not on the concrete client.
- *A raw `net.Conn` with hand-rolled framing.* Rejected — that is what `internal/protocol`
  already is, and duplicating it would be the actual waste.

**Scope limit, stated so it is not discovered later**: this is a client for one publisher
with a handful of topics. QoS 2, persistent sessions, and topic-alias trickery are not
implemented and are not needed — Home Assistant discovery uses QoS 0/1 with retained
messages.

## 3. Home Assistant discovery: the JSON light schema

**Decision**: Publish one discovery config per lamp to
`homeassistant/light/<node>/<object_id>/config` with `schema: "json"`, plus a retained
state topic and an availability topic.

**Rationale**: The JSON schema light is the one Home Assistant platform that expresses this
lamp exactly: a single state topic carrying `{"state":"ON","brightness":204,
"color_temp_kelvin":3400,"color_mode":"color_temp"}`, and a command topic taking the same
shape. The alternative "default" (non-JSON) light schema needs a separate topic per
attribute — five topics per lamp instead of two, more retained state to keep coherent, and
no gain.

Crucially, `supported_color_modes` is what makes User Story 2 work. A white-only lamp
declares `["color_temp"]` and Home Assistant renders a warmth slider and **no colour
wheel** — not because we hid it, but because the entity never claimed to have one. That is
the difference between a UI that is filtered and a UI that is correct.

**Alternatives considered**:
- *Default (topic-per-attribute) schema* — rejected, above.
- *A template light in YAML* — rejected: it is manual configuration, which FR-001 forbids.

## 4. Availability, and the difference between "off" and "not answering"

**Decision**: Two availability inputs per lamp, combined by Home Assistant with
`availability_mode: "all"`:

1. A **bridge** topic (`haigosmart/status`), set `online` on connect and backed by an MQTT
   **last will** of `offline`. The broker publishes the will if the server dies, crashes, or
   loses the network — which is exactly the case a graceful shutdown handler cannot cover.
2. A **per-lamp** topic, `online` while the bulb is connected to us and `offline` when it
   is not.

**Rationale**: FR-011 and FR-012 are different failures. A lamp unplugged at the wall is one
lamp gone; the server being killed is every lamp gone. Both must show as unavailable rather
than as stale state, and the last will is the only mechanism that survives the server not
getting a chance to say anything.

**Alternatives considered**: publishing `offline` from a shutdown handler only — rejected,
it cannot handle a crash or a pulled cable, which is precisely when it matters.

## 5. Colour temperature: percent to Kelvin, and why it is a knob

**Decision**: Map the lamp's 0–100 warmth scale linearly onto **2700 K (warmest, percent 0)
– 6500 K (coolest, percent 100)**, confirmed by the operator for this hardware. Kept as
`-ct-min-kelvin` / `-ct-max-kelvin` flags defaulting to those values.

**Rationale**: The lamps take `ColorTemperature` as a percentage where 0 is warmest and 100
is coolest — that much is established from the capture and confirmed on hardware. What no
capture can tell us is which *actual* colour temperatures those endpoints produce; that is
a property of the LEDs, and it differs between models and production runs. Home Assistant
speaks Kelvin, so a mapping is unavoidable.

2700–6500 K is both the near-universal range for consumer tunable-white lamps and the
operator's stated figure for this hardware, so the conversion is a known quantity rather
than an assumption dressed up as one.

The flags survive that confirmation for one reason: the range is a property of the LEDs, not
of the protocol, and a different Aigo model — an RGB one, or a later CCT revision — will
have different endpoints. Two flags cost nothing and mean the next lamp needs a config
change rather than a rebuild. They are no longer described to the owner as something they
probably need to tune.

**Alternatives considered**:
- *Hard-code 2700–6500 K.* Rejected: right for most, silently wrong for some, unfixable
  without a rebuild.
- *Advertise the lamp as brightness-only and drop warmth.* Rejected: it works, and User
  Story 2 is about showing what the lamp genuinely does.
- *Advertise warmth in mireds.* Home Assistant still accepts mireds but Kelvin is the
  current convention and reads better in the UI.

## 6. Identity, naming, and not breaking someone's dashboard

**Decision**: `unique_id` and the device identifier are the lamp's stable device id (its
MAC, from feature 001). The discovery payload carries the TUI name as `name`, and is
republished when the name changes.

**Rationale**: FR-013 and FR-014 pull in opposite directions — the identity must never
change, but the name should follow the TUI. Home Assistant separates these: it keys the
entity on `unique_id` and treats a discovery `name` as the *default*, which a name set by
the owner inside Home Assistant overrides permanently. So republishing on rename updates
the default without stamping on a local override, and history survives because the
`unique_id` never moved.

Removal (FR-016) is a retained empty payload on the config topic, which is Home Assistant's
documented way to say "this device is gone" rather than leaving a permanent unavailable
entry.

## 7. Startup state, and why nothing is restored

**Decision**: Publish state only from what the lamp reports. Never publish a remembered
state on startup, and never mark an entity available before its lamp has connected.

**Rationale**: This is User Story 3 and it is where most integrations get it wrong. The
registry persists last-known state across restarts, which is useful for the TUI's `list`,
but publishing it to Home Assistant on startup would assert something unverified — and the
whole scenario the owner described is *the lamp changed while we were not looking*. An
entity that is unavailable until its lamp speaks is honest; an entity showing last week's
brightness is not, and an automation reading it will act on a fiction.

**Alternatives considered**: publishing the persisted state as an optimistic initial value
— rejected. It would make FR-010 unsatisfiable, since there would be nothing to distinguish
"restored" from "reported".

## 8. Testing without a broker or a Home Assistant

**Decision**: `internal/mqtt/mqtttest` provides an in-process stub broker; `internal/hass`
is tested against it end to end, asserting on the actual published payloads.

**Rationale**: Constitution II requires integration tests across boundaries, and CI has
neither a broker nor Home Assistant. The precedent is `fakebulb`, which made the device
boundary testable — with one lesson attached: a double built from assumptions will agree
with those assumptions. So the stub broker is deliberately dumb. It implements the MQTT
wire protocol and nothing about Home Assistant's semantics, and the assertions are on
payload *content* checked against Home Assistant's published discovery format, not against
a helper of ours that could be wrong in the same direction as the code.

The parts a stub cannot prove — that Home Assistant actually renders a warmth slider and no
colour wheel — are gate G3, on the owner's real installation.

## 9. What is deliberately not built

- **No adoption from Home Assistant.** Instructed, and consistent with FR-015: naming is a
  deliberate act with one place responsible for it.
- **No scenes, schedules, or automations.** Home Assistant does these properly.
- **No broker bundled or installed.** The owner runs it; the server is a client.
- **No configuration UI.** Flags and a config file, as feature 001 established.
