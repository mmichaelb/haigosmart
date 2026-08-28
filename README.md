# haigosmart

A local replacement for the Aigo cloud. Your smart bulbs connect to this instead
of the vendor's servers, and you drive them from a terminal.

```text
┌──────────────────────────────────────────────────────────────┐
│ haigosmart · 1 bulbs · 1 connected                           │
├──────────────────────────────────────────────────────────────┤
│ 14:02:11  kitchen      connected                             │
│ 14:02:11  kitchen      power off→on  brightness 40→80        │
│ 14:02:19  kitchen      power on→off                          │
├──────────────────────────────────────────────────────────────┤
│ > on kitchen                                                 │
└──────────────────────────────────────────────────────────────┘
```

## What it does

Aigo bulbs talk MQTT to Alibaba Cloud on port 1883 with JSON payloads —
sometimes in the clear, sometimes over TLS, so the server handles both on one
port. The bulb does not check the certificate it is given, so pointing that
hostname at this server is enough: no firmware change, no modification, the bulb
cannot tell the difference. Nothing leaves your network.

## Install

Three ways, pick one.

**With Go:**

```bash
go install github.com/mmichaelb/haigosmart/cmd/haigosmartd@latest
```

**Download a binary** — Linux, macOS, and Windows, on Intel and ARM — from the
[latest release](https://github.com/mmichaelb/haigosmart/releases/latest), and
check it:

```bash
curl -LO https://github.com/mmichaelb/haigosmart/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

**Pull the container** (the unattended server; adopting a lamp needs a terminal):

```bash
docker pull ghcr.io/mmichaelb/haigosmart:latest
```

Or build from a clone:

```bash
go build -o haigosmartd ./cmd/haigosmartd
```

## Quick start

```bash
haigosmartd
```

Redirect `47.254.156.103:1883` to this machine, power a bulb on, then:

```text
> list
> name 703e975dc388 kitchen     # naming a new bulb adopts it
> on kitchen
> bri kitchen 80
> temp kitchen 20               # white warmth: 0 warmest, 100 coolest
```

`help` explains the rest.

## Supported hardware

Tested against a physical `aigo_light_cct_v4.0.0` — on/off, brightness, and
white warmth all work from the terminal. This model has **no colour channel**,
so `color` says so rather than pretending. Adding an RGB model needs a packet
capture; see [docs/capture-setup.md](docs/capture-setup.md).

Brightness changes fade, and the bulb reports only once the fade finishes — up
to about four seconds for a large swing. The interface stays responsive
throughout; `-command-timeout` adjusts the limit.

## Home Assistant

The lamps work from Home Assistant as ordinary light entities — dashboards,
automations, voice. You need an MQTT broker (the Mosquitto add-on); this project
publishes to it and Home Assistant discovers the lamps on its own.

```bash
./haigosmartd -mqtt-broker 192.168.1.10:1883
```

A white-only lamp shows brightness and warmth and no colour wheel, because the
entity only claims what the hardware has. See
[docs/homeassistant.md](docs/homeassistant.md).

## Docs

- [docs/deploying.md](docs/deploying.md) — adopting a lamp, then running it
  unattended from environment variables or a container; every setting, and how
  to read the records
- [docs/releasing.md](docs/releasing.md) — how releases are built and published,
  and what to do when one fails
- [docs/operating.md](docs/operating.md) — running it, systemd, verifying no
  traffic escapes, recovering a corrupt registry
- [docs/capture-setup.md](docs/capture-setup.md) — how the protocol was worked
  out, and how to do it for another model
- [docs/homeassistant.md](docs/homeassistant.md) — broker setup, adoption, availability, diagnosis
- [docs/performance.md](docs/performance.md) — benchmark and soak baselines
- [specs/001-local-bulb-server/](specs/001-local-bulb-server/) — spec, plan,
  and the protocol contract

## Licence

GPL-3.0. See [LICENSE](LICENSE). You may use, modify, and redistribute this
code; if you distribute a modified version, its source must be published under
the same terms.

## Development

```bash
make check     # fmt, vet, race tests
make bench     # benchmarks
```

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):
they decide the next version, so `feat:` and `fix:` ship and everything else does
not. See [CONTRIBUTING.md](.github/CONTRIBUTING.md).

Built with the Go standard library throughout; the only dependency is Bubble Tea,
and only `internal/tui` imports it. The MQTT client is written against the same
codec that serves the bulbs, so the Home Assistant integration added no
dependencies at all.
