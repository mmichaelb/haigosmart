# Deploying haigosmartd

From a bulb still talking to the vendor's cloud to a server running unattended.

There are two halves, and the split matters:

- **Part one happens once per lamp, at a terminal.** Adopting a lamp — deciding
  it is yours and giving it a name — is a deliberate act by a person. There is
  no unattended path for it, by design.
- **Part two is repeatable and needs no terminal.** The unattended server runs
  from configuration alone: it serves exactly the lamps it is told about, prints
  JSON records to standard output, and accepts no input.

---

## Part one — adopt the lamps (once, at a terminal)

### 1. Point the lamp at this machine

The bulbs reach Alibaba Cloud on port 1883 — `47.254.156.103`, and
`public.iot-as-mqtt.eu-central-1.aliyuncs.com` by SNI on live hardware. Redirect
that to this machine with a DNS override, a hosts entry on your router, or a
DNAT rule. Nothing on the bulb changes. Details and alternatives are in
[operating.md](operating.md); a packet capture, if you need one, is in
[capture-setup.md](capture-setup.md).

### 2. Run interactively

Install it, download it, or build it — any of the three:

```bash
go install github.com/mmichaelb/haigosmart/cmd/haigosmartd@latest
```

```bash
# from https://github.com/mmichaelb/haigosmart/releases/latest
tar -xzf haigosmart_*_linux_amd64.tar.gz
sha256sum -c checksums.txt --ignore-missing
```

```bash
go build -o haigosmartd ./cmd/haigosmartd
```

Then:

```bash
./haigosmartd
```

The container is not an option here: adoption needs a terminal, and the image
runs headless.

### 3. Adopt each lamp and write down its device id

Power the lamp on. It appears as **discovered**. Name it — naming *is* adoption:

```text
> list
NAME           ID             STATUS        PWR   BRI  TEMP  LAST SEEN
(unadopted)    703e975dc388   discovered    off     0     0  14:02:11

> name 703e975dc388 kitchen
ok      kitchen: adopted (was 703e975dc388)
> on kitchen
ok      kitchen: on
```

**The `ID` column is what part two needs.** Write it down for every lamp, next
to the name you gave it. Repeat for each lamp, then `quit`.

### 4. Confirm before moving on

`on`, `off`, and `bri` should work for every lamp. A lamp that does not respond
here will not respond unattended either, and debugging it is much easier with a
terminal in front of you.

---

## Part two — run it unattended (repeatable)

### 5. Turn the adopted lamps into configuration

Every setting is an environment variable. The lamp set is one of them:

```bash
export HAIGOSMART_HEADLESS=true
export HAIGOSMART_LAMPS="703e975dc388=kitchen,e5f6a7b8c9d0=desk"
```

`HAIGOSMART_LAMPS` is comma-separated `deviceID=name`. The device ids are the
ones from step 3; the names are yours to choose, and they are what appears in
Home Assistant.

**The configuration is authoritative.** An unattended instance serves exactly
the lamps named here:

- a lamp in this list is present from startup, named, and shown as unavailable
  until it connects;
- a lamp *not* in this list is refused when it connects, and nothing about it is
  written anywhere;
- a lamp in the registry file but not in this list is not served, and is reported
  once at startup so you notice a bad edit rather than a dark room.

That is what makes a restart predictable: the served lamp set is whatever the
configuration says, and it cannot drift.

### 6. Choose the remaining settings

Copy [`configs/haigosmart.env.example`](../configs/haigosmart.env.example) and
edit it. Only `HAIGOSMART_LAMPS` is required, and only when headless; everything
else has a working default.

### 7. Start it

```bash
./haigosmartd
```

Or run the container, which is the same server with the same settings:

```bash
docker run -d --name haigosmart \
  -p 1883:1883 \
  -v haigosmart-data:/data \
  -e HAIGOSMART_LAMPS="703e975dc388=kitchen,e5f6a7b8c9d0=desk" \
  -e TZ=Europe/Berlin \
  ghcr.io/mmichaelb/haigosmart:latest
```

The image bakes `HAIGOSMART_HEADLESS=true` and `HAIGOSMART_REGISTRY=/data/registry.json`
and nothing else — every setting in the table below works exactly as it does for
the binary. Details in [releasing.md](releasing.md); the short version:

- **`/data`** holds the registry and the self-signed TLS key. Both are caches, so
  running without a volume works too — the state simply does not outlive the
  container. A *bind* mount from the host needs to be writable by uid `65534`
  (`chown 65534 ./dir`); a named volume needs nothing.
- **`TZ`** is honoured; without it, records are UTC.
- **There is no shell in the image.** `docker exec` gives you nothing, by design.
  The records on standard output are the diagnosis.

No flags. Every setting came from the environment, and the records go to
standard output:

```json
{"time":"2026-08-28 14:03:12.100","level":"INFO","msg":"starting","config":{"listen":":1883","headless":true,"…":"…","mqtt-password":"(set)"},"since":"0s"}
{"time":"2026-08-28 14:03:12.101","level":"INFO","msg":"lamp configured","device":"703e975dc388","name":"kitchen","created":false,"renamed":false,"since":"1ms"}
{"time":"2026-08-28 14:03:12.102","level":"INFO","msg":"listening for bulbs","addr":":1883","registry":"/root/.config/haigosmart/registry.json","since":"2ms"}
{"time":"2026-08-28 14:03:19.882","level":"INFO","msg":"bulb connected","kind":"connected","device":"703e975dc388","name":"kitchen","since":"7.782s"}
```

That is a healthy startup: the configuration it is running with, one line per
configured lamp, the listener, then lamps connecting.

---

## Settings

Every setting has a flag and an environment variable. The variable name is the
flag name uppercased with `-` replaced by `_`, under `HAIGOSMART_`. There is no
exception to that rule, so a flag you find in `-help` always has a variable.

**Precedence**: built-in default < environment variable < command-line flag. When
a setting is given both ways, the flag wins and the server records that an
override happened — by name only, never by value.

| Flag | Environment | Default | Required | Meaning |
|---|---|---|---|---|
| `-listen` | `HAIGOSMART_LISTEN` | `:1883` | no | Address lamps connect to |
| `-headless` | `HAIGOSMART_HEADLESS` | `false` | no | Run with no terminal and no input |
| `-v` | `HAIGOSMART_V` | `false` | no | Debug records, including protocol traces |
| `-log` | `HAIGOSMART_LOG` | *(empty)* | no | Record destination. Empty means stdout when headless, a temp file otherwise |
| `-registry` | `HAIGOSMART_REGISTRY` | user config dir | no | Registry file. A cache, not the source of truth |
| `-command-timeout` | `HAIGOSMART_COMMAND_TIMEOUT` | `5s` | no | How long before a command is reported unconfirmed |
| `-lamps` | `HAIGOSMART_LAMPS` | *(empty)* | **when headless** | The lamps this instance serves |
| `-mqtt-broker` | `HAIGOSMART_MQTT_BROKER` | *(empty)* | no | `host:port`; empty disables Home Assistant |
| `-mqtt-username` | `HAIGOSMART_MQTT_USERNAME` | *(empty)* | no | Broker username |
| `-mqtt-password` | `HAIGOSMART_MQTT_PASSWORD` | *(empty)* | no | Broker password. Never appears in any record |
| `-mqtt-client-id` | `HAIGOSMART_MQTT_CLIENT_ID` | `haigosmart` | no | Client id presented to the broker |
| `-mqtt-prefix` | `HAIGOSMART_MQTT_PREFIX` | `haigosmart` | no | Base topic for state, availability, commands |
| `-mqtt-discovery-prefix` | `HAIGOSMART_MQTT_DISCOVERY_PREFIX` | `homeassistant` | no | Home Assistant discovery prefix |
| `-ct-min-kelvin` | `HAIGOSMART_CT_MIN_KELVIN` | `2700` | no | Kelvin at the lamp's warmest (percent 0) |
| `-ct-max-kelvin` | `HAIGOSMART_CT_MAX_KELVIN` | `6500` | no | Kelvin at the lamp's coolest (percent 100) |

### Credentials

The only secret is `HAIGOSMART_MQTT_PASSWORD`. Keep it out of the file you
commit: supply it from wherever your platform keeps secrets, and inject it as
that one variable.

It never appears in a record. The configuration renders itself with the password
replaced by `(set)` or `(unset)`, so there is no code path — including future
ones — that can print it. The startup record is safe to paste into an issue, and
so is everything after it.

### Bad settings

Everything is checked before a socket opens, so a mistake costs you a failed
start and nothing else:

```text
$ HAIGOSMART_CT_MIN_KELVIN=7000 ./haigosmartd -headless -lamps a1=x
haigosmartd: ct-min-kelvin (7000) must be below ct-max-kelvin (6500); an inverted
range would reverse every warmth value shown in Home Assistant

$ ./haigosmartd -headless
haigosmartd: no lamps are configured: set HAIGOSMART_LAMPS (or -lamps) to
deviceID=name pairs. An unattended instance serves only the lamps it is told
about, so one with an empty set would refuse every connection
```

A malformed lamp entry is reported by position and content — never skipped. A
silently dropped lamp presents later as a room that stopped working, with a
clean log and no explanation.

---

## Reading the records

One JSON object per line, on standard output. Every record carries:

| Field | Meaning |
|---|---|
| `time` | `2026-08-28 14:03:12.123`, local |
| `since` | Elapsed since this process started — how far into the run it happened |
| `level` | `DEBUG` / `INFO` / `WARN` / `ERROR` |
| `msg` | Fixed per kind of event, with no values in it, so you can group by it |
| `kind`, `device`, `name` | Which lamp, for lamp events |
| `detail` | The variable part: a disconnect reason, an error, an address |

`msg` never contains a value. To find every disconnect, filter on
`msg == "bulb disconnected"`; to see why one happened, read its `detail`.

### Healthy

`starting` → `lamp configured` (one per lamp) → `listening for bulbs` →
`bulb connected` and `bulb reported state` as lamps come up. With a broker
configured, also `home assistant integration enabled`.

### The three that mean something is wrong

| `msg` | Level | What it means |
|---|---|---|
| `bulb rejected` | WARN | A lamp connected that is not in `HAIGOSMART_LAMPS`. Either something is on your network that should not be, or you meant to configure this lamp and did not. `detail` has its address and `device` its id — that id is what to add. Repeats are rate-limited to one per five minutes per lamp, carrying the suppressed count, because a refused lamp reconnects forever |
| `registry lamp not configured` | WARN | The registry file knows a lamp your configuration does not name. It will not be served. Usually a lamp dropped from the manifest by accident |
| `saving the registry failed` | WARN once, then DEBUG | The registry file cannot be written — typically a read-only mount. **Not fatal**: the file is a cache, the configured lamps still work, and only the last-known state is lost across a restart. Reported loudly once and quietly after, because a read-only mount makes every save fail forever |

### When the output goes nowhere

If the record stream cannot be written — a closed pipe, a full disk — the server
prints one line to standard error and exits 1:

```text
haigosmart: cannot write the record stream: write /dev/stdout: broken pipe
```

An unattended server nobody can hear is not running, and restarting it is the
supervisor's decision.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Clean shutdown. `SIGTERM` or `SIGINT` — the signal `docker stop` and Kubernetes send first: stops accepting connections, records `shutting down` with the signal, saves the registry, exits. Takes tens of milliseconds, so any sane grace period is ample |
| `1` | Refused to start (an invalid setting, a port already in use, an unreadable registry), or the record stream failed while running. The reason is on standard error in every case |

`SIGTERM` is the signal Kubernetes sends first, and it is handled. A second `SIGTERM` during
shutdown is *not* a force-quit — the handler stays installed — which is harmless, because the
supervisor's own `SIGKILL` after the grace period is the escalation, and it always works.
Shutdown is fast enough that it never gets that far.

---

## Shutdown

`SIGTERM` or `SIGINT`: the server stops accepting connections, records
`shutting down` with the signal, saves what it knows about the lamps, and exits
with status 0. A command in flight is abandoned — the lamp keeps whatever state
it reached, and you are never told a command succeeded that did not.

---

## Home Assistant

Set `HAIGOSMART_MQTT_BROKER` and the lamps appear via MQTT discovery. Unattended,
the configured lamps are published at startup, before they connect — they show as
unavailable until the lamp is powered on. Full details in
[homeassistant.md](homeassistant.md).

---

## Migration note: the record format changed

Records used to interpolate values into the message
(`"disconnected (no keep-alive for 180s)"`). They no longer do: the message is
fixed per kind (`"bulb disconnected"`) and the variable part is in `detail`.

If you grep the log file for message text, move to the fields — filter on `msg`
for the kind of event and read `detail` for the specifics. The terminal display
is unaffected; it renders from the event, not from the record.
