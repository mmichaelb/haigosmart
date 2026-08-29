# Data Model: Public Release Automation

**Feature**: 004-public-release-automation | **Date**: 2026-08-28

This feature has no runtime data model — no new types, no persistence, no schema. What it
does have is a set of published objects with rules about their names, contents, and
lifecycles. Those rules are the model, and they are what the gates check.

---

## Release

A published, immutable point in the project's history.

| Field | Value | Source |
|---|---|---|
| Tag | `v<major>.<minor>.<patch>` | semantic-release, from commit messages |
| Notes | Grouped list of changes since the previous release | GoReleaser changelog |
| Assets | Six archives + one checksum file | GoReleaser |
| State | `draft` while publishing, then published | Workflow (see FR-026) |
| Commit | The main-line commit that triggered the run | Implicit |

**Rules**

- Strictly increasing across releases (FR-012). Guaranteed by semantic-release reading the
  previous tag; there is no path where a version is chosen by hand.
- Immutable once published (FR-016). A re-run for an existing version must fail rather than
  replace. The tag already existing is what makes it fail.
- Never created from a red check run (FR-015).
- Never created when no releasable commit landed (FR-014).
- Visible only when complete (FR-026): draft until the image manifest verifies.

**Lifecycle**

```text
commits land on main
  → checks pass
  → analyser reads messages since the last tag
      ├─ nothing releasable → stop, publish nothing        (FR-014)
      └─ releasable
          → version computed, tag created                  (FR-012)
          → binaries, archives, checksums built            (FR-018, FR-019, FR-021)
          → release created as DRAFT with the assets
          → image built and pushed for both architectures  (FR-022)
          → manifest verified for each architecture
          → draft flag removed → release is visible        (FR-026)
```

Every arrow after "releasable" happens in one job. A failure at any arrow leaves the draft
and a red run.

---

## Binary artefact

One standalone executable for one operating system and processor, inside one archive.

| Property | Value |
|---|---|
| Platforms | `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64` |
| Archive | `.tar.gz`, except Windows which is `.zip` |
| Contents | The executable, `README.md`, `LICENSE` |
| Binary name | `haigosmartd`, or `haigosmartd.exe` on Windows |
| Build | `CGO_ENABLED=0`, `-trimpath`, `-tags timetzdata`, `-ldflags "-s -w -X main.version=…"` |
| Checksum | One SHA-256 line per archive in a single `checksums.txt` |

**Rules**

- All six exist on every release, or the release stays a draft (FR-018, FR-026).
- Each is reproducible from the release's commit (FR-021) — `-trimpath` is what keeps build
  paths out of the binary so the same source gives the same output.
- Each reports its release version when run with `-version` (FR-020).
- `CGO_ENABLED=0` is not an optimisation here; it is what makes the `scratch` image possible
  and what makes cross-compilation work without a toolchain per target. All six were
  confirmed to compile this way before the plan was written.

---

## Container image

The unattended server, packaged.

| Property | Value |
|---|---|
| Repository | `ghcr.io/mmichaelb/haigosmart` |
| Base | `scratch` |
| Contents | The `linux` binary for the image's architecture, plus an empty `/data` owned by `65534`. Nothing else |
| Architectures | `linux/amd64`, `linux/arm64`, under one manifest list |
| Tags | `<version>`, `<major>.<minor>`, `<major>`, `latest` |
| Entrypoint | `/haigosmartd` |
| Baked environment | `HAIGOSMART_HEADLESS=true`, `HAIGOSMART_REGISTRY=/data/registry.json` |
| Volume | `/data` |
| Exposed port | `1883` |
| User | `65534:65534` |

**Rules**

- Pullable without authentication (FR-022).
- A published version tag is never replaced (FR-025). Moving tags — `latest`, `<major>` — do
  move, by definition; the immutability rule is about the exact version.
- Behaviour is feature 003's unattended server exactly: same variables, same JSON records on
  standard output, same exit codes, same signal handling (FR-024). The image is packaging,
  not a variant. Anything that differs is a defect in the image, not a documented difference.

**Why the baked variables are these two and no others**

`HAIGOSMART_HEADLESS=true` because the image has no terminal and the interactive mode needs
one. `HAIGOSMART_REGISTRY=/data/registry.json` because the default path resolves through the
user configuration directory, which does not exist in `scratch`; without it the server falls
back to a relative path in an unwritable root and warns on every save while otherwise working
— degraded in a way that takes a while to notice.

Everything else stays unset so the settings table in `docs/deploying.md` remains the single
description of the configuration surface.

---

## Version

An ordered identifier, derived and never chosen.

| Commit type | Effect |
|---|---|
| `feat:` | minor |
| `fix:`, `perf:` | patch |
| `!` after the type, or a `BREAKING CHANGE:` footer | major |
| `docs:`, `chore:`, `test:`, `refactor:`, `style:`, `ci:`, `spec:` | none |
| Anything unrecognised | none, and no failure |

**Rules**

- The first version was `1.0.0` — semantic-release's default with no previous tag. The plan
  called for `v0.1.0` and it was not tagged in time. It became permanent when a diagnostic
  request to `proxy.golang.org` caused the proxy to cache `v1.0.0`; see
  [research.md §7](research.md), which records that cause rather than glossing it.
- The unrecognised case must not fail the merge and must not guess. Silence is the correct
  behaviour and the cost — an unconventionally-worded fix does not ship until a later commit
  triggers a release — is documented for contributors rather than engineered around.

---

## Check run

The automated verification gating both merge and release.

| Property | Value |
|---|---|
| Triggers | Every pull request, every push to the main line |
| Steps | `gofmt` diff check, `go vet ./...`, `go test ./... -race`, cross-compile all six targets |
| Token | Read-only on pull requests; no repository secrets (FR-009) |
| Result | Reported on the pull request itself (FR-007) |

**Rules**

- A failure blocks merge (FR-008) and prevents a release (FR-015).
- Must name what failed precisely enough to act on without reproducing locally (FR-010) —
  which is why the format check prints the offending file list rather than only exiting
  non-zero.
- The cross-compile step is new and is not decoration: it is the only thing that catches a
  change compiling on the maintainer's macOS ARM machine while breaking the Windows or Linux
  artefact, and it catches it at pull-request time rather than at release time when a draft is
  already half-built.

---

## Relationships

```text
Commit ──analysed──> Version ──names──> Release
                                          ├── 6 × Binary artefact (+ checksums.txt)
                                          └── 1 × Container image (2 architectures)

Check run ──gates──> Merge
Check run ──gates──> Release
```

One release, one version, one commit. The binaries and the image are built in the same job
from the same tree, so a version identifies the same source in both forms — which is the
property that makes "run the image, or download the binary" an operator's free choice rather
than a subtle difference.
