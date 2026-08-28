# Quickstart & Validation: Home Assistant Integration

**Feature**: 002-homeassistant-integration | **Date**: 2026-08-28

## Prerequisites

1. Feature 001 running, with at least one adopted lamp (`list` shows it as `connected`).
2. Home Assistant on the same network.
3. **An MQTT broker you run.** The Mosquitto add-on is the usual choice. Install it, note
   its address and any credentials, and confirm Home Assistant's MQTT integration is
   configured against it. This project does not provide a broker and does not install one.

## Running

```bash
go build -o haigosmartd ./cmd/haigosmartd
./haigosmartd -mqtt-broker 192.168.1.10:1883 -mqtt-username ha -mqtt-password '…'
```

Without `-mqtt-broker`, nothing changes from feature 001 — the integration simply does not
start.

Within a minute, adopted lamps appear in Home Assistant under **Settings → Devices &
Services → MQTT**, with no YAML edited by hand.

## Validation scenarios

Each maps to a spec criterion. 1–7 need a real Home Assistant; the rest run in the suite.

| # | Scenario | Steps | Expected | Covers |
|---|---|---|---|---|
| 1 | Lamp appears | Start with a broker configured, open HA | The adopted lamp is listed as a device with a light entity, no YAML touched | SC-001, FR-001, FR-002 |
| 2 | Control works | Toggle, then set brightness and warmth from a dashboard card | The physical lamp changes each time; the entity settles on the lamp's reported state | SC-002, FR-003 to FR-005 |
| 3 | **Only real capabilities** | Open the entity's more-info dialog | Brightness and warmth sliders present, **no colour wheel**; every control shown changes the lamp | SC-003, FR-006, FR-007 |
| 4 | Wall switch | Flip the lamp at the wall | HA reflects it with no action by the owner | SC-004, FR-009 |
| 5 | Startup state | Unplug the lamp, change nothing, plug it back in | HA shows the state the lamp reports on startup, no reload | SC-005, FR-010 |
| 6 | Unavailability | Unplug the lamp and wait | The entity shows unavailable, not a stale value | SC-007, FR-011 |
| 7 | Server crash | `kill -9` the server | Every lamp goes unavailable via the last will; all recover on restart | SC-006, FR-012 |
| 8 | Broker outage | Stop the broker | **Lamps still fully controllable from the terminal.** HA recovers when the broker returns, nothing restarted | SC-010, FR-021, FR-022 |
| 9 | Rename | Rename the lamp in the terminal | No duplicate device in HA; history and automations intact; a name set inside HA is not overwritten | FR-013, FR-014 |
| 10 | Unadopted stays hidden | Power on a brand-new bulb | It appears in the terminal as discovered and **not** in HA until named | FR-015 |
| 11 | Both surfaces agree | Change from the terminal, then from HA | Each change appears in both; neither shows a value the lamp has not reported | SC-009, FR-018 |
| 12 | Automation | Automate the lamp on at sunset | It fires with nobody present | FR-002 |

### Without a broker or Home Assistant

Scenarios 8–11 and every conversion, payload and reconnect behaviour run against the stub
broker:

```bash
go test ./... -race
go test ./internal/hass -race -run TestDiscovery -v   # payload shape
go test ./internal/mqtt -race                         # client, reconnect, last will
```

Scenarios 1–7 and 12 need the real thing. Scenario 3 is the one no test can substitute for:
whether Home Assistant *renders* a warmth slider and no colour wheel is a fact about Home
Assistant, not about our payload.

## Colour temperature

The lamps report warmth as a percentage; Home Assistant speaks Kelvin. The mapping is
linear over **2700 K (warmest) to 6500 K (coolest)**, this hardware's actual range, so
nothing needs configuring.

A different Aigo model with different endpoints would need:

```bash
./haigosmartd -mqtt-broker … -ct-min-kelvin 3000 -ct-max-kelvin 6000
```

The lamp's behaviour does not change — only the numbers Home Assistant displays and the
mapping of a Kelvin request back onto the lamp's scale.

## Definition of done

- Scenarios 1–12 pass.
- `go test ./... -race` green; `gofmt -l .` empty; `go vet ./...` clean.
- **The terminal behaves exactly as before.** Tests adapted to the moved API are fine;
  assertions and expected values are not (G1, see plan.md). `contracts/tui-commands.md` from
  feature 001 is the reference for what must still come out.
- `docs/` explains broker setup.
