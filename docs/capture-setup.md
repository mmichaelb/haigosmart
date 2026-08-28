# Capturing the bulbs' traffic

This is the procedure that produced `specs/001-local-bulb-server/contracts/bulb-protocol.md`.
It is kept so the work can be repeated for a bulb model this server does not yet
handle — an RGB one, for instance.

**The capture for the CCT bulb is already done.** You only need this if you add a
new model.

> **A caution learned the hard way.** This capture said "no TLS", and the flow
> file genuinely recorded `tls:false`. The same firmware in the field opens TLS
> 1.2 on the same port. One capture of one device on one evening is a sample,
> not a specification — prefer sniffing at runtime over asserting from a capture.

## What the existing capture found

| | |
|---|---|
| Endpoint | `47.254.156.103:1883` (Alibaba Cloud) |
| Transport | MQTT 3.1.1 on port 1883 — cleartext in this capture, **TLS 1.2 on live hardware** |
| Payload | plain JSON (Alibaba "Alink") |
| Identity | MQTT username `{deviceName}&{productKey}`; deviceName is the MAC |
| Keep-alive | declared 120 s, `PINGREQ` every ~60 s |
| Firmware | `aigo_light_cct_v4.0.0` — `cct` means white-only |

## Running a capture

Redirect the bulb's traffic to the machine running mitmproxy, then:

```bash
mitmdump \
  --mode transparent \
  --set connection_strategy=lazy \
  --set tcp_hosts='.*' \
  --set block_global=false \
  --ssl-insecure \
  --showhost \
  -w captures/bulb.mitm
```

Two of those flags are the ones that matter:

- `connection_strategy=lazy` — without it mitmproxy dials upstream before the
  client speaks, which hides the handshake on protocols where the server talks
  first.
- `tcp_hosts='.*'` — treat every host as a raw TCP tunnel instead of forcing the
  HTTP parser. This is what makes MQTT visible at all.

Capture the phone app the same way on a second port, with mitmproxy's CA
installed on the phone. Insurance, in case TLS interception fails:

```bash
sudo tcpdump -i <iface> -s0 -w captures/bulb.pcap host <bulb-ip>
```

Then convert to something readable:

```bash
mitmdump -nr captures/bulb.mitm -s scripts/dump_flows.py > captures/bulb.jsonl
```

## The interaction sequence

Do these in order, with the pauses. The pauses are what let a message be
attributed to the action that caused it.

| # | Action | Pause | Reveals |
|---|---|---|---|
| 0 | captures running, bulb off at the wall | 30 s | baseline |
| 1 | power the bulb on, do not touch the app | 90 s | handshake, registration, first state report |
| 2 | open the app, do not open the bulb | 30 s | login, device list |
| 3 | open the bulb's detail page | 30 s | **device metadata — where capabilities come from** |
| 4 | toggle off | 20 s | the power-off command |
| 5 | toggle on | 20 s | diff against 4 isolates the power field |
| 6 | brightness to minimum | 20 s | brightness low bound |
| 7 | brightness to maximum | 20 s | high bound; two extremes fix the range |
| 8 | brightness to ~50% | 20 s | confirms it is linear, not an enum |
| 9-11 | colour to red, then green, then blue | 20 s each | colour model and byte order (skip on CCT bulbs) |
| 12 | white / warm-white mode | 20 s | whether white is a separate channel |
| 13 | colour temperature warmest, then coolest | 20 s each | the temperature field and its range |
| 14 | **kill the app completely** | 120 s | **the keep-alive interval the server must honour** |
| 15 | power off at the wall | 30 s | disconnect behaviour |
| 16 | power on, app still closed | 120 s | the reconnect path the server serves |

With two bulbs, repeat steps 1-3 on the second one before stopping. Two device
identities side by side make it obvious which bytes are the id and which are
protocol constants.

## Turning a capture into code

1. Identify the platform from the destination hostname and port.
2. Fill in `specs/001-local-bulb-server/contracts/bulb-protocol.md`.
3. Extract fixtures into `internal/protocol/testdata/` as
   `<direction>_<messagetype>_step<N>.hex`, **scrubbing tokens and passwords
   with replacements of exactly the same length** — MQTT length prefixes are
   byte counts, and a shorter replacement corrupts the frame.
4. The fixtures become the codec's tests, so the tests agree with the hardware
   rather than with our reading of it.
