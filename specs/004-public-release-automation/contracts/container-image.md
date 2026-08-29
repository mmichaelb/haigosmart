# Contract: Container Image

**Feature**: 004-public-release-automation

The image is feature 003's unattended server, packaged. This contract says what the packaging
adds — and it adds very little on purpose, because every container-only behaviour is a second
thing to document and a second place for the two to drift apart.

## Address

```text
ghcr.io/mmichaelb/haigosmart:X.Y.Z     # exact version, immutable
ghcr.io/mmichaelb/haigosmart:X.Y        # moving, latest patch of that minor
ghcr.io/mmichaelb/haigosmart:X          # moving, latest minor of that major
ghcr.io/mmichaelb/haigosmart:latest     # moving, most recent release
```

Public: no `docker login` to pull. Architecture is selected by the manifest list —
`linux/amd64` and `linux/arm64` — so the pull command is the same on every machine.

## Contents

`scratch`, one binary, and an empty `/data` owned by `65534`. No shell, no package manager, no
CA bundle, no `/etc/passwd`.

The empty directory is not incidental. `scratch` has no filesystem for a volume to inherit
ownership from, so without it a volume mounted at `/data` is created root-owned and the server
— running as `65534` — cannot write the registry. It still serves, warning on every save, which
is the sort of half-working that takes a while to notice.

The consequence, stated up front rather than discovered: **`docker exec` gives you nothing.**
There is no shell to run. Diagnosis is the JSON record stream on standard output, which is
what feature 003 built it for. If that stream is not enough to diagnose a problem, the fix is
a better record, not a bigger image.

## Runtime surface

| Property | Value |
|---|---|
| Entrypoint | `/haigosmartd` |
| Default arguments | none — everything comes from the environment |
| User | `65534:65534`, non-root |
| Port | `1883` |
| Volume | `/data` |
| Baked variables | `HAIGOSMART_HEADLESS=true`, `HAIGOSMART_REGISTRY=/data/registry.json` |

Both baked variables are overridable like any other. They are defaults, not constraints.

## Configuration

Exactly the environment variables in [`docs/deploying.md`](../../../docs/deploying.md). The
image introduces no setting of its own and renames nothing. There is one settings table for
this project and it does not gain a container column.

Minimum to run:

```bash
docker run --rm \
  -p 1883:1883 \
  -v haigosmart-data:/data \
  -e HAIGOSMART_LAMPS="703e975dc388=kitchen" \
  ghcr.io/mmichaelb/haigosmart:X.Y.Z
```

`HAIGOSMART_LAMPS` is required because the server is headless, and headless serves exactly
the lamps it is told about. Adoption still happens at a terminal — see below.

## Behaviour

Identical to the unattended binary (FR-024):

| Aspect | Behaviour |
|---|---|
| Records | JSON lines on standard output, one object per line, same fields |
| Exit 0 | Clean shutdown on `SIGTERM` or `SIGINT` |
| Exit 1 | Refused to start, or the record stream failed |
| Shutdown | `SIGTERM` stops accepting, saves the registry, exits — tens of milliseconds |
| Unknown lamps | Refused with CONNACK `0x05`, rate-limited warning, nothing persisted |

`SIGTERM` is what `docker stop` and Kubernetes send, and it is handled, so the default grace
period is never approached.

## Time zones

The binary embeds the zone database (`-tags timetzdata`), so `TZ=Europe/Berlin` works despite
`scratch` having no `/usr/share/zoneinfo`. Without `TZ`, records are UTC.

This is worth being explicit about because the failure it prevents is silent: a `TZ` that has
no effect produces timestamps that are wrong by hours and look perfectly well-formed.

## The `/data` volume

Holds `registry.json` and `tls.key`.

Neither is precious, and it is worth knowing which kind of "not precious" each is:

- **`registry.json`** is a cache. The configured lamp set is authoritative, so losing this
  costs only the last-known state, which lamps report again when they connect.
- **`tls.key`** is a self-signed key the server generates for itself. Losing it means a new
  certificate next start. The lamps do not verify certificates, so they do not notice.

Running with no volume at all works and writes into the container's own `/data`, so there is
no warning — the state simply does not outlive the container. Mounting `/data` is recommended,
not required, and either way the operator gets a working server rather than a broken one.

## What the image does not do

**Adoption.** Naming a lamp is a deliberate act at a terminal, and headless mode does not
discover lamps at all. The path is unchanged from feature 003: adopt interactively once with
a locally built or downloaded binary, note each device id, then configure
`HAIGOSMART_LAMPS` and run the container.

A container that could adopt would mean an unattended process deciding which lamps on the
network belong to it, which is the opposite of the guarantee headless mode makes.
