# Implementation Plan: Public Release Automation

**Branch**: `004-public-release-automation` | **Date**: 2026-08-28 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/004-public-release-automation/spec.md`

## Summary

Rename the module to `github.com/mmichaelb/haigosmart`, licence the code under GPL-3.0, and
put a release pipeline behind the main line so that a releasable commit becomes a published
version with no human action.

The chain is deliberately short: **semantic-release decides the version and creates the tag;
GoReleaser builds and publishes everything else.** One workflow, one job, one release-owning
tool. The reason for one job rather than two workflows is not tidiness — a tag pushed with
the workflow's own token does not start another workflow, so the obvious two-workflow design
silently never releases. See [research.md §3](research.md).

Artefacts per release: six binaries (Linux, macOS, Windows × amd64, arm64) with a checksum
file, and a `scratch` container image for `linux/amd64` and `linux/arm64` published to
`ghcr.io/mmichaelb/haigosmart`. The image contains the binary and nothing else — verified
reachable because the server dials the broker in plaintext and generates its own TLS
material, so it needs no CA bundle and no shell.

The Go code changes very little: the import path, a `version` variable stamped at link time,
and a `-version` flag. All six targets already compile with `CGO_ENABLED=0`, confirmed before
this plan was written.

## Technical Context

**Language/Version**: Go 1.27 (pinned in `go.mod`), unchanged

**Primary Dependencies**: no new Go dependencies. `go.mod` gains nothing — the whole feature
lives in CI configuration, a Dockerfile, and a licence file. New *tooling*, none of it
imported by the program: GoReleaser 2.x (build and publish), semantic-release 24.x with
`commit-analyzer` and `release-notes-generator` (version decision), Docker Buildx and QEMU
(cross-architecture images). See [research.md §1](research.md) for the dependency
justification the constitution requires.

**Storage**: N/A for this feature. The container's registry file is a mounted path
(`/data/registry.json`), which is configuration, not new storage.

**Testing**: `go test ./... -race` unchanged, plus cross-compilation of all six targets as a
build-level check, plus `goreleaser check` for configuration validity. The release pipeline
itself is proven by dry runs (`goreleaser release --snapshot`) and then by the first real
release — CI configuration cannot be unit-tested, and pretending otherwise with a mock would
repeat the mistake feature 003 recorded twice.

**Target Platform**: binaries for `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`,
`windows/{amd64,arm64}`; images for `linux/{amd64,arm64}`

**Project Type**: single Go module producing one command, plus repository-level release
infrastructure

**Constraints**:

- The container image must be `scratch` — binary only, no base layer.
- No standing publishing credential may exist, so an outside pull request cannot reach one
  (FR-009). GHCR was chosen partly for this: it authenticates with the per-run token.
- An already-published version must never be replaced (FR-016, FR-025).
- A release that did not publish everything must not appear finished (FR-026).
- The containerised server's behaviour is feature 003's contract, unchanged (FR-024).

**Scale/Scope**: about ten new repository-root files, one Go source change of a few lines,
one mechanical import rewrite across the module, and three documentation updates. No package
gains or loses a responsibility.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

### I. Code Quality — PASS

The only Go change is a `version` variable, a `-version` flag, and the import path. `gofmt`
and `go vet` gates are unchanged and now run in CI on every pull request rather than only on
the main line (FR-007), which strengthens this principle rather than straining it. The
import rewrite is mechanical and verified by the compiler: a missed reference does not
resolve to something else, it fails to build.

### II. Testing Standards (NON-NEGOTIABLE) — PASS, with a stated boundary

Every existing test keeps passing unmodified (FR-003, SC-002) — the import rewrite must not
touch a single assertion. The new Go behaviour is one flag and one string, and it gets a
test: `-version` prints the stamped value and exits zero.

The boundary worth stating plainly: **the release pipeline has no unit test, and cannot have
an honest one.** A test double for "GitHub published a release" would agree with whatever I
assumed about GitHub. Feature 003 produced exactly two escaped defects and both came from
that pattern. So this feature's verification is execution, not simulation: `goreleaser check`
for validity, `goreleaser release --snapshot` for a full local build including images, and a
first real release observed end to end. These are the gates in [quickstart.md](quickstart.md),
not optional extras.

### III. User Experience Consistency — PASS

`-version` follows the existing flag conventions from `internal/config`. The container's
configuration surface is exactly feature 003's environment variables — no container-only
settings are invented (FR-024). The record format, exit codes, and shutdown behaviour are
unchanged, so `docs/deploying.md` describes one server whether it runs from a binary or an
image. Documentation updates (FR-027…FR-029) keep the published instructions matching what is
actually published.

The one user-facing break — the import path — is a rename of something never published, and
it is documented as such.

### Additional Constraints — PASS

`go.mod` gains no dependency and its Go version is untouched; the verification for that is
`git diff --exit-code go.mod go.sum` showing only the module line changed. The new tooling is
build-time only and justified in [research.md §1](research.md), which is what the
constitution asks for.

### Development Workflow — STRENGTHENED

This feature is what makes "CI must be green before merge" enforceable rather than aspirational:
until now the checks ran, but nothing depended on them because nothing else existed.

**Result: no violations. Complexity Tracking is empty.**

### Post-design re-check (after Phase 1)

Re-evaluated against the artefacts actually produced. Nothing moved:

- **Code Quality**: the design added no Go code beyond what was already counted — one
  variable, one flag, one test. The three contracts are documentation, not indirection.
- **Testing Standards**: the boundary stated above survived the design rather than widening.
  The verification gates in [quickstart.md](quickstart.md) got more specific, not more
  optimistic — G3 in particular is now marked as the one gate that cannot be re-run, which is
  a constraint the design has to respect rather than a note about it.
- **UX Consistency**: designing the container contract confirmed the settings surface stays
  single. The image bakes two existing variables and invents none, so `docs/deploying.md`
  keeps describing one server.
- **Dependencies**: still zero Go dependencies. `go.mod` changes by one line.

One design decision moved after research: the release is drafted and undrafted rather than
published directly, because that was the only mechanism found that satisfies FR-026 without
deleting a published release — and deleting one would violate FR-016.

## Project Structure

### Documentation (this feature)

```text
specs/004-public-release-automation/
├── plan.md              # This file
├── research.md          # Phase 0: the seven decisions and what was rejected
├── data-model.md        # Phase 0/1: what a release is made of
├── quickstart.md        # Phase 1: how to prove it works, gates G1-G4
├── contracts/
│   ├── release-artifacts.md   # What every release must carry, and how it is named
│   ├── container-image.md     # The image's runtime contract
│   └── commit-messages.md     # How a commit message becomes a version
└── tasks.md             # Phase 2 output (/speckit-tasks, not created here)
```

### Source Code (repository root)

```text
.github/
├── workflows/
│   ├── ci.yml                 # exists; extended to run on pull requests explicitly
│   └── release.yml            # new: the single release job
├── dependabot.yml             # new: keeps the actions themselves current
└── CONTRIBUTING.md            # new: commit convention and what CI enforces

.goreleaser.yaml               # new: builds, archives, checksums, image, release
.releaserc.json                # new: semantic-release plugins and branch
Dockerfile                     # new: FROM scratch, one COPY
LICENSE                        # new: GPL-3.0 full text
.dockerignore                  # new

cmd/haigosmartd/
├── main.go                    # changed: version variable, -version flag, import paths
└── version_test.go            # new

internal/**/*.go               # changed: import path only, mechanically
go.mod                         # changed: module line only

docs/
├── deploying.md               # changed: install, download, and container paths
└── releasing.md               # new: how the pipeline works and how to intervene
README.md                      # changed: canonical name, install, pull, licence
```

**Structure Decision**: The Go layout is untouched — this feature adds repository
infrastructure around an existing module rather than restructuring it. Every new file sits at
the repository root or under `.github/`, which is where the tools that read them look. The
only files inside `cmd/` and `internal/` that change are import lines, plus the version flag
in `main.go`.

## Complexity Tracking

> No constitution violations. This section is intentionally empty.

## Key design decisions

Each is argued in [research.md](research.md); this is the index.

| # | Decision | Short reason |
|---|---|---|
| 1 | semantic-release for the version, GoReleaser for everything else | The user asked for semantic-release and it is the only common tool that publishes *nothing* when nothing releasable landed (FR-014) |
| 2 | One workflow, one job | A tag pushed by the run's own token does not trigger another workflow — the two-workflow design fails silently |
| 3 | `dockers_v2` rather than `dockers` + `docker_manifests` | One block instead of three; confirmed present and accepted in GoReleaser OSS 2.18 |
| 4 | Draft the release, publish it only after the image manifest verifies | This is how FR-026 is met: a partial failure leaves a draft nobody sees, not a release missing half its assets |
| 5 | `scratch`, not `distroless` or `alpine` | The server dials the broker in plaintext and generates its own TLS key, so no CA bundle, no shell, no libc is reachable from any code path |
| 6 | `-tags timetzdata` on release builds | `scratch` has no zone database; without this an operator setting `TZ` silently gets UTC timestamps, which is a wrong answer rather than a missing one |
| 7 | `HAIGOSMART_REGISTRY=/data/registry.json` baked into the image | The default path resolves through the user config directory, which does not exist in `scratch`; the fallback is a relative path in an unwritable root |
| 8 | Release-please rejected | Its release step is a pull request a human merges, which is precisely the manual action FR-011 removes |

## What could still go wrong

Honest list, carried into [quickstart.md](quickstart.md) as things to look for rather than
assume:

- **The first release.** semantic-release with no previous tag defaults to `1.0.0`, which is a
  strong claim for a project whose hardware support is two lamp models. Gate G3 decides this
  deliberately rather than discovering it.
- **GoReleaser and semantic-release disagreeing about who owns the GitHub release.** The
  design gives it to GoReleaser alone (`@semantic-release/github` is not installed), and the
  snapshot run cannot prove that — only the first real release can.
- **arm64 image builds under QEMU.** They work but are slow; if the release job times out, it
  fails visibly rather than publishing half.
- **The import rewrite touching a test.** SC-002 requires zero test edits; the check is
  `git diff -- '*_test.go'` showing only import lines.
