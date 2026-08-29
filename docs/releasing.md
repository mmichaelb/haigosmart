# Releasing

Releases are automatic. A `feat:` or `fix:` commit landing on `main` produces a
published version — binaries for six platforms, a two-architecture container
image, and notes — with nobody choosing a number or pressing anything.

This document is for when that goes wrong, and for whoever inherits the pipeline.

## The shape

One workflow, one job:

```text
push to main
  → checks pass
  → semantic-release reads the commits since the last tag
      ├─ nothing releasable   → stop. Green job, nothing published
      └─ releasable
          → version computed, tag created
          → goreleaser release --clean
              → six binaries, archives, checksums
              → GitHub release created as a DRAFT
              → image built and pushed for amd64 and arm64
          → manifest verified
          → draft flag removed → the release is visible
```

`semantic-release` decides the version. GoReleaser does everything else,
including creating the GitHub release. `@semantic-release/github` is deliberately
not installed, so exactly one tool owns the release.

## Do not split this into two workflows

The obvious design — one workflow tags, another triggers on `push: tags` and
releases — **does not work, and fails silently.**

GitHub does not start a workflow from an event created with the run's own
`GITHUB_TOKEN`. It is a loop guard, and it means the tag-triggered workflow never
runs. No error is raised anywhere. The tag exists, the release does not, and
nobody notices until someone asks where the binaries went.

The ways around it are worse:

- A personal access token or App token would start the second workflow, and would
  mean a standing credential with write access sitting in the repository.
- A `workflow_dispatch` hand-off adds a gap where one half can succeed and the
  other fail.

So the two tools share a job. GoReleaser is invoked from semantic-release's
publish phase (`publishCmd` in `.releaserc.json`), which also means it runs only
when there was something to release — "publish nothing when nothing changed"
needs no condition anywhere.

If you take one thing from this document: **the single job is load-bearing.**

## Why a release can be a draft

GoReleaser creates the release as a draft and the workflow publishes it only
after confirming the image manifest resolves. A run that uploads binaries and
then fails to push images leaves an invisible draft and a red run, rather than a
release advertising an image that does not exist.

So: **a draft release means a failed run.** Look at the job log, fix the cause,
and re-run. Do not publish the draft by hand unless you have checked that its
assets and its image are both actually there.

## When something fails

| Symptom | Cause | What to do |
|---|---|---|
| No release, green job | No releasable commit — `docs:`, `chore:`, or an unconventional message | Expected. If a real fix is stuck behind a bad message, land an empty `fix:` commit |
| Version is `0.0.0-next` or similar | GoReleaser ran without the tag visible | The ordering broke. Check that semantic-release created the tag before `publishCmd`, and that checkout used `fetch-depth: 0` |
| Job fails on an existing tag | A re-run for a version already published | Correct behaviour. Published versions are immutable; do not delete and recreate one |
| Draft release, red job | Something after the binaries failed, usually the image push | Read the log, fix, re-run. The draft is replaced |
| arm64 image times out | Building under emulation is slow | Re-run. If it recurs, the arm64 image build needs a native runner |
| Release notes are empty | Every commit matched an excluded type | Expected — nothing user-facing shipped |
| `goreleaser: not found` | The install step was removed or reordered after the release step | GoReleaser is installed by `goreleaser/goreleaser-action` in `install-only` mode and must come before the step running semantic-release, which shells out to it |

## Versions

`v0.1.0` was tagged by hand to start the project pre-1.0. Everything after it is
computed. While the project is below 1.0, a breaking change (`!` or a
`BREAKING CHANGE:` footer) jumps straight to `1.0.0` — worth knowing before
writing one, since it declares the project stable as a side effect.

## The image has no shell

`ghcr.io/mmichaelb/haigosmart` is `scratch` plus one binary. There is no shell,
no package manager, no `/etc/passwd`, and no CA bundle. `docker exec` gives you
nothing.

That is a deliberate trade and it has a real cost, so it is worth stating what
replaces the shell: the JSON record stream on standard output. If a problem
cannot be diagnosed from the records, the fix is a better record, not a bigger
image.

It is not literally binary-only: the image also carries an empty `/data` owned by
`65534`. That is not decoration. `scratch` has no filesystem for a volume to
inherit ownership from, so without it a volume mounted at `/data` is created
root-owned, the server cannot write the registry, and it warns on every save while
otherwise appearing to work.

What makes `scratch` possible, in case a future change threatens it:

- The only outbound connection is a plaintext TCP dial to the MQTT broker, so no
  certificate store is needed. **Adding TLS to the broker connection would break
  this** and require a CA bundle in the image.
- The TLS material the lamps see is generated at runtime, and a key that cannot
  be written to disk is already non-fatal.
- Every target builds with `CGO_ENABLED=0`. A dependency requiring cgo would end
  the `scratch` image.

## Time zones

Release builds carry `-tags timetzdata`, embedding the zone database, because
`scratch` has none. Without it, `TZ=Europe/Berlin` on a container silently
produces UTC timestamps — well-formed and wrong by hours.

If the build tag is ever dropped, nothing fails. The check that catches it is
running the image with `TZ` set and reading a timestamp.

## Why semantic-release is installed rather than npx'd

semantic-release's documentation recommends running it with `npx` and advises against
installing it locally. This repository installs it anyway.

The reason is the tradeoff that documentation names: under `npx`, the dependency graph is not
pinned by a lockfile. The release job holds write access to this repository's contents and
packages, so an unpinned graph means roughly two hundred packages resolving fresh from the
registry on every release and running with those tokens — and a release that changes behaviour
with no commit here. The lockfile earned its place before the first release: it surfaced 42
high-severity advisories in a version that would otherwise have been used.

The documentation's advice targets JavaScript packages, where semantic-release publishes the
package it is a dependency of. Nothing here is published to npm, and `.releaserc.json` declares
an explicit `plugins` array, so `@semantic-release/npm` never loads.

If this ever becomes more trouble than it is worth, the documented form is a deletion — see
`specs/004-public-release-automation/research.md` §1.

## Pinned versions

Two things are pinned to exact versions rather than floating: GoReleaser, in
`release.yml`, and semantic-release with its plugins, in `package.json` with
`package-lock.json` fixing the whole graph. Both are the versions the pipeline was
actually verified against.

The cost is that they need updating deliberately — Renovate raises those pull
requests. The alternative is a release whose behaviour changes with no commit to
this repository, which is a poor trade for a job holding write tokens.

## Running the pipeline locally

Everything except publishing works on a laptop:

```bash
goreleaser check                       # is the configuration valid
goreleaser release --snapshot --clean  # build the whole set, publish nothing
npx semantic-release --dry-run --no-ci # what version would this produce
```

The snapshot build needs a running Docker daemon for the image half; without one,
add `--skip=docker` to check the binaries alone.
