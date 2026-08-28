# Data Model: Home Assistant Integration

**Feature**: 002-homeassistant-integration | **Date**: 2026-08-28

Feature 001's types are unchanged. This document covers what is added and what the existing
types now feed.

---

## lights.Service — the abstraction

`internal/lights.Service` is the one place that knows how to read and change a lamp. Both
front-ends consume it; neither reaches past it to the registry or a driver.

```go
type Service struct { /* registry, event bus, command timeout */ }
```

| Method | Purpose |
|---|---|
| `Snapshot() []bulb.Bulb` | Every known lamp, ordered by name |
| `Get(deviceID string) (bulb.Bulb, error)` | One lamp by stable id — no prefix matching |
| `SetPower(ctx, deviceID string, on bool) error` | |
| `SetBrightness(ctx, deviceID string, pct uint8) error` | |
| `SetColorTemp(ctx, deviceID string, pct uint8) error` | |
| `Apply(ctx, deviceID string, c Change) error` | Several attributes in one request, as a Home Assistant command carries |
| `Subscribe(depth int) *events.Subscription` | Delegates to the existing bus |

### Change

A partial update. Nil means "leave alone", which is what distinguishes a Home Assistant
command that sets only brightness from one that also turns the lamp on.

| Field | Type | Notes |
|---|---|---|
| `Power` | `*bool` | |
| `Brightness` | `*uint8` | 0–100 |
| `ColorTemp` | `*uint8` | 0–100, 0 = warmest |

Ordering and single-property dispatch are unchanged from feature 001 — the service builds a
target `LightState` and hands it to the driver, which sends one property per command.

### Typed errors

The point of the refactor. Each surface renders these its own way; neither parses the other's
text.

| Error | Meaning | TUI renders | Home Assistant renders |
|---|---|---|---|
| `ErrUnknownBulb` | No lamp with that id | `error unknown bulb %q…` | ignored command, logged |
| `ErrNotAdopted` | Discovered, not yet named | `error … not adopted yet…` | never occurs — unadopted lamps are not published |
| `ErrNotConnected` | Lamp offline | `error … not connected…` | entity already unavailable |
| `ErrOutOfRange` | Value outside 0–100 | `error brightness must be 0-100…` | clamped by HA, logged if it still arrives |
| `bulb.ErrUnsupported` | Capability absent | `error … does not support colour` | never offered in the first place |
| `bulb.ErrUnconfirmed` | Delivered, not yet confirmed | `info … watch the feed` | entity stays optimistic until the lamp reports |

`ErrOutOfRange` and friends carry the offending value so the message can state it, rather
than being a bare sentinel.

---

## hass.ExposedLamp

The projection of a `bulb.Bulb` onto what Home Assistant needs. Derived, never stored.

| Field | Source | Notes |
|---|---|---|
| `UniqueID` | `bulb.DeviceID` | Stable forever; the entity's identity (FR-013) |
| `ObjectID` | `bulb.DeviceID` | Topic path component |
| `Name` | `bulb.Name` | The *default* name; an owner's override in HA wins (FR-014) |
| `ColorModes` | `bulb.Capabilities` | See the table below — this is what makes US2 work |
| `MinKelvin`, `MaxKelvin` | configuration | 2700/6500, confirmed for this hardware (research.md §5) |
| `MinBrightnessPct` | `Capabilities.MinBrightness` | The floor below which the lamp switches off |

### Capability mapping

| `Capabilities` | `supported_color_modes` | What the owner sees |
|---|---|---|
| `Known && ColorTemp && !Color` | `["color_temp"]` | Brightness and warmth. **No colour wheel** |
| `Known && Color && ColorTemp` | `["color_temp", "rgb"]` | Full colour and warmth |
| `Known && Color && !ColorTemp` | `["rgb"]` | Colour, no warmth |
| `Known && !Color && !ColorTemp` | `["brightness"]` | Brightness only |
| `!Known` | `["brightness"]` | The conservative floor: on/off and brightness are certain for every lamp seen. Warmth and colour are not claimed until proven (FR-008) |

The `!Known` row is the one that matters. Feature 001 deliberately distinguishes "this lamp
has no colour" from "we never found out", and this is where that distinction pays: an
undetermined lamp advertises only what is certain rather than guessing in either direction.

---

## State payload

Published retained to the lamp's state topic on every reported change.

```json
{
  "state": "ON",
  "brightness": 204,
  "color_mode": "color_temp",
  "color_temp_kelvin": 3400
}
```

| Field | Derived from | Conversion |
|---|---|---|
| `state` | `LightState.Power` | `"ON"` / `"OFF"` |
| `brightness` | `LightState.Brightness` (0–100) | scaled to Home Assistant's 0–255 |
| `color_temp_kelvin` | `LightState.ColorTemp` (0–100) | linear onto the configured Kelvin range; omitted when the lamp has no warmth |
| `color_mode` | capabilities | omitted when the lamp is brightness-only |

Scaling is lossy in both directions — 101 lamp values against 256 Home Assistant values —
so the conversion round-trips through the lamp's scale to stay stable: a slider set to 128
must not oscillate because 128→50→127.

---

## Availability

| Topic | Values | Set when |
|---|---|---|
| Bridge: `haigosmart/status` | `online` / `offline` | `online` on broker connect; `offline` by **last will**, so a crash or pulled cable still reports (research.md §4) |
| Per lamp | `online` / `offline` | Follows `bulb.Status == Connected` |

Home Assistant combines both with `availability_mode: "all"`: a lamp is available only if
the server is up *and* the lamp is connected.

**Nothing is published as available until the lamp has actually reported.** Persisted state
from the registry is never published (research.md §7) — publishing a remembered value would
make FR-010 unsatisfiable, because nothing would distinguish "restored" from "reported".

---

## Lifecycle

```text
 lamp adopted in TUI ──► discovery config published (retained)
                          per-lamp availability = offline
                                    │
                      lamp connects and reports state
                                    ▼
                          state published (retained)
                          availability = online
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        ▼                           ▼                           ▼
 lamp reports change        HA sends a command          lamp disconnects
 state republished          → lights.Service            availability = offline
                            → lamp confirms             (state left as last reported)
                            → state republished
                                    │
                      lamp removed from registry
                                    ▼
              empty retained payload on the config topic (FR-016)
```

The loop is deliberately one-directional at the end: a command from Home Assistant does not
publish an optimistic state itself. It changes the lamp, the lamp reports, and the report is
what gets published — the same rule feature 001 established, extended to the second surface.
