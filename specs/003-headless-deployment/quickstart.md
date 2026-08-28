# Quickstart: Headless Deployment

**Feature**: 003-headless-deployment | **Date**: 2026-08-28

How to prove this feature works. Scenarios 1–4 need no hardware; 5–8 need a real lamp.
Gate names (G1…) are referenced by `tasks.md`.

## Prerequisites

- Go 1.27, this repository, `go build ./cmd/haigosmartd`
- For scenarios 5–8: one adopted Aigo lamp with its traffic redirected (see
  [docs/capture-setup.md](../../docs/capture-setup.md)) and, for 8, a running MQTT broker

## Scenario 1 — Configuration comes entirely from the environment (SC-001, FR-008)

```bash
HAIGOSMART_LISTEN=:18830 \
HAIGOSMART_HEADLESS=true \
HAIGOSMART_REGISTRY=/tmp/hg/registry.json \
HAIGOSMART_LAMPS="a1b2c3d4=headlamp" \
./haigosmartd
```

**Expect**: starts with an empty command line; the `starting` record shows exactly these
values; the `listening for bulbs` record shows `:18830`.

## Scenario 2 — The command line still wins (FR-011)

```bash
HAIGOSMART_LISTEN=:18830 ./haigosmartd -listen :18831 -headless -lamps a1=x
```

**Expect**: listening on `:18831`, plus one record
`setting overridden on the command line` naming `listen` — and **not** its value.

## Scenario 3 — Bad settings are refused before anything opens (SC-005, FR-013)

```bash
HAIGOSMART_CT_MIN_KELVIN=7000 ./haigosmartd -headless -lamps a1=x   # min ≥ max
HAIGOSMART_LAMPS="a1=x,a1=y"     ./haigosmartd -headless            # duplicate id
HAIGOSMART_LAMPS="a1=off"        ./haigosmartd -headless            # name is a verb
HAIGOSMART_COMMAND_TIMEOUT=soon  ./haigosmartd -headless -lamps a1=x # not a duration
                                  ./haigosmartd -headless            # no lamps at all
```

**Expect**: each exits 1 within a second, naming the setting, the value received, and what
was expected. No port is bound — confirm with `lsof -i :1883` while it is failing.

## Scenario 4 — Records are JSON lines and carry no credential (SC-002, SC-006)

```bash
HAIGOSMART_MQTT_PASSWORD=hunter2 HAIGOSMART_MQTT_BROKER=127.0.0.1:1 \
HAIGOSMART_HEADLESS=true HAIGOSMART_LAMPS="a1=x" \
./haigosmartd -listen :18830 > out.jsonl 2>err.txt &
sleep 3; kill %1
jq -e '.time and .since and .level and .msg' out.jsonl > /dev/null   # every line parses
grep -c hunter2 out.jsonl err.txt                                     # must be 0
```

**Expect**: `jq` accepts every line; the password appears nowhere, including in the
broker-connection-failure records. `time` reads `2026-08-28 14:03:12.123`, and every `since`
lies between zero and the run's own length. Do not expect `since` to rise strictly down the
file: a record is stamped when it is logged and written when its goroutine reaches the
writer, so under concurrency the two orders differ. That is precisely why the field is
anchored to process start rather than to the previous record.

**G1** — scenarios 1–4 green. **Passed 2026-08-28**: environment-only startup, flag
override recorded by name, four invalid settings each refused before the listener with a
message naming the setting, and a three-second run whose every line satisfied
`jq -e '.time and .since and .level and .msg'` with zero occurrences of the password —
including the repeated broker-connection-failure records.

## Scenario 5 — A configured lamp is served (FR-016, FR-021)

Configure the lamp's real device id (`list` in the terminal shows it under `ID`), start
headless, power the lamp on.

**Expect**: at startup, before the lamp connects, one `lamp configured` record and the lamp
present as disconnected. On power-up: `connected`, then `state_changed` records. No
`discovered` record — headless never discovers.

## Scenario 6 — An unknown lamp is refused (FR-017, FR-017a, SC-004)

Power on a second lamp not named in `HAIGOSMART_LAMPS`.

**Expect**:

- one `bulb rejected` record at WARN naming its device id and address;
- the lamp reconnects repeatedly but produces **at most one record per five minutes**, each
  repeat carrying the suppressed attempt count;
- `jq '.device' registry.json` — the rejected id is absent, and stays absent across a
  restart;
- the lamp never appears in Home Assistant.

Then stop, start interactively, and power the same lamp on: it appears as discovered and
adopts normally. Rejection was a mode, not a verdict.

## Scenario 7 — Configuration beats the registry file (FR-016, FR-022, Q2)

1. Adopt two lamps interactively so both are in `registry.json`.
2. Start headless with only the first in `HAIGOSMART_LAMPS`.
3. Also change that first lamp's name in the configuration.

**Expect**: one `registry lamp not configured` record naming the second lamp; the second
lamp is refused if it connects, and its entry is **still in the file** afterwards; the first
lamp is served under its configured name, in the terminal and in Home Assistant.

Then delete `registry.json` entirely and restart. **Expect**: the same served lamp set, the
same name, no operator action (SC-008) — only the last-known state is gone, and the lamp
reports it again on connecting.

## Scenario 8 — Home Assistant, unattended (SC-004, SC-009)

Start headless with a broker configured.

**Expect**: configured lamps appear in Home Assistant at startup, unavailable, and become
available on connecting. Rejected lamps never appear. Killing the process makes every lamp
unavailable via the last will; restarting restores them.

## Scenario 9 — Shutdown and unwritable storage (FR-007, edge cases)

```bash
kill -TERM <pid>                       # graceful
chmod a-w /tmp/hg && kill -TERM <pid>  # read-only registry
```

**Expect**: `shutting down` recorded, registry saved when it can be, exit status 0. With the
directory read-only: one `saving the registry failed` at WARN, later attempts at DEBUG, the
instance keeps serving throughout and still exits 0.

**G2** — scenarios 5–9 green on real hardware. **Not yet run**: needs a lamp, and
scenario 6 needs a second lamp that is deliberately left out of the configuration.

Scenario 7's software half was exercised on 2026-08-28 with a hand-written registry file
holding two lamps and a configuration naming one: the named lamp was renamed to its
configured name, the other was reported once as `registry lamp not configured` and left on
disk. The hardware half — that the unconfigured lamp is actually refused when it connects —
is covered by `internal/server/admit_test.go` through a real CONNECT/CONNACK exchange, and
remains to be confirmed against the physical lamp.

## Scenario 10 — Interactive mode is untouched (SC-010)

```bash
go test ./... -race    # the feature 001 and 002 suites, unchanged
./haigosmartd          # no flags
```

**Passed 2026-08-28** for the automated half: `go test ./... -race` is green across all 12
packages that have tests (15 in the module), and `git diff -- '*_test.go'` is empty — no existing test was changed to
accommodate this feature. Six test files were added, none edited. The terminal run itself
belongs with G2, since it needs a lamp.

**Expect**: every existing test passes without modification; the terminal behaves exactly as
documented in
[001's TUI contract](../../001-local-bulb-server/contracts/tui-commands.md) — same commands,
same wording, adoption included.

**G3** — the documentation gate: someone who has not seen this project follows
[docs/deploying.md](../../docs/deploying.md) end to end and reaches a running unattended
instance controlling a real lamp from Home Assistant, without asking a question (SC-007).
