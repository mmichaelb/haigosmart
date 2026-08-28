# Research: Public Release Automation

**Feature**: 004-public-release-automation | **Date**: 2026-08-28

Everything below was decided before implementation. Where a claim could be checked on this
machine, it was checked, and the check is recorded — a decision resting on what a tool is
assumed to do is exactly the failure mode feature 003 hit twice.

---

## 1. The release tool chain

**Decision**: semantic-release decides the version and creates the tag. GoReleaser builds,
packages, and publishes everything — binaries, checksums, image, and the GitHub release
itself. `@semantic-release/github` is **not** installed, so exactly one tool owns the release.

**Rationale**:

The two jobs are genuinely different, and no single tool does both well:

- *Deciding the version* means reading commit messages since the last tag and concluding
  "this is a minor bump" or, importantly, "nothing here is releasable". GoReleaser cannot do
  this; it starts from a tag that already exists.
- *Building and publishing* means six cross-compiled binaries, archives, a checksum file, a
  two-architecture image, and a release with all of it attached. semantic-release can only
  shell out for this.

FR-014 — publish nothing when nothing releasable landed — is the requirement that forces a
commit-analysing tool into the design. Without it, any tag-on-push scheme would do.

The user asked for semantic-release, and it does the job. Its cost is honest and worth
stating: it drags a Node runtime and roughly two hundred npm packages into the release job of
a Go repository. That cost is confined to CI. It touches `go.mod` not at all, it is absent
from every artefact, and nobody installing or running haigosmart ever sees it.

**Alternatives considered**:

| Option | Why not |
|---|---|
| **release-please** (Google) | Its release step is a pull request that a human merges. That is precisely the manual action FR-011 exists to remove. Auto-merging it is possible but means bolting an approval bypass onto a tool whose design centre is the approval. Rejected on the requirement, not on quality — it is otherwise the better fit for a Go repo, needing no Node |
| **go-semantic-release** | A single Go binary doing what semantic-release does, with no Node. Genuinely the leaner choice and it was seriously considered. Rejected because the user named semantic-release, the ecosystem around it is far better documented, and the Node cost lands only in CI. **Revisit trigger**: if the release job's dependency installation becomes slow or a supply-chain concern, this is a drop-in swap — the workflow shape does not change |
| **GoReleaser alone, on manually pushed tags** | Simplest by far, and rejected only because "the releases should happen automatically" is the feature. A human choosing a version number is the thing being removed |
| **A hand-written version script** | Reading conventional commits and computing a semver bump is not hard, but it is a parser of a specification someone else maintains. Writing one to avoid a dependency that lives only in CI is the wrong trade |

### Deviation: installed locally, not run through npx

semantic-release's own documentation recommends the opposite of what this repository does:

> We recommend running **semantic-release** directly in the CI environment with npx […] we
> recommend against installing it as a local dependency of your project.

This project installs it — `package.json`, `package-lock.json`, `npm ci` — and the reason is
the tradeoff the same documentation names: *"the full semantic-release dependency graph is not
pinned by your project's lockfile"* when run through npx.

That matters here more than it would in most repositories, because the release job holds
`contents: write` and `packages: write`. Under npx, roughly 218 packages resolve fresh from the
registry on every release, executing with those tokens, and a release can change behaviour or
break with no commit to this repository — the same silent-failure class this feature is built
to avoid everywhere else. The lockfile also earned its place immediately: it is how the 42
high-severity advisories in the initially pinned version were found, before a release rather
than after one.

The documentation's advice is aimed at JavaScript packages, where semantic-release publishes
the very package it is a dependency of. That self-reference does not exist in a Go repository:
nothing here is published to npm, and `.releaserc.json` declares an explicit `plugins` array,
so the default `@semantic-release/npm` plugin never loads.

**The costs, stated rather than buried**: three files in a Go repository that are not Go, a
language breakdown on GitHub that will show JavaScript, and one more Dependabot ecosystem to
review.

**Revisit trigger**: if the npm dependency graph becomes a maintenance burden, the documented
form is a small deletion — remove `package.json`, `package-lock.json`, the npm Dependabot
entry, and the `node_modules` ignore line, then run
`npx --package semantic-release@25 --package @semantic-release/exec@7 semantic-release`. Direct
versions stay pinned; only the transitive graph floats.

**Constitution note**: the additional-constraints clause requires justifying new third-party
dependencies. None of these are Go dependencies — `go.mod` and `go.sum` are unchanged apart
from the module line, and that is verified by `git diff` rather than asserted.

---

## 2. Why one workflow and one job

**Decision**: a single `release.yml` with a single job that runs semantic-release and then
GoReleaser in sequence, guarded by a `concurrency` group so two releases cannot interleave.

**Rationale**:

The design everyone reaches for first is two workflows: one that tags, and one triggered by
`on: push: tags` that releases. **It does not work, and it fails silently.** GitHub does not
start a workflow from an event created by the default `GITHUB_TOKEN` — a deliberate loop
guard. So the tag appears, no release job runs, and nothing reports an error. Nobody notices
until someone asks where the binaries are.

The three ways out:

1. Push the tag with a personal access token or a GitHub App token. Works, and creates a
   standing credential with write access — directly against FR-009 and the reason GHCR was
   chosen in the first place.
2. `workflow_dispatch` the second workflow explicitly. Works, and adds an orchestration step
   that can partially fail between two runs, making FR-026 harder.
3. **Do both in one job.** No cross-workflow trigger, no extra credential, no gap. Chosen.

The sequencing inside the job matters. semantic-release creates the tag locally *before* its
publish plugins run, so by the time GoReleaser is invoked, `git describe` resolves and
GoReleaser builds the right version. That ordering is the whole reason the single-job design
works, and it is the first thing to check if a release ever produces `0.0.0-next`.

**Concurrency**: `group: release`, `cancel-in-progress: false`. Cancelling a release halfway
is worse than queueing it — an interrupted publish is exactly the partial state FR-026 is
about.

### The shape

GoReleaser is invoked from semantic-release's publish phase, not as a later workflow step:

```json
"plugins": [
  "@semantic-release/commit-analyzer",
  "@semantic-release/release-notes-generator",
  ["@semantic-release/exec", { "publishCmd": "goreleaser release --clean" }]
]
```

Two properties fall out of that placement, and both are requirements met for free:

- **FR-014 needs no logic.** When nothing releasable landed, semantic-release never reaches
  its publish phase, so `goreleaser` is never invoked and nothing is published. A green job
  that did nothing is the correct outcome, not a missed one. Any design where GoReleaser is a
  separate step needs a conditional carrying "was there a release?" across the gap — one more
  thing to get wrong, in the direction of publishing when it should not.
- **The tag exists by then.** semantic-release creates the tag before publish plugins run, so
  `git describe` resolves and GoReleaser builds the right version. If a release ever produces
  `0.0.0-next`, this ordering is what broke.

The workflow around it is unremarkable: checkout with `fetch-depth: 0` so the analyser can see
history back to the last tag, Go, QEMU and Buildx for the arm64 image, a GHCR login using the
run's own token, then `npx semantic-release`. Permissions are `contents: write` and
`packages: write` and nothing else.

### The residual risk is legibility, not mechanism

Nothing about this can fail at runtime — there is no event crossing a boundary to be dropped.
The risk is that a single job doing two things looks untidy, someone later splits it into the
"obvious" two workflows, and the repository silently stops releasing. The trap is invisible
in the split version: it looks more correct, and it is broken.

So the protection is a comment in `release.yml` stating why the two steps share a job, the
same explanation in `docs/releasing.md`, and the FR-026 verification step — which asserts that
the tag just created has a complete, published release, so a release that half-happened fails
red rather than quietly.

---

## 3. Making a partial publish invisible (FR-026)

**Decision**: GoReleaser creates the GitHub release as a **draft**. A later step in the same
job verifies the image manifest resolves for both architectures, and only then removes the
draft flag.

**Rationale**:

GoReleaser's publish stage pushes the image and creates the release. Either can fail after
the other succeeded — a registry timeout after the binaries uploaded leaves a release that
looks complete and has no image. FR-026 says such a release must be identifiable as
incomplete rather than presented as finished.

A draft is exactly that, and it costs one configuration line plus one verification step:

- Everything succeeds: the draft is published, and users see one complete release.
- Anything fails: the job is red and a draft sits there — invisible to anyone browsing
  releases, and inspectable by whoever fixes it.

This also handles a subtler case. Undrafting last means the release becomes visible only
after the image is confirmed pullable, so the window where a user could read release notes
naming an image that does not exist yet is closed rather than merely short.

**Alternatives considered**: publishing directly and relying on the red run to signal trouble
was rejected because a red run in a repository's history is far less visible than a broken
release on its front page. Deleting a failed release was rejected because it fights FR-016 —
recreating the same version is exactly the overwrite that requirement forbids.

---

## 4. GoReleaser image configuration: `dockers_v2`

**Decision**: use the `dockers_v2` block with `platforms: [linux/amd64, linux/arm64]`.

**Verified on this machine, not assumed**: GoReleaser 2.18.0 is installed; its emitted JSON
schema contains `dockers_v2` under the project definition with `platforms`, `images`, `tags`,
`dockerfile`, `labels`, and `build_args`. A candidate configuration using it passed
`goreleaser check` — the only complaint on the first run was `no remote configured to list
refs from`, which is true of this repository today and unrelated to the block itself.

**Rationale**: the older approach needs three coordinated pieces — one `dockers` entry per
architecture, each passing `--platform` by hand, plus a `docker_manifests` entry stitching
them together with templated tag references. `dockers_v2` expresses the same thing once and
drives Buildx directly. Three places to keep in sync becomes one.

**Alternatives considered**: the classic `dockers` + `docker_manifests` pair is the older,
more widely documented path and remains the fallback if `dockers_v2` behaves unexpectedly
under CI. Building images in a separate `docker/build-push-action` step was rejected because
it would rebuild the binary that GoReleaser already produced, and two builds of "the same"
binary is one build too many for FR-021.

---

## 5. `scratch`, and what it costs

**Decision**: `FROM scratch` with a single `COPY` of the binary. No base image.

**Verified reachable**, rather than hoped for:

- **No CA bundle needed.** The only outbound connection is to the MQTT broker, and
  `internal/mqtt/client.go:135` dials it with `net.DialTimeout("tcp", ...)` — plaintext, no
  TLS. There is no other outbound dial in the module.
- **No certificate files needed.** The server's TLS material for the lamps is generated at
  runtime (`internal/server/tls.go`), self-signed, and — this is the part that matters for a
  read-only container — a key that cannot be written to disk is explicitly non-fatal: the run
  continues with a fresh certificate. The lamps do not verify it.
- **No libc.** All six targets build with `CGO_ENABLED=0`, confirmed by cross-compiling each
  before this plan was written.
- **Size**: the `linux/amd64` binary with `-s -w` is 7.4 MB, which is the whole image.

**Two consequences that must be handled, not discovered:**

- **Time zones.** `scratch` has no zone database, so `time.Local` falls back to UTC. Feature
  003's records are formatted in local time. An operator who sets `TZ=Europe/Berlin` on the
  container would silently get UTC timestamps — a wrong answer, not a missing one, and the
  kind that costs an hour during an incident. Fixed by building with `-tags timetzdata`,
  which embeds the database in the binary (≈450 KB). No source change: it is a build tag, so
  `go build` locally is unaffected.
- **The default registry path.** `config.DefaultRegistryPath` resolves through
  `os.UserConfigDir`, which needs `HOME` or `XDG_CONFIG_HOME`. Neither exists in `scratch`.
  The code does not crash — `registry.DefaultPath` returns an error and the fallback is the
  relative path `registry.json`, which in a container means the root directory, unwritable.
  The failure would be a `saving the registry failed` warning every save, forever, with the
  lamps still working. Degraded and confusing rather than broken. Fixed by baking
  `HAIGOSMART_REGISTRY=/data/registry.json` into the image and declaring `/data` a volume.

**Alternatives considered**: `distroless/static` adds CA certificates, `/etc/passwd`, and a
tzdata-free non-root user for a few megabytes; every one of those is a thing this program does
not use. `alpine` adds a shell, which is convenient for debugging and is also a shell in a
production image. The user asked for lean and the code supports it, so `scratch` it is. The
honest cost: no `docker exec` debugging. `docs/releasing.md` will say so rather than let
someone find out during an outage.

---

## 6. Version stamping

**Decision**: a package-level `version` variable in `main`, defaulting to `dev`, set at link
time by GoReleaser's `-X main.version={{.Version}}`. A `-version` flag prints it and exits
zero, handled before configuration is loaded.

**Rationale**: FR-020 requires a released binary to report its own release version. Handling
the flag before `config.Load` matters — asking a binary what it is must not require a valid
configuration, or a misconfigured container cannot answer the first diagnostic question
anyone asks.

The version also goes into the `starting` record. It costs one attribute and answers "which
build is this?" from the log alone, which is the form the question actually arrives in for an
unattended server.

**Alternatives considered**: `runtime/debug.ReadBuildInfo` reports the module version for
`go install`-built binaries and reads `(devel)` for locally built ones; it does not cover
GoReleaser's cross-compiled artefacts without the same ldflags anyway. Making `version` a
`config` setting was rejected — it is a property of the build, not something an operator sets,
and putting it in the settings table would invite someone to override it.

---

## 7. Conventional commits, and the first version

**Decision**: adopt Conventional Commits as the version signal. `feat:` yields a minor bump,
`fix:` a patch, a `!` marker or a `BREAKING CHANGE:` footer a major. Anything else — `docs:`,
`chore:`, `test:`, `refactor:`, `spec:` — releases nothing.

**Rationale**: automatic versioning needs a signal in the history, and this is the convention
semantic-release reads by default. Its useful property here is the silence: FR-014 and SC-009
require that a documentation-only change produces no release, and with this convention that
falls out of the default configuration rather than needing rules.

**Malformed messages**: a commit whose subject matches no type is ignored by the analyser. It
does not fail the merge and does not guess — which is the behaviour the spec's edge case asks
for. The practical consequence is worth documenting for contributors: a fix landed as
`fixed the thing` rather than `fix: the thing` will not ship until some later commit triggers
a release.

**The first release** is a real decision, not a default to stumble into. With no previous tag,
semantic-release produces `1.0.0`. For a project supporting two lamp models on hardware the
maintainer owns, `1.0.0` claims a stability commitment that is not intended yet. Starting at
`0.1.0` instead — by tagging `v0.1.0` before the first automated run — keeps semver's own
rule that pre-1.0 makes no compatibility promise, and lets `feat:` commits bump the minor
without implying a breaking-change budget. This is gate **G3** in
[quickstart.md](quickstart.md), decided deliberately at the moment of publication.

---

## 8. Checks on outside contributions (FR-009)

**Decision**: `ci.yml` runs on `pull_request` with the default read-only token and no secrets.
`release.yml` runs only on `push` to the main line.

**Rationale**: a pull request from a fork gets a read-only `GITHUB_TOKEN` and no access to
repository secrets, which is exactly the isolation FR-009 asks for — and it is the platform's
default, so it holds without configuration to maintain. The release job never runs on a pull
request, so no fork can reach a publishing path.

This is also where the GHCR choice pays a second time: because publishing authenticates with
the per-run token, there is **no standing registry credential in the repository at all**. A
credential that does not exist cannot leak. Docker Hub would have required storing one.

---

## 9. What has no test, and why that is stated rather than hidden

The release pipeline cannot be unit-tested honestly. A double for "GitHub created a release"
or "the registry accepted a manifest" would confirm my assumptions about GitHub and the
registry, and feature 003 shipped two defects that were invisible to exactly that kind of
double — the Home Assistant availability bug and the SIGPIPE exit.

So verification here is execution:

- `goreleaser check` — the configuration is valid.
- `goreleaser release --snapshot --clean` — all six binaries, the archives, the checksums, and
  both image architectures are actually produced locally. This needs a running Docker daemon
  (the daemon was not running when this research was written; the gate records it).
- A real release, watched end to end, with the resulting binary and image run.

These are gates G1–G4 in [quickstart.md](quickstart.md). The Go-side change gets an ordinary
unit test, because it is ordinary Go: `-version` prints the stamped string and exits zero.
