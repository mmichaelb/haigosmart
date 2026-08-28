# Contract: Operational records

**Feature**: 003-headless-deployment | Spec: FR-002 … FR-005, FR-015, FR-017a, SC-002, SC-003

One JSON object per line. In headless mode this is the entire content of standard output —
nothing else is ever written there (FR-005).

## Shape

Field order is whatever `slog`'s JSON handler emits — `time`, `level`, `msg`, then
attributes, with `since` among them. Order carries no meaning; read by key.

```json
{"time":"2026-08-28 14:03:12.123","level":"INFO","msg":"bulb connected","kind":"connected","device":"a1b2c3d4","name":"headlamp","since":"1m12.345s"}
```

| Field | Present | Type | Notes |
|---|---|---|---|
| `time` | always | string | `2006-01-02 15:04:05.000`, local time |
| `since` | always | string | Elapsed from process start (`1m12.345s`), millisecond precision |
| `level` | always | string | `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `msg` | always | string | Stable, lower-case, **no interpolated values** — grep-able |
| `kind` | event records | string | `events.Kind` name; the vocabulary the terminal already uses |
| `device` | event records | string | Device identifier |
| `name` | event records | string | Display name; empty until adopted |
| `detail` | when present | string | The event's free text: a disconnect reason, an error |
| `<field>` | state changes | string | One key per changed property, value `from→to` (e.g. `"brightness":"40→100"`) |

Variable parts of an event belong in fields, never in `msg`. `{"msg":"bulb disconnected",
"detail":"no keep-alive for 180s"}` is greppable by message and precise by field;
`{"msg":"disconnected (no keep-alive for 180s)"}` is neither. This is a change from the
current log file, which interpolates — the terminal's rendering is unaffected, since it
formats from the event, not from the record.

## Record per event kind

| `kind` | `msg` | Level | Notes |
|---|---|---|---|
| `discovered` | `bulb discovered` | INFO | Interactive only — headless never discovers |
| `connected` | `bulb connected` | INFO | |
| `disconnected` | `bulb disconnected` | INFO | `detail` carries the reason |
| `state_changed` | `bulb reported state` | INFO | One field per change; none when the report changed nothing |
| `command_result` | `command failed` | WARN | Only failures are published as events today |
| `renamed` | `bulb renamed` | INFO | Covers adoption |
| `protocol_error` | `protocol error` | WARN | |
| `duplicate_id` | `duplicate device id` | WARN | |
| `rejected` | `bulb rejected` | WARN | **New.** `detail` is the remote address |

Every event the terminal displays produces a record (SC-003). The bus already logs
unconditionally while subscriber queues may drop under load, so the record stream stays
complete even when a display cannot keep up — that guarantee is inherited from feature 001,
not re-implemented here.

## Non-event records

| `msg` | Level | When | Fields |
|---|---|---|---|
| `starting` | INFO | Once, before the listener opens | The whole configuration, password redacted (FR-015) |
| `listening for bulbs` | INFO | After the listener opens | `addr` |
| `lamp configured` | INFO | Per declared lamp at startup | `device`, `name` |
| `registry lamp not configured` | WARN | Startup, per registry entry absent from the configuration | `device`, `name` |
| `setting overridden on the command line` | INFO | Per setting given both ways | `setting` — never the value, which may be a credential |
| `home assistant integration enabled` | INFO | When a broker is configured | `broker`, `kelvin_range` |
| `saving the registry failed` | WARN then DEBUG | First failure loud, later ones quiet | `path`, `error` |
| `shutting down` | INFO | On signal | `signal` |

## Rejection rate limiting

A refused lamp reconnects indefinitely, so its rejection is recorded on first sight and then
at most once per five minutes per device identifier. The suppressed count is carried in the
repeat record, so suppression is visible rather than silent:

```json
{"time":"2026-08-28 14:08:12.401","level":"WARN","msg":"bulb rejected","kind":"rejected","device":"9f9f9f9f","detail":"192.168.45.77:51234 (34 attempts since 14:03:12)","since":"6m12.6s"}
```

## Redaction

`HAIGOSMART_MQTT_PASSWORD` never appears in any record at any level, including records about
configuration errors concerning it. This holds because `Config` renders itself through
`slog.LogValuer` with the password replaced by `(set)` or `(unset)` — there is no code path
that formats the real value (FR-014, SC-006).

## Destination and failure

| Mode | Destination |
|---|---|
| Headless, no `-log` | Standard output |
| Headless, `-log` given | That file |
| Interactive, no `-log` | A temp file — never the terminal, which the interface is drawing |
| Interactive, `-log` given | That file |

A failed write to the record stream terminates the process: one line on standard error and
exit status 1. An unattended instance whose output goes nowhere is not running, and the
restart decision belongs to whatever supervises it.

This needs one deliberate piece of setup. Go's runtime raises `SIGPIPE` when a write to file
descriptor 1 or 2 hits a broken pipe, and the default disposition kills the process —
status 141, nothing on standard error, no explanation for whoever finds the dead container.
The process therefore registers for `SIGPIPE`, which makes those writes return `EPIPE`
instead, so the failure reaches the code that can describe it. Verified against a real
broken pipe, not only in the unit test.

## Level

`level` defaults to INFO and becomes DEBUG with `-v` / `HAIGOSMART_V=true`, which also turns
on per-frame protocol traces. There is no way to raise the threshold above INFO: an operator
who wants fewer records filters downstream, where the record that mattered is still
recoverable.
