# Phase 0 Research: Local Replacement Server for Aigo Smart Bulbs

**Feature**: 001-local-bulb-server | **Date**: 2026-08-27

The one blocking unknown is the bulbs' wire protocol. Sections 1–3 are the operator
runbook that produces the evidence; section 4 is what happens if that evidence is
encrypted; sections 5–9 record the technology decisions that do not depend on the capture.

---

## 1. Capture setup (mitmproxy)

You already know how to redirect the bulb's traffic. What follows is only what to run on
the machine receiving that redirected traffic, and what to keep afterwards.

### 1.1 Prerequisites

```bash
mitmdump --version        # note the major version; option names differ across 10.x / 11.x
mkdir -p captures
```

Confirm the two non-obvious options exist in your build before relying on them:

```bash
mitmdump --options | grep -E 'tcp_hosts|connection_strategy|ssl_insecure'
```

If `tcp_hosts` is absent in your version, capture the bulb with `tcpdump` (§1.5) instead —
do not skip the bulb capture.

### 1.2 Capture A — the bulb (the important one)

The bulb almost certainly does not speak plain HTTP, so mitmproxy must be told to proxy
arbitrary TCP rather than trying to parse HTTP:

```bash
mitmdump \
  --mode transparent \
  --set connection_strategy=lazy \
  --set tcp_hosts='.*' \
  --set block_global=false \
  --ssl-insecure \
  --showhost \
  --set termlog_verbosity=info \
  -w captures/bulb.mitm
```

- `--mode transparent` — matches a router/iptables redirect. If you instead redirect by
  DNS to a fixed destination, swap in `--mode reverse:tcp://<vendor-host>:<port>` (or
  `reverse:https://<vendor-host>` for an HTTP endpoint) and keep every other flag.
- `--set connection_strategy=lazy` — **required** for non-HTTP traffic. Without it
  mitmproxy opens the upstream connection before it has seen a client byte, which breaks
  protocols where the server speaks first and hides the handshake you need.
- `--set tcp_hosts='.*'` — treat every host as a raw TCP tunnel instead of forcing the
  HTTP parser. This is what makes MQTT and custom binary protocols show up at all.
- `--ssl-insecure` — do not verify the vendor's certificate upstream.
- `--showhost` — display the real destination hostname rather than the redirected IP;
  the hostname is what you will later point at your own server.
- `-w captures/bulb.mitm` — the binary flow file. Keep it; it is the source of truth.

Leave this running for the entire interaction sequence in §2. Expect the bulb to either
(a) complete a TLS handshake against mitmproxy's CA, which means no pinning and you get
plaintext, or (b) abort the handshake, which means pinning — see §4.

### 1.3 Capture B — the phone app

Run this as a **second** mitmdump on a different port, with the phone configured to use it
and mitmproxy's CA installed on the phone. The app's traffic is what tells us the meaning
of each field (which value is brightness, which is colour temperature); the bulb capture
tells us the framing.

```bash
mitmdump \
  --set connection_strategy=lazy \
  --set tcp_hosts='.*' \
  --ssl-insecure \
  --showhost \
  -w captures/app.mitm
```

If the app refuses to connect, it pins too — capture what you can and note it; the bulb
capture is the one that matters for the server.

### 1.4 Producing readable dumps

The `.mitm` files are binary. After the interaction sequence, generate the text form:

```bash
# quick human-readable overview
mitmdump -nr captures/bulb.mitm --set flow_detail=4 > captures/bulb.txt
mitmdump -nr captures/app.mitm  --set flow_detail=4 > captures/app.txt

# structured, hex-preserving dump (both HTTP and raw TCP messages)
mitmdump -nr captures/bulb.mitm -s scripts/dump_flows.py > captures/bulb.jsonl
mitmdump -nr captures/app.mitm  -s scripts/dump_flows.py > captures/app.jsonl
```

`scripts/dump_flows.py` is committed with this feature. It emits one JSON object per
message with direction, timestamp, peer, length, hex, and printable-ASCII rendering —
that is the file to hand back for analysis, because `flow_detail` alone mangles binary
payloads.

**Leave everything in `captures/`.** That directory is gitignored; nothing from it is
committed except the small byte fixtures later extracted into
`internal/protocol/testdata/`.

### 1.5 Fallback / supplement — packet capture

Cheap insurance, and the only option if TLS interception fails outright. Run it alongside
mitmproxy:

```bash
sudo tcpdump -i <iface> -s0 -w captures/bulb.pcap host <bulb-ip>
```

Even a fully encrypted pcap still yields the destination hostnames, ports, packet
cadence, and keep-alive interval — all of which the replacement server must reproduce.

---

## 2. App interaction sequence

Do these **in this order**, with the pauses. The pauses matter more than they look: they
create idle gaps in the capture that separate one action from the next, so a message can
be attributed to the action that caused it without guessing.

Keep a paper log of wall-clock time per step. Announce nothing to the app between steps.

| # | Action | Pause after | What it should reveal |
|---|---|---|---|
| 0 | Both captures running; bulb powered **off** at the wall | 30 s | Baseline: idle traffic, if any |
| 1 | Power the bulb on at the wall; do **not** touch the app | 90 s | Boot handshake, registration/auth, first state report, initial keep-alives |
| 2 | Open the Aigo Smart app; do not open the bulb yet | 30 s | App login and device-list fetch |
| 3 | Open the bulb's detail page | 30 s | Device metadata and full state query — usually the clearest field list you will get |
| 4 | Toggle **off** | 20 s | The power-off command and the bulb's confirmation |
| 5 | Toggle **on** | 20 s | The power-on command; diff against step 4 isolates the power field |
| 6 | Brightness to **minimum** | 20 s | Brightness field at its low bound |
| 7 | Brightness to **maximum** | 20 s | Brightness field at its high bound — two extremes fix the range and byte width |
| 8 | Brightness to roughly **50%** | 20 s | Confirms it is linear and not a step enum |
| 9 | Colour to pure **red** | 20 s | Colour encoding; red is chosen because it is unambiguous in RGB, HSV, and HSL |
| 10 | Colour to pure **green** | 20 s | Second axis |
| 11 | Colour to pure **blue** | 20 s | Third axis; the three together determine the colour model and byte order |
| 12 | Colour to **white / warm-white mode** if the bulb has one | 20 s | Whether white is a separate channel or a colour value |
| 13 | Set colour temperature to warmest, then coolest (skip if unsupported) | 20 s each | Colour-temperature field and range |
| 14 | Close the app completely (kill it) | 120 s | Keep-alive behaviour with no app attached — **this is the interval the server must honour** (FR-006) |
| 15 | Power the bulb **off** at the wall | 30 s | Disconnect behaviour / what the cloud sees on a hard drop |
| 16 | Power the bulb back **on**, app still closed | 120 s | Reconnect without the app — the exact path the replacement server must serve |
| 17 | Stop both captures, run §1.4 | — | — |

Total: roughly 15 minutes.

**If you have two bulbs, repeat steps 1–3 with the second one before stopping the
capture.** Two device identifiers side by side make it obvious which bytes are the device
ID and which are protocol constants — that single fact usually saves hours.

---

## 3. Protocol identification

**Decision**: Identify the family from the capture rather than assuming one. Cheap bulbs
sold under many brand names are usually one of a handful of platforms, and the destination
hostname plus port pins it down immediately.

| Signal in the capture | Likely platform | Consequence for the server |
|---|---|---|
| Hostnames under `*.tuya*.com` / `*.tuyaeu.com`; MQTT on 8883; local TCP 6668 | Tuya OEM | Well-documented; AES-128 with a per-device local key. The key is recoverable from the app capture or the device pairing exchange. Highest likelihood for a rebranded bulb |
| TCP 5577, short plaintext binary frames with a trailing checksum | Magic Home / Zengge / Flux LED | Easiest case: no crypto, frames are a few bytes |
| Hostnames under `*.ibroadlink.com`; UDP discovery on 80 | Broadlink | AES-128-CBC with a fixed initial key, then a session key |
| Raw MQTT (`CONNECT` packet visible, `MQTT` magic in the first bytes) to a vendor host | Generic MQTT | Reimplement a small MQTT broker subset; topics carry the device ID |
| Plain HTTP/JSON polling | Vendor-specific HTTP | Simplest of all; the server is an HTTP handler |
| TLS handshake completes but payload is unknown binary | Custom over TLS | Reverse the framing from the capture; the server terminates TLS with its own cert |

**Rationale**: an assumption here propagates into every layer. One look at the destination
hostname in `captures/bulb.txt` settles it.

**Alternatives considered**: assuming Tuya and building for it directly — rejected because
being wrong costs the entire protocol layer, and the check costs one command.

**Output of this step**: fill in `contracts/bulb-protocol.md` and drop the raw byte
sequences into `internal/protocol/testdata/` as fixtures. Those fixtures become the codec's
unit tests (Constitution II), so the tests are grounded in real hardware behaviour rather
than in our reading of it.

---

## 4. If the capture is opaque (pinning or unrecoverable keys)

In order of effort, stop at the first that works:

1. **Re-read the handshake.** Many devices negotiate a session key in the clear on first
   contact and only then encrypt. Check the first few frames before assuming pinning.
2. **Pull the key from the app capture.** If the phone accepted mitmproxy's CA, the local
   key or device secret is usually visible in the device-list response from step 3.
3. **Check the pairing flow.** Re-pair the bulb with the capture running. Pairing usually
   transmits credentials over the softAP/UDP path in the clear.
4. **Extract from the app package.** Static extraction of an embedded key from the
   installed app.
5. **Firmware dump over UART/SPI.** Last resort — physical access, real effort. If the
   plan reaches this point, stop and re-plan rather than absorbing it silently.

If none succeed, the feature is not deliverable as specified and the operator needs to hear
that, not a partial server that never gets a bulb to connect.

---

## 5. Language and standard library

**Decision**: Go 1.27, standard library for everything except the TUI.

**Rationale**: `net` and `crypto/tls` cover a custom TCP/TLS server with no help;
`log/slog` covers structured logging (FR-018); `encoding/json` covers persistence;
goroutine-per-connection is the natural fit for ~30 long-lived sockets; the result is a
single static binary that drops onto a Raspberry Pi. Constitution: new dependencies need a
justification, and none of these would have one.

**Alternatives considered**: an existing home-automation platform (Home Assistant, Tasmota,
ESPHome) — rejected because the bulbs are unmodified and no platform supports an
unidentified protocol; identifying it is the work either way.

---

## 6. TUI

**Decision**: Bubble Tea (`bubbletea` + `bubbles/textinput` + `lipgloss`), confined to
`internal/tui`.

**Rationale**: FR-013 and FR-015 together require a scrolling event feed and a live command
prompt on screen at once, staying responsive while events arrive. On a raw terminal that
means implementing raw mode, differential redraw, resize handling, and input buffering by
hand — several hundred lines of exactly the code that breaks in edge cases (spec edge case:
terminal resize). Bubble Tea's `Update`/`View` loop with a channel-fed `tea.Msg` maps
directly onto the event bus, and it is the smallest maintained option for this shape.

**Alternatives considered**:
- **Plain line-oriented REPL** — `bufio.Scanner` on stdin, events printed via `slog`.
  Zero dependencies and roughly 50 lines. Rejected as the default because printed events
  interleave with what the operator is typing, which the spec's responsiveness requirement
  is specifically about. **Kept as a documented fallback**: `internal/tui` is the only
  package importing Bubble Tea, so swapping it costs one package and nothing else. If the
  dependency ever becomes a burden, this is the exit.
- **tview / termui** — heavier and more transitive dependencies for the same result.
- **Web UI** — contradicts "simple TUI" and adds an HTTP surface the spec never asked for.

---

## 7. Persistence

**Decision**: A single JSON file in `os.UserConfigDir()/haigosmart/registry.json`, written
atomically (write temp file in the same directory, `fsync`, `os.Rename`).

**Rationale**: FR-004 requires the registry to survive restarts. Thirty records that change
a few times a day do not need a database. Atomic rename is the standard defence against a
truncated file after a crash mid-write, and it is four lines.

**Alternatives considered**: SQLite (a dependency and a build-tag headache for a
30-row table), BoltDB (same objection), append-only log (needs compaction we would then
have to maintain).

---

## 8. Concurrency and the event bus

**Decision**: One goroutine per bulb connection. A central registry guarded by a
`sync.RWMutex`. Events published to subscribers over buffered channels with a
**drop-oldest** policy when a subscriber falls behind.

**Rationale**: a slow or paused TUI must never stall a bulb's read loop, so the bus drops
rather than blocks and reports the drop count so nothing is lost silently. This is a
correctness property — the alternative is a terminal that can silently disconnect bulbs —
and it is proven by race-detected tests, not by a performance target. `context.Context` cancellation gives clean
shutdown of every connection on SIGINT.

**Note on SC-008 / SC-009**: the spec was amended (2026-08-27) to separate the two claims
this design actually makes. SC-008 is about the *record* — every event reaches the
structured log unconditionally, so the audit trail is complete. The drop-oldest policy
applies only to the *display* buffer under pathological load, and the count of
not-shown events is surfaced rather than hidden. SC-009 covers the reason for the policy:
a busy feed must never block the operator's ability to type a command. Both are tested
separately.

**Alternatives considered**: unbounded queues (memory growth under a misbehaving bulb),
blocking sends (couples the TUI's speed to the network path, so a paused terminal could drop
a bulb off the network — a correctness failure, not a slow one).

---

## 9. Testing strategy

**Decision**: `internal/bulb/fakebulb` implements the bulb side of the protocol over a real
`net.Pipe` or loopback listener. Codec tests are table-driven over byte fixtures taken
verbatim from `captures/`.

**Rationale**: Constitution II requires integration tests across the connection boundary,
and hardware-in-the-loop tests cannot run in CI. A fake that speaks the real bytes covers
registration, reconnection, keep-alive, command round-trip, and malformed input (FR-016)
without a physical bulb. It is also the only way to test the 30-connection soak (SC-005)
repeatably.

The fake is written **after** G1, from the fixtures — writing it from our assumptions first
would produce a test suite that agrees with our misunderstanding.

---

## Open items carried into Phase 2

| Item | Resolved by | Blocking |
|---|---|---|
| Transport, framing, encryption | G1 (capture analysis) | `internal/protocol`, `internal/server` |
| Keep-alive interval and format | Step 14 of §2 | FR-006 |
| Device identifier field | Steps 1 and 16 of §2, two-bulb comparison | FR-003, FR-005 |
| Colour model and byte order | Steps 9–12 of §2 | FR-010 |
| Whether the bulb needs TLS with a specific SNI/cert | §1.2 handshake outcome | `internal/server` listener setup |
