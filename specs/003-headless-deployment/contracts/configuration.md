# Contract: Configuration

**Feature**: 003-headless-deployment | Spec: FR-008 … FR-015, FR-019

Supersedes [002's configuration contract](../../002-homeassistant-integration/contracts/configuration.md)
by adding an environment name to every setting it defined. No flag is renamed and no default
changes: a command line that worked before this feature works after it, identically.

## Precedence

```
built-in default  <  HAIGOSMART_* environment variable  <  command-line flag
```

The last writer wins, and the ordering is the mechanism — the environment is applied to the
flag set before parsing, so parsing overwrites it. When a flag is given on the command line
*and* its variable is set, one record notes the override with the setting name (FR-011). The
value is not included, because the setting may be a credential.

## Settings

Environment names are the flag name uppercased with `-` replaced by `_`, under the
`HAIGOSMART_` prefix. There is no exception to that rule.

| Flag | Environment | Default | Required | Meaning |
|---|---|---|---|---|
| `-listen` | `HAIGOSMART_LISTEN` | `:1883` | no | Address lamps connect to |
| `-headless` | `HAIGOSMART_HEADLESS` | `false` | no | Run with no terminal and no input |
| `-v` | `HAIGOSMART_V` | `false` | no | Debug records including protocol traces |
| `-log` | `HAIGOSMART_LOG` | *(empty)* | no | Record destination. Empty means stdout when headless, a temp file otherwise |
| `-registry` | `HAIGOSMART_REGISTRY` | user config dir | no | Registry file; a cache, not the source of truth |
| `-command-timeout` | `HAIGOSMART_COMMAND_TIMEOUT` | `5s` | no | How long before a command is reported unconfirmed |
| `-lamps` | `HAIGOSMART_LAMPS` | *(empty)* | **when headless** | The lamps this instance serves |
| `-mqtt-broker` | `HAIGOSMART_MQTT_BROKER` | *(empty)* | no | `host:port`; empty disables Home Assistant |
| `-mqtt-username` | `HAIGOSMART_MQTT_USERNAME` | *(empty)* | no | Broker username |
| `-mqtt-password` | `HAIGOSMART_MQTT_PASSWORD` | *(empty)* | no | Broker password. **Never appears in any record** |
| `-mqtt-client-id` | `HAIGOSMART_MQTT_CLIENT_ID` | `haigosmart` | no | Client id presented to the broker |
| `-mqtt-prefix` | `HAIGOSMART_MQTT_PREFIX` | `haigosmart` | no | Base topic for state, availability, commands |
| `-mqtt-discovery-prefix` | `HAIGOSMART_MQTT_DISCOVERY_PREFIX` | `homeassistant` | no | Home Assistant discovery prefix |
| `-ct-min-kelvin` | `HAIGOSMART_CT_MIN_KELVIN` | `2700` | no | Kelvin at the lamp's warmest (percent 0) |
| `-ct-max-kelvin` | `HAIGOSMART_CT_MAX_KELVIN` | `6500` | no | Kelvin at the lamp's coolest (percent 100) |

## The lamp set

```
HAIGOSMART_LAMPS="a1b2c3d4=headlamp,e5f6a7b8=desk"
```

Comma-separated `deviceID=name`. Whitespace around either side of an entry is trimmed. The
device identifier is the one the lamp presents on connecting — the same string the terminal
shows in `list` under `ID`, which is where an operator gets it.

| Input | Result |
|---|---|
| `a1=lamp,b2=desk` | Two lamps |
| `a1 = lamp , b2 = desk` | Two lamps, whitespace trimmed |
| `a1=lamp,` | **Error**: `HAIGOSMART_LAMPS entry 2 is empty` |
| `a1` | **Error**: `HAIGOSMART_LAMPS entry 1 "a1" is not deviceID=name` |
| `=lamp` | **Error**: `HAIGOSMART_LAMPS entry 1 has an empty device id` |
| `a1=` | **Error**: `HAIGOSMART_LAMPS entry 1 (a1) has an empty name` |
| `a1=lamp,a1=desk` | **Error**: `HAIGOSMART_LAMPS repeats device id "a1" at entries 1 and 2` |
| `a1=lamp,b2=lamp` | **Error**: `HAIGOSMART_LAMPS reuses name "lamp" for a1 and b2` |
| `a1=off` | **Error**: `HAIGOSMART_LAMPS name "off" is a terminal command; pick another so the name stays addressable` |

Nothing is skipped silently. A lamp missing from a manifest because of a typo would otherwise
present as a room that stopped working, with a clean log.

## Startup

In order, before anything opens a socket:

1. Defaults declared, environment applied, command line parsed.
2. Every setting validated. First failure aborts with exit status 1 and a message naming the
   setting, the value received, and what was expected. Nothing has been opened yet, so there
   is nothing to unwind (SC-005).
3. Record destination opened. If it cannot be opened, that is the error, on standard error.
4. One record at INFO with the whole configuration, password redacted (FR-015).
5. Registry loaded; configured lamps declared; lamps in the file but not configured reported
   once.
6. Listener opened.

## Failure behaviour

| Situation | Result |
|---|---|
| Unparseable value in a variable (`HAIGOSMART_CT_MIN_KELVIN=warm`) | Refused at startup, naming the variable, the value, and the type expected |
| `min ≥ max` Kelvin | Refused, naming both — unchanged from feature 002 |
| Headless with no lamps configured | Refused: an unattended instance that would reject every connection is a configuration mistake (FR-019) |
| Duplicate lamp id or name | Refused, naming both entries |
| Configured lamp never connects | Not an error. Started, reported as configured and not connected, published to Home Assistant as unavailable |
| Registry file holds a lamp the configuration omits | Not an error. Left on disk, excluded from service, reported once at startup |
| Registry file unwritable (read-only mount) | Not an error. First failed save reported at WARN, later ones at DEBUG. The instance keeps serving; the file is a cache |
| Broker unreachable | Unchanged from feature 002: the server starts, lamps work, the client retries |
| Record stream cannot be written | Fatal. A message on standard error, non-zero exit. An instance nobody can hear is not running |

## What does not change

- Every existing flag keeps its name, type, and default.
- Interactive mode behaves exactly as before, including adoption, renaming, and unknown lamps
  appearing as discovered (SC-010).
- No configuration file format is introduced.
