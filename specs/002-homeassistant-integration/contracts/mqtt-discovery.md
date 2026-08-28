# Contract: MQTT Topics and Payloads

**Feature**: 002-homeassistant-integration | Spec: FR-001, FR-002, FR-006 to FR-016

What the server publishes for Home Assistant to consume. Home Assistant's MQTT discovery
convention; nothing here needs installing on the Home Assistant side beyond its MQTT
integration.

`<disc>` is Home Assistant's discovery prefix, `homeassistant` by default.
`<base>` is this server's own prefix, `haigosmart` by default.
`<id>` is the lamp's stable device id (its MAC, e.g. `703e975dc388`).

## Topics

| Topic | Retained | Direction | Purpose |
|---|---|---|---|
| `<disc>/light/<base>/<id>/config` | yes | out | Discovery. Empty payload removes the device (FR-016) |
| `<base>/light/<id>/state` | yes | out | Current state, published on every reported change |
| `<base>/light/<id>/availability` | yes | out | `online` / `offline` per lamp (FR-011) |
| `<base>/light/<id>/set` | no | **in** | Commands from Home Assistant |
| `<base>/status` | yes | out | Bridge availability; `offline` is the MQTT **last will** (FR-012) |

Retention is what makes a Home Assistant restart recover on its own (FR-013, SC-006): the
broker replays the last config, state and availability without the server doing anything.

## Discovery payload

For the captured white-only lamp:

```json
{
  "schema": "json",
  "unique_id": "haigosmart_703e975dc388",
  "object_id": "703e975dc388",
  "name": "headlamp",
  "state_topic": "haigosmart/light/703e975dc388/state",
  "command_topic": "haigosmart/light/703e975dc388/set",
  "brightness": true,
  "brightness_scale": 255,
  "supported_color_modes": ["color_temp"],
  "min_kelvin": 2700,
  "max_kelvin": 6500,
  "availability_mode": "all",
  "availability": [
    {"topic": "haigosmart/status"},
    {"topic": "haigosmart/light/703e975dc388/availability"}
  ],
  "device": {
    "identifiers": ["haigosmart_703e975dc388"],
    "name": "headlamp",
    "manufacturer": "Aigo",
    "model": "aigo_light_cct",
    "sw_version": "aigo_light_cct_v4.0.0",
    "via_device": "haigosmart"
  }
}
```

**`supported_color_modes` is the whole of User Story 2.** A lamp that declares
`["color_temp"]` gets a warmth slider and no colour wheel — not because the interface hides
one, but because the entity never claimed to have one. See data-model.md for the full
capability mapping, including the `Known == false` case, which advertises `["brightness"]`
and claims nothing unproven (FR-008).

`min_kelvin`/`max_kelvin` come from configuration, not from the lamp — the lamp reports only
a percentage. 2700–6500 K is confirmed correct for this hardware; the flags exist for models
with different endpoints (research.md §5).

## State payload

```json
{"state": "ON", "brightness": 204, "color_mode": "color_temp", "color_temp_kelvin": 3400}
```

- `brightness` is Home Assistant's 0–255, converted from the lamp's 0–100.
- `color_temp_kelvin` is converted linearly from the lamp's 0–100, where **0 is warmest**.
- `color_mode` and `color_temp_kelvin` are omitted for lamps without warmth.
- Published **only from what the lamp reported**. Persisted state is never published, and a
  lamp is never available before it has spoken (FR-010, research.md §7).

## Command payload

Home Assistant sends the same shape:

```json
{"state": "ON", "brightness": 128, "color_temp_kelvin": 4000}
```

Handling:

1. Convert to the lamp's units; build a `lights.Change` with only the fields present.
2. Call `lights.Service`. Validation and capability checks happen there, once.
3. **Publish nothing optimistically.** The lamp changes, the lamp reports, the report is
   published. A command that is not yet confirmed leaves the entity as it was.
4. A malformed payload is logged and ignored. It never drops the broker connection, and it
   never affects another lamp.

Conversion is round-trip stable: a Home Assistant brightness of 128 must not oscillate
because 128 → 50 → 127.

## Connection behaviour

- Reconnect with backoff, indefinitely (FR-022). A broker outage never touches the lamps or
  the terminal (FR-021) — the lamps do not talk to the broker at all.
- On every (re)connect: publish `<base>/status` = `online`, republish all discovery configs,
  and republish current state and availability. Retained messages make this cheap and make
  recovery automatic.
- Last will registered at connect: `<base>/status` = `offline`, retained. This is the only
  mechanism that reports a crash or a pulled cable.
- Credentials, if the broker needs them, come from configuration and are never logged.
