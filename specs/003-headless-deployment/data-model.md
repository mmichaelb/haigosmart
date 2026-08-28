# Data Model: Headless Deployment

**Feature**: 003-headless-deployment | **Date**: 2026-08-28

Nothing persistent is added. The registry file keeps schema version 1 and its existing
shape; this feature only changes who decides what belongs in it. The entities below are
runtime values built at startup and, in one case, a value written to the record stream.

## Config

The complete description of one running instance. Built once by `config.Load`, validated,
then read-only for the process lifetime.

| Field | Type | Default | Source | Notes |
|---|---|---|---|---|
| `Listen` | string | `:1883` | `HAIGOSMART_LISTEN` / `-listen` | Where lamps connect |
| `Headless` | bool | `false` | `HAIGOSMART_HEADLESS` / `-headless` | No terminal, no input |
| `Verbose` | bool | `false` | `HAIGOSMART_V` / `-v` | Debug records, protocol traces |
| `LogPath` | string | *(empty)* | `HAIGOSMART_LOG` / `-log` | Empty means stdout in headless, a temp file otherwise |
| `RegistryPath` | string | user config dir | `HAIGOSMART_REGISTRY` / `-registry` | A cache under Q2, not the source of truth |
| `CommandTimeout` | duration | `5s` | `HAIGOSMART_COMMAND_TIMEOUT` / `-command-timeout` | Reporting threshold, unchanged from feature 001 |
| `Lamps` | `[]ConfiguredLamp` | empty | `HAIGOSMART_LAMPS` / `-lamps` | Required when `Headless` |
| `MQTTBroker` | string | *(empty)* | `HAIGOSMART_MQTT_BROKER` / `-mqtt-broker` | Empty disables Home Assistant, unchanged |
| `MQTTUsername` | string | *(empty)* | `HAIGOSMART_MQTT_USERNAME` / `-mqtt-username` | |
| `MQTTPassword` | string | *(empty)* | `HAIGOSMART_MQTT_PASSWORD` / `-mqtt-password` | **Never rendered** — see Redaction |
| `MQTTClientID` | string | `haigosmart` | `HAIGOSMART_MQTT_CLIENT_ID` / `-mqtt-client-id` | |
| `MQTTPrefix` | string | `haigosmart` | `HAIGOSMART_MQTT_PREFIX` / `-mqtt-prefix` | |
| `DiscoveryPrefix` | string | `homeassistant` | `HAIGOSMART_MQTT_DISCOVERY_PREFIX` / `-mqtt-discovery-prefix` | |
| `MinKelvin` | int | `2700` | `HAIGOSMART_CT_MIN_KELVIN` / `-ct-min-kelvin` | |
| `MaxKelvin` | int | `6500` | `HAIGOSMART_CT_MAX_KELVIN` / `-ct-max-kelvin` | |

Every default is today's default (FR-012): an operator who upgrades and sets nothing sees
no change.

### Validation

Runs before any listener opens; any failure aborts startup naming the setting, the value
received, and what was expected (FR-013).

| Rule | Requirement |
|---|---|
| `MinKelvin < MaxKelvin` | Existing rule from feature 002, unchanged |
| `CommandTimeout > 0` | A non-positive threshold would report every command as unconfirmed |
| `Listen` parses as a host:port | Fails at startup rather than at first connection |
| `Headless` ⇒ `len(Lamps) > 0` | FR-019 — an unattended instance serving nothing is a mistake, not a deployment |
| Lamp entries well formed and unique | See ConfiguredLamp below |
| `LogPath` set ⇒ its directory exists and is writable | Checked by opening it, before the listener |

### Redaction

`Config` implements `slog.LogValuer`, returning every field except `MQTTPassword`, which is
rendered as `"(set)"` or `"(unset)"`. This is what makes FR-014 structural rather than a
promise: passing the whole config to any record — including the startup record required by
FR-015, and including a record written by future code that has not been reviewed for this —
cannot print the password, because there is no path through which it renders.

## ConfiguredLamp

One entry of the configured set: the lamp this instance is responsible for, and the name it
is presented under.

| Field | Type | Rules |
|---|---|---|
| `DeviceID` | string | Non-empty; unique within the configuration; matches the identifier the lamp presents in its CONNECT |
| `Name` | string | Non-empty; unique within the configuration; not a terminal verb |

**Wire form**: `HAIGOSMART_LAMPS="a1b2c3d4=headlamp,e5f6a7b8=desk"`. Surrounding whitespace
around an identifier or a name is trimmed; an empty entry between two commas is an error, not
a skip.

**Relationship to the registry**: at startup each configured lamp is `Declare`d — an entry is
created if absent (`Disconnected`, no state yet, no capabilities yet) or renamed if the stored
name differs. The configured name wins (FR-022). Registry entries not in the configured set
are left untouched on disk and excluded from admission.

## Admission set

The set of device identifiers this instance will serve, derived from `Config.Lamps` at
startup and fixed for the process lifetime.

| Mode | Value | Effect |
|---|---|---|
| Interactive | `nil` predicate | Every lamp admitted — today's behaviour, unchanged |
| Headless | configured identifiers | Anything else refused with CONNACK `0x05` and closed |

It is deliberately not derived from the live registry: a set that grew as lamps connected
would reintroduce exactly the drift Q2 removed.

## Operational record

One JSON object per line on the record stream. Fields, in the order `slog` emits them:

| Field | Always | Meaning |
|---|---|---|
| `time` | yes | `2006-01-02 15:04:05.000`, local |
| `since` | yes | Elapsed from process start, e.g. `1m12.345s` |
| `level` | yes | `DEBUG` / `INFO` / `WARN` / `ERROR` |
| `msg` | yes | Short, stable, lower-case |
| `kind` | event records | The `events.Kind` name — the same vocabulary the terminal uses |
| `device` | event records | Device identifier |
| `name` | event records | Display name, empty until adopted |
| `detail` | when present | Free text from the event |
| *field names* | state changes | One key per changed property, value `from→to` |

The full contract, including the record emitted for each event kind, is in
[contracts/log-records.md](./contracts/log-records.md).

## Runtime mode

Not a stored value — the consequences of `Config.Headless`, collected here because they are
what distinguishes the two modes:

| | Interactive | Headless |
|---|---|---|
| Terminal interface | drawn | never |
| Records go to | file (`-log` or temp) | standard output |
| Input accepted | yes | none, on any channel |
| Unknown lamp connects | discovered, adoptable | refused, nothing persisted |
| Adoption / rename | available | unavailable |
| Lamp set | whatever has connected, plus the registry | the configuration, exactly |

## New event kind

`events.Rejected` — a lamp not in the admission set attempted to connect.

| | |
|---|---|
| `Text()` | `rejected: not in the configured lamp set` |
| Level | warning |
| `Detail` | the remote address; on a suppressed-then-reported repeat, also the count of attempts covered |

It is a first-class event rather than a bare log line so that both surfaces describe it
identically (Principle III) and so that a future interactive use — showing what has been
knocking at the door — needs no new plumbing.
