# Contract: Bulb Wire Protocol

**Feature**: 001-local-bulb-server | Spec: FR-001, FR-002, FR-006

> **STATUS: FILLED at gate G1** from `captures/bulb.jsonl` (203 lines, 101 TCP records,
> 2026-08-27), then **corrected on 2026-08-28 against physical hardware**. Where the
> capture and the live device disagree, both are recorded and the difference is called
> out — the capture was one bulb on one evening, and it turned out not to generalise.
> Fixtures live in `internal/protocol/testdata/`.

## 0. Platform verdict (research.md §3)

**Alibaba Cloud IoT Platform (Link Kit / "Alink")** — not Tuya, not Magic Home.

| Evidence | Value |
|---|---|
| Endpoint | `47.254.156.103:1883` (Alibaba Cloud) |
| Transport | MQTT 3.1.1 on port 1883, **cleartext in the capture, TLS 1.2 on live hardware** — see §1 |
| Payloads | **Plain JSON** (Alink), no encryption anywhere |
| ProductKey | `a1GGnyln558` |
| DeviceName | `703e975dc388` (the device MAC) |
| Firmware | `aigo_light_cct_v4.0.0` |
| SDK | `sdk-c-2.3.0_FY_1.6.6-16`, platform `ALI_AOS_TG7100CEVB` |

**This is close to the best possible outcome**: no reverse-engineered framing, no key
extraction, no PSK. The server is an MQTT broker subset plus a JSON message handler, behind
an optional TLS terminator. research.md §4 (the pinning fallback) is not needed — the bulb
does not validate the certificate it is given.

## 1. Transport

- Port **1883**, TCP. Endpoints seen: `47.254.156.103` (capture) and, by SNI on live
  hardware, `public.iot-as-mqtt.eu-central-1.aliyuncs.com` (Alibaba EU-Central).
- **Both cleartext and TLS occur on the same port**, so the server sniffs the first byte:
  `0x16` starts a TLS handshake record, anything else is MQTT.
  - *In the capture*: cleartext. The client id says `securemode=2`, which in Alibaba Link
    Kit means plain TCP, and the mitmproxy flow file records `tls:false`.
  - *On live hardware*: TLS 1.2. Same firmware version string, different behaviour —
    most likely because the device now reaches a different regional endpoint.
- **TLS parameters** (from a live `ClientHelloInfo`):
  - Version: TLS 1.2. SNI: the vendor hostname above.
  - Cipher suites offered, in order: `0x003D` `TLS_RSA_WITH_AES_256_CBC_SHA256`,
    `0x0035` `TLS_RSA_WITH_AES_256_CBC_SHA`, `0x003C` `TLS_RSA_WITH_AES_128_CBC_SHA256`,
    `0x002F` `TLS_RSA_WITH_AES_128_CBC_SHA`, plus `0x00FF` (renegotiation SCSV).
  - **All are RSA key exchange.** Two consequences, both of which cost a debugging round:
    Go disables these suites by default and they must be listed explicitly, and Go will
    not select any of them against an ECDSA certificate. The server's self-signed
    certificate is therefore **RSA-2048**. `0x003D` is not implemented by Go at all; the
    other three suffice.
  - **The bulb does not validate the certificate.** A self-signed certificate generated
    on demand for whatever SNI arrives is accepted. No client certificate is presented.
- The bulb speaks first: MQTT `CONNECT` immediately after the transport is up.

## 2. Framing

Standard MQTT 3.1.1 (protocol level 4). No vendor framing on top. Fixed header byte
(type<<4 | flags), varint remaining-length, then the body. Packet types observed:
`CONNECT(1) CONNACK(2) PUBLISH(3) PUBACK(4) SUBSCRIBE(8) SUBACK(9) PINGREQ(12) PINGRESP(13)`.

## 3. Encryption

**At the MQTT layer, none** — payloads are plaintext JSON in both cases. Confidentiality,
where present, comes from the TLS transport in §1, not from the protocol.

Authentication is a token in the client id plus an HMAC-SHA1 password. Since we *are* the
server, no verification is required — see §5.

## 4. Message types

| Direction | Topic | Trigger (capture step) | Purpose |
|---|---|---|---|
| bulb→server | `/sys/{pk}/{dn}/thing/event/property/post` | 1, 4–13 | State report (27 seen) |
| server→bulb | `/sys/{pk}/{dn}/thing/service/CommonService` | 4–13 | **Command** (26 seen) |
| bulb→server | `/sys/{pk}/{dn}/thing/service/CommonService_reply` | 4–13 | Command ack (26 seen) |
| bulb→server | `/sys/{pk}/{dn}/thing/deviceinfo/update` | 1 | Device attributes |
| bulb→server | `/ota/device/inform/{pk}/{dn}` | 1 | **Firmware version — the capability source** |
| bulb→server | `/ext/ntp/{pk}/{dn}/request` | periodic | Time sync (3 seen) |
| server→bulb | `/ext/ntp/{pk}/{dn}/response` | — | Time sync reply |
| bulb→server | `/sys/{pk}/{dn}/thing/lan/prefix/get` | 1 | LAN config query |
| bulb→server | `/sys/{pk}/{dn}/thing/awss/enrollee/match` | 1 | Pairing/enrollee |
| bulb→server | `/sys/{pk}/{dn}/thing/log/post` | 1 | Device logs |

The bulb subscribes to exactly one topic: `/sys/{pk}/{dn}/thing/event/+/post_reply` (QoS 0).
Commands arrive on `CommonService` without the bulb subscribing to it — the real broker
pushes it regardless, so our broker must do the same.

## 5. Handshake and registration (FR-001, FR-005)

Order from power-on, times relative to first byte:

```text
0.00  C→S  CONNECT
0.05  S→C  CONNACK (0x20 0x02 0x00 0x00 — session present 0, accepted)
0.07  C→S  SUBSCRIBE  /sys/{pk}/{dn}/thing/event/+/post_reply  QoS 0
0.07  S→C  SUBACK
0.08  C→S  PUBLISH  thing/deviceinfo/update
0.08  C→S  PUBLISH  /ota/device/inform/...   {"params":{"version":"aigo_light_cct_v4.0.0"}}
0.10  S→C  PUBACK / deviceinfo update_reply  {"code":200,...}
0.13  C→S  PUBLISH  thing/awss/enrollee/match
0.26  C→S  PUBLISH  thing/event/property/post   ← full initial state
```

**CONNECT fields** (the identity source):

```text
protocol   MQTT level 4, flags 0xc0 (username+password), keep-alive 120 s
client id  a1GGnyln558.703e975dc388|securemode=2,tokenType=0,token=<32 hex>,
           _v=sdk-c-2.3.0_FY_1.6.6-16,timestamp=2524608000000,signmethod=hmacsha1,
           lan=C,pid=ALI_AOS_TG7100CEVB,mid=TG7100CEVB,authtype=custom-ilop,_fy=1.6.6-16,_ss=1|
username   703e975dc388&a1GGnyln558          ← {deviceName}&{productKey}
password   645f6dc36928ee2abe27685335736d5378de56d7   (HMAC-SHA1)
```

- **Stable device identifier**: `deviceName` — the part before `&` in the username, and the
  part after `.` in the client ID. It is the device MAC (`703e975dc388`) and is stable
  across reconnects and power cuts.
- **What the server must reply**: `CONNACK` accepted (`20 02 00 00`). The password is an
  HMAC over a shared secret we do not have and do not need — we are the authority now, so
  the server **accepts any credentials** and takes the identity from the username. This is
  safe on a trusted LAN (spec assumption) and is the whole point of the replacement.
- **Reconnect after a hard power cut** is byte-identical to the first connect. No special
  path (FR-005: same `deviceName` → same registry entry).

### 5a. Capabilities (FR-010)

The protocol reports **firmware version**, and the model is encoded in it:
`aigo_light_cct_v4.0.0` → **`cct` = correlated colour temperature, white-only**.

| Signal | Source | Result for this device |
|---|---|---|
| Colour support | model token in `/ota/device/inform` version string | **`Color: false`** |
| Colour-temp support | same, plus `ColorTemperature` present in `property/post` | `ColorTemp: true` |
| Brightness floor | lowest `Brightness` accepted in capture steps 6/8 | `MinBrightness: 1` |
| Determined? | version string parsed successfully | `Known: true` |

Rules implemented in `internal/protocol/capabilities.go`:
- version contains `_rgb` / `_rgbcct` → `Color: true, ColorTemp: true`
- version contains `_cct` → `Color: false, ColorTemp: true`
- version parsed but no known token, or no version received → **`Known: false`**, and
  colour commands are *attempted* rather than pre-refused (data-model.md).
- Fallback confirmation: the initial `property/post` lists exactly the properties the
  device has. This capture shows no RGB property of any kind, corroborating CCT-only.

> **Deviation from the spec worth stating plainly**: FR-010 assumes colour control. This
> hardware is white-only, so `color` is answered with the documented "does not support
> colour" error. That is the spec's own stated behaviour for white-only models, not a gap.
> No RGB property mapping is implemented, because none was observed — it goes in when an
> RGB model is captured.

## 6. Keep-alive (FR-006)

- CONNECT declares **keep-alive 120 s**.
- Bulb sends `PINGREQ` (`c0 00`) roughly every **60 s** — half the declared interval.
  Observed at t=71.8, 120.1, 180.1 (deltas 48.2 s, 60.0 s).
- Server replies `PINGRESP` (`d0 00`).
- Server-side disconnect threshold: MQTT convention is 1.5× keep-alive. With 120 s
  declared, mark the bulb `Disconnected` after **180 s** of silence.

## 7. State report (FR-013, FR-019)

The bulb reports **unprompted** on every change. No polling needed.

Initial full state (capture t=0.26) — this is also the property inventory:

```json
{"id":"5","version":"1.0","method":"thing.event.property.post",
 "params":{"LightType":1,"LightSwitch":1,"WorkMode":0,"LightMode":0,
           "ColorTemperature":2,"Brightness":30,
           "LightScene":{"SceneItems":"...","ColorArr":"[]","SceneId":"0",
                         "LightMode":0,"ColorSpeed":5,"Enable":0,"SceneMode":0}}}
```

Subsequent reports are **deltas**, and each value is wrapped with a device timestamp:

```json
{"id":"11","version":"1.0","method":"thing.event.property.post",
 "params":{"Brightness":{"value":100,"time":1787788293341},
           "CommonServiceResponse":{"value":{"seq":"10000@1787788298998"},
                                    "time":1787788293341}}}
```

**Both shapes must be parsed**: a bare scalar (initial post) and a `{"value":…,"time":…}`
wrapper (deltas). `CommonServiceResponse` echoes the `seq` of the command that caused the
change — that is how a report is correlated to a command.

The server replies on `/sys/{pk}/{dn}/thing/event/property/post_reply` with
`{"code":200,"data":{},"id":"<same id>","message":"success","method":"thing.event.property.post","version":"1.0"}`.

## 8. Commands (FR-008 to FR-010)

Server publishes to `/sys/{pk}/{dn}/thing/service/CommonService`:

```json
{"method":"thing.service.CommonService","id":"1865450001","version":"1.0.0",
 "params":{"flag":0,"method":0,"params":"{\"LightSwitch\":1}",
           "seq":"10000@1787788228900"}}
```

Note `params.params` is a **JSON string**, not an object — double-encoded. `seq` format is
`10000@<unix-millis>`. `id` is a random numeric string.

### 8a. One property per command (confirmed on hardware)

**Every command carries exactly one property.** All 11 commands in the capture do, even
where the user changed two things at once — those arrive as two messages microseconds
apart. A command bundling several properties is **silently ignored**: no
`CommonService_reply`, no state report, nothing at all until the caller times out.

The server therefore sends only properties that actually changed, one message each,
ordered: power-on first so attributes land on a lit bulb, power-off last and alone.

### 8b. Confirmation and timing

The bulb confirms a command **twice**, and either alone is sufficient:

1. `CommonService_reply` carrying the command's `id`.
2. A property post echoing the command's `seq` in `CommonServiceResponse`.

The second is the stronger evidence — it says the change happened, not merely that the
message arrived — so the server completes a command on whichever arrives first.

**Latency is not uniform.** `on`, `off` and colour-temperature changes confirm in
0.1–0.6 s, matching the capture. **Brightness changes ramp**: the bulb fades to the new
level and reports only when the fade completes. A 100→1 change was measured at ~4 s on
hardware. Any timeout must accommodate this; the server allows 10 s by default.

| Command | Property | Observed range | Normalisation |
|---|---|---|---|
| power on / off | `LightSwitch` | `0` \| `1` | `bool` |
| brightness | `Brightness` | **1–100** (never 0) | identity — already 0–100 |
| colour temperature | `ColorTemperature` | **0–100** (0=warmest, 100=coolest) | **percent, NOT Kelvin** |
| colour | — | not present on this model | unsupported |

The bulb acks twice: `CommonService_reply` with `{"id":"<same id>","code":200,"data":{}}`
(correlated by `id`), and a `property/post` carrying the new value plus the echoed `seq`.
Ack latency observed: **0.1–0.6 s** — comfortably inside SC-003's 1 s.

Out-of-range values were not exercised in the capture; the server validates before sending
(data-model.md), so out-of-range never reaches the wire (FR-016).

## 9. Error and edge behaviour

- Unparseable frame: not exercised. The server drops the connection and logs, per FR-016.
- The bulb tolerates commands for properties it does not have by ignoring them; the server
  refuses them earlier via capabilities, which is the better error (§5a).
- `/ext/ntp/.../request` must be answered or the bulb retries; the reply carries server
  send/receive millis. Unanswered NTP is not fatal but is noisy.

## 10. Fixtures

In `internal/protocol/testdata/`, named `<direction>_<messagetype>_step<N>.hex`.
Tokens, the HMAC password, and the auth token are replaced with clearly marked
placeholders; `deviceName`/`productKey` are kept because they are LAN-local identifiers and
the tests need a realistic device identity.
