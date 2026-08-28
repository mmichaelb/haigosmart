# Running haigosmartd

## Getting the bulbs to talk to you

The bulbs reach Alibaba Cloud on port 1883 — `47.254.156.103` in one capture,
`public.iot-as-mqtt.eu-central-1.aliyuncs.com` by SNI on live hardware. Point
that at this machine with a DNS override, a hosts entry on your router, or a
DNAT rule. Nothing on the bulb changes; it has no idea it is talking to you.

The bulb may connect in the clear or over TLS. The server handles both on the
one port, generating a self-signed RSA certificate for whatever hostname the
bulb asks for; the bulb does not check it. The key is kept next to the registry
as `tls.key` so the certificate is stable across restarts. Delete it and a new
one is generated — nothing breaks.

Then run the server on port 1883:

```bash
go build -o haigosmartd ./cmd/haigosmartd
./haigosmartd
```

Port 1883 is below 1024 on some systems' reckoning — it is not, so no privileges
are needed. If something else holds the port, `-listen` moves it, but then the
redirect has to move too.

## First run

A bulb that connects for the first time shows up as **discovered**. It is
visible but not controllable until you name it:

```text
> list
> name 703e975dc388 kitchen
ok      kitchen: adopted (was 703e975dc388)
> on kitchen
ok      kitchen: on
```

Naming is adoption; there is no separate command. Everything after that survives
restarts.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-listen` | `:1883` | where to accept bulb connections |
| `-registry` | `$XDG_CONFIG_HOME/haigosmart/registry.json` | where the bulb list lives |
| `-log` | temp file, or stderr with `-headless` | structured JSON logs |
| `-headless` | `false` | no terminal interface; for systemd |
| `-v` | `false` | debug logging, including the TLS ClientHello a bulb offers |
| `-command-timeout` | `10s` | how long to wait for a bulb to confirm a command |

With the terminal interface running, logs go to a file rather than stderr — on
stderr they would scribble over the display.

## Under systemd

```ini
[Unit]
Description=haigosmart local bulb server
After=network-online.target

[Service]
ExecStart=/usr/local/bin/haigosmartd -headless -log /var/log/haigosmart.log
Restart=always
User=haigosmart

[Install]
WantedBy=multi-user.target
```

## Verifying no traffic leaves

The point of the exercise. With the server running:

```bash
sudo tcpdump -i <iface> host <bulb-ip> and not net 192.168.0.0/16
```

Zero packets is the correct result. Anything else means the redirect is leaking.

## Recovering a corrupt registry

A corrupt registry file is a startup error, not a silent reset — the file is left
exactly as it was so you can look at it:

```text
haigosmartd: registry /path/registry.json is corrupt: unexpected end of JSON input.
the file was left untouched; inspect or restore it, or move it aside to start fresh
```

Move it aside and every bulb comes back as discovered, needing to be renamed
once. Nothing is lost but the names.

## What this build supports

The tested hardware is `aigo_light_cct_v4.0.0`: on/off, brightness 1-100, and
white warmth 0-100 (0 warmest, 100 coolest). It has **no colour channel**, so
`color` reports that plainly rather than pretending.

Brightness changes fade and are only confirmed once the fade completes — about
four seconds for a 100-to-1 swing. If you see a command time out that then
visibly works, raise `-command-timeout`. An RGB model needs a capture
(see [capture-setup.md](capture-setup.md)) and a property mapping; the capability
detection already has a slot for it.
