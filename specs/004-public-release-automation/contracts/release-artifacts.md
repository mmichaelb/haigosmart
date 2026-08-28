# Contract: Release Artefacts

**Feature**: 004-public-release-automation

What every release must carry, and what each thing is called. This is the contract a user
relies on when they write a download script, and the checklist the release gate verifies.

## Asset names

For version `X.Y.Z`, the release carries exactly seven assets:

```text
haigosmart_X.Y.Z_linux_amd64.tar.gz
haigosmart_X.Y.Z_linux_arm64.tar.gz
haigosmart_X.Y.Z_darwin_amd64.tar.gz
haigosmart_X.Y.Z_darwin_arm64.tar.gz
haigosmart_X.Y.Z_windows_amd64.zip
haigosmart_X.Y.Z_windows_arm64.zip
checksums.txt
```

Names are stable across releases: only the version substring changes. A script that
constructs a URL from an operating system, an architecture, and a version keeps working.

## Archive contents

| File | Notes |
|---|---|
| `haigosmartd` (`haigosmartd.exe` on Windows) | The executable. No wrapper script, no installer |
| `README.md` | So an extracted archive is self-describing |
| `LICENSE` | GPL-3.0 requires the terms travel with the binary |

Flat layout — no top-level directory inside the archive. Extracting gives the binary in the
current directory.

## Checksums

`checksums.txt` holds one SHA-256 line per archive, in the format `sha256sum -c` accepts:

```text
<64 hex characters>  haigosmart_X.Y.Z_linux_amd64.tar.gz
```

Verification is expected to be:

```bash
sha256sum -c checksums.txt --ignore-missing
```

## Build guarantees

| Guarantee | How |
|---|---|
| Built from the release's commit | The job builds the tree it tagged; there is no other source |
| No host paths in the binary | `-trimpath` |
| No C toolchain dependency | `CGO_ENABLED=0` — verified for all six targets before the plan |
| Time zones work without a system database | `-tags timetzdata` |
| Reports its own version | `-ldflags -X main.version=X.Y.Z` |

## Version reporting

```console
$ haigosmartd -version
haigosmartd X.Y.Z
```

Requirements on this output:

- Exit status 0.
- Written to standard output.
- Answered **before** configuration is loaded, so an invalid or absent configuration cannot
  stop a binary from identifying itself. This matters in a container: "what version is this?"
  is the first question asked about a misbehaving deployment, and it must be answerable while
  it is misbehaving.
- A binary not built by the release pipeline reports `dev`. Never a fake version number — an
  unstamped build claiming `0.0.0` would be worse than one that admits what it is.

The version also appears as an attribute on the `starting` record, because for an unattended
server the log is where the question actually gets asked.

## Completeness

A release is complete only with all seven assets **and** a resolvable two-architecture image
manifest. Anything less stays a draft (FR-026). The rule exists because a release missing one
platform is not a smaller release — it is a broken one for whoever needed that platform, and
they find out at download time.
