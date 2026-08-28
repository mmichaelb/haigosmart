---

description: "Task list for feature 004: public release automation"
---

# Tasks: Public Release Automation

**Input**: Design documents from `/specs/004-public-release-automation/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: The constitution makes tests non-negotiable for new exported behaviour, so the one
piece of new Go behaviour — the `-version` flag — has a test task, written first. The release
pipeline deliberately has none: see [research.md §9](research.md) and the note before Phase 8.

**Organization**: Grouped by user story. Each story is independently verifiable, and the
verification for each is a scenario in [quickstart.md](quickstart.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story the task belongs to
- **[!]**: Cannot be undone or re-run — think before running

## Path Conventions

Single Go module at the repository root. New infrastructure lives at the root or under
`.github/`; the only Go files that change are import lines plus `cmd/haigosmartd/main.go`.

---

## Phase 1: Setup

**Purpose**: Establish the ground truth this feature is measured against, before anything
changes. Every task here is a measurement, not an edit — if one of them fails, the plan is
wrong and the rest of the feature is built on a false premise.

- [ ] T001 Verify the full git history carries no credential, capture, or private key (FR-005, SC-010): `git log --all --pretty=format: --name-only --diff-filter=A | sort -u | grep -Ei 'key|pem|crt|pcap|secret|token'` and inspect every hit. Record the result in this file's Status section
- [ ] T002 [P] Record the baseline: `go test ./... -race -count=1` green, `gofmt -l .` empty, `go vet ./...` clean. This is what "no test was modified" (SC-002) is compared against
- [ ] T003 [P] Confirm all six targets cross-compile today with `CGO_ENABLED=0` for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`. All six passed during planning; a failure here means something changed since

**Checkpoint**: the starting state is measured, so every later claim can be checked rather than asserted.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The module rename. This is User Story 1's FR-001…FR-003, but it sits here rather
than in the US1 phase for a practical reason: every subsequent phase adds or edits Go files,
and renaming after them means renaming twice, with the second pass touching files this feature
just wrote. Doing it first makes it one mechanical change verified by the compiler.

**⚠️ CRITICAL**: No other phase may begin until this one is complete and green.

- [ ] T004 Change the module line in `go.mod` to `module github.com/mmichaelb/haigosmart`
- [ ] T005 Rewrite every import of `haigosmart/internal/...` to `github.com/mmichaelb/haigosmart/internal/...` across `cmd/` and `internal/`, e.g. `grep -rl '"haigosmart/' --include='*.go' . | xargs sed -i '' 's|"haigosmart/|"github.com/mmichaelb/haigosmart/|g'`, then `gofmt -w` the touched files
- [ ] T006 Verify the rewrite is complete: `grep -rn '"haigosmart/' --include='*.go' .` returns nothing, and `go build ./... && go vet ./...` are clean. A missed reference cannot resolve to something unexpected — it fails to build, which is why this rewrite needs no test
- [ ] T007 Verify nothing else moved: `go test ./... -race -count=1` is green, and `git diff -- '*_test.go'` shows only import lines. SC-002 forbids editing a test to accommodate the rename — this is the check, and if a test needed changing, stop and find out why
- [ ] T008 Verify `git diff go.mod go.sum` shows only the module line — no dependency was added or bumped, which is the constitution's dependency constraint held to (plan.md, Additional Constraints)

**Checkpoint**: the module answers to its public name, the suite is green, and no test moved.

---

## Phase 3: User Story 1 - Someone finds the project and installs it (Priority: P1) 🎯 MVP

**Goal**: A stranger can identify the project, install it by its canonical name, and know what
they are permitted to do with it.

**Independent Test**: On a machine with no copy of the repository, install by the canonical
path and get a working binary; open the repository and find the licence.
[quickstart.md](quickstart.md) scenario 1.

- [ ] T009 [P] [US1] Add the full GPL-3.0 text to `LICENSE` at the repository root, unmodified from the canonical text (FR-004)
- [ ] T010 [P] [US1] Add the GPL-3.0 notice to `README.md` and state the canonical module path `github.com/mmichaelb/haigosmart` (FR-006)
- [ ] T011 [US1] Add `go install github.com/mmichaelb/haigosmart/cmd/haigosmartd@latest` to `README.md` as the primary install path (FR-002, SC-001)
- [ ] T012 [US1] Run quickstart scenario 1 end to end and record the result

**Checkpoint** — **G1 (part)**: the project is identifiable, licensed, and installable by name.

---

## Phase 4: User Story 2 - Every change is checked before it lands (Priority: P1)

**Goal**: Formatting, vetting, and race-enabled tests run on every proposed change, visibly,
and a failure blocks the merge.

**Independent Test**: Open a pull request with a misformatted file, watch it fail and block;
fix it, watch it pass. [quickstart.md](quickstart.md) scenario 5.

- [ ] T013 [US2] Extend `.github/workflows/ci.yml` to trigger explicitly on `pull_request` and on `push` to the main branch, replacing the bare `on: [push, pull_request]` (FR-007)
- [ ] T014 [US2] Make the format step name the offending files rather than only exiting non-zero, so a failure is actionable without reproducing it locally (FR-010)
- [ ] T015 [US2] Add a cross-compile job to `.github/workflows/ci.yml` building all six targets with `CGO_ENABLED=0`. This is the only check that catches a change working on the maintainer's macOS ARM machine while breaking the Windows or Linux artefact — and it catches it at pull-request time rather than mid-release (data-model.md, Check run)
- [ ] T016 [US2] Set explicit least-privilege `permissions: contents: read` on the CI workflow, so a fork's pull request reaches no credential (FR-009)
- [ ] T017 [P] [US2] Add `.github/dependabot.yml` covering `github-actions` and `gomod`, so the actions this feature depends on stay current without anyone remembering to look

**Checkpoint**: no unchecked change can merge. **G2** is provable once the repository is on GitHub.

---

## Phase 5: User Story 3 - A release happens without anyone cutting it (Priority: P1)

**Goal**: A releasable commit on the main line becomes a tagged version with notes, with no
human action — and a non-releasable one produces nothing at all.

**Independent Test**: `npx semantic-release --dry-run --no-ci` names the next version on a
history ending in `feat:`, and reports nothing to release on one ending in `docs:`.
[quickstart.md](quickstart.md) scenario 3. No GitHub needed for the dry run.

- [ ] T018 [US3] Add `.releaserc.json` with `commit-analyzer`, `release-notes-generator`, and `exec`, branch `main`. Do **not** add `@semantic-release/github` — GoReleaser owns the release, and two tools creating it is the conflict [research.md §1](research.md) exists to avoid
- [ ] T019 [US3] Set the `exec` plugin's `publishCmd` to `goreleaser release --clean`. This placement is the design: it runs only when a release was decided, so FR-014 needs no conditional anywhere (research.md §2)
- [ ] T020 [US3] Create `.github/workflows/release.yml` triggering on `push` to the main branch, with `concurrency: {group: release, cancel-in-progress: false}` and `permissions: {contents: write, packages: write}`
- [ ] T021 [US3] In `release.yml`, check out with `fetch-depth: 0` so the analyser can read history back to the previous tag. Without this it sees a shallow clone and decides wrongly
- [ ] T022 [US3] In `release.yml`, add the comment explaining why semantic-release and GoReleaser share one job: a tag pushed by the run's own token does not start another workflow, so splitting this into two workflows breaks releases **silently**. This comment is the only thing protecting the design from a future tidy-up (research.md §2)
- [ ] T023 [US3] Add `package.json` pinning the semantic-release version and its three plugins, so a release is not built against whatever npm resolves that day
- [ ] T024 [US3] Verify both directions with `npx semantic-release --dry-run --no-ci`: a `feat:` commit names the next minor; a `docs:`-only history reports nothing to release (FR-012, FR-014, SC-009)

**Checkpoint**: the version decision is correct in both directions. Nothing is published yet.

---

## Phase 6: User Story 4 - Downloading a binary for your machine (Priority: P2)

**Goal**: Every release carries six executables and a checksum file, each reporting its own
version.

**Independent Test**: `goreleaser release --snapshot --clean` produces the full set locally;
the extracted binary prints the snapshot version. [quickstart.md](quickstart.md) scenario 2.

### Tests for User Story 4

- [ ] T025 [P] [US4] Write `cmd/haigosmartd/version_test.go` asserting that `-version` prints the stamped version and exits zero, and that an unstamped build reports `dev` rather than a fabricated number. Confirm it fails before T026

### Implementation for User Story 4

- [ ] T026 [US4] Add `var version = "dev"` to `cmd/haigosmartd/main.go` and handle `-version` **before** `config.Load`, so a binary can identify itself while misconfigured — which is when the question is actually asked (contracts/release-artifacts.md)
- [ ] T027 [US4] Add the version as an attribute on the `starting` record in `cmd/haigosmartd/main.go`. For an unattended server the log is where "which build is this?" gets asked
- [ ] T028 [US4] Create `.goreleaser.yaml` with `version: 2`, a `builds` entry for `./cmd/haigosmartd`, `env: [CGO_ENABLED=0]`, goos `linux, darwin, windows`, goarch `amd64, arm64`
- [ ] T029 [US4] Add build flags `-trimpath`, `-tags timetzdata`, and `-ldflags "-s -w -X main.version={{.Version}}"`. The `timetzdata` tag is not optional polish: without it a container operator setting `TZ` gets UTC timestamps that look perfectly well-formed (research.md §5)
- [ ] T030 [US4] Configure `archives` as `tar.gz` with a `zip` override for Windows, including `README.md` and `LICENSE` in each — GPL-3.0 requires the terms travel with the binary (contracts/release-artifacts.md)
- [ ] T031 [US4] Configure `checksum` as a single `checksums.txt` in `sha256sum -c` format (FR-019)
- [ ] T032 [US4] Configure `release` in `.goreleaser.yaml` with `draft: true` and a conventional-commit `changelog`. The draft is how FR-026 is met; the changelog is FR-013
- [ ] T033 [US4] Verify with `goreleaser check` and `goreleaser release --snapshot --clean`: six archives, `checksums.txt`, each archive holding the binary plus `README.md` and `LICENSE`, the Windows archive holding `haigosmartd.exe`
- [ ] T034 [US4] Verify the stamped version reached the artefact: extract the linux/amd64 binary and run `-version`, expecting the snapshot version rather than `dev` (FR-020, FR-021)

**Checkpoint**: the full binary set builds locally and identifies itself.

---

## Phase 7: User Story 5 - Running it as a container on any machine (Priority: P2)

**Goal**: A two-architecture `scratch` image that behaves exactly like the unattended binary.

**Independent Test**: build locally, run with feature 003's environment variables, get feature
003's records. [quickstart.md](quickstart.md) scenario 4.

- [ ] T035 [P] [US5] Add `.dockerignore` excluding everything but the built binary — GoReleaser supplies it, so the build context needs nothing else
- [ ] T036 [US5] Create `Dockerfile`: `FROM scratch`, one `COPY` of the binary to `/haigosmartd`, `ENTRYPOINT ["/haigosmartd"]`, `USER 65534:65534`, `EXPOSE 1883`, `VOLUME /data`
- [ ] T037 [US5] Bake `ENV HAIGOSMART_HEADLESS=true` and `ENV HAIGOSMART_REGISTRY=/data/registry.json` into the `Dockerfile`. The second is not a convenience: the default path resolves through `os.UserConfigDir`, which fails in `scratch`, and the fallback is a relative path in an unwritable root — the server would warn on every save forever while otherwise working (research.md §5)
- [ ] T038 [US5] Add a `dockers_v2` block to `.goreleaser.yaml` with `images: [ghcr.io/mmichaelb/haigosmart]`, `platforms: [linux/amd64, linux/arm64]`, and tags for the version, major.minor, major, and `latest` (FR-022, FR-023)
- [ ] T039 [US5] Add OCI labels (`org.opencontainers.image.source`, `.licenses`, `.version`) so the image links back to the repository and declares GPL-3.0
- [ ] T040 [US5] In `.github/workflows/release.yml`, add QEMU and Buildx setup plus a GHCR login using the run's own token — no stored registry credential exists, and that is deliberate (FR-009, research.md §8)
- [ ] T041 [US5] Verify locally with a Docker daemon running: `goreleaser release --snapshot --clean` builds both architectures; `docker image inspect` reports **one** layer and user `65534`. More than one layer means something beyond the binary got in
- [ ] T042 [US5] Verify behaviour against feature 003 (FR-024): run the image with `HAIGOSMART_LAMPS` set, confirm JSON records with the same fields, confirm `docker stop` exits 0 within a second rather than waiting out the grace period
- [ ] T043 [US5] Verify the time zone fix: run with `TZ=Europe/Berlin` and confirm the record timestamps are Berlin local, not UTC. This is the check that fails silently if T029's build tag is ever dropped
- [ ] T044 [US5] Verify the no-volume case: run without mounting `/data` and confirm the server serves normally with one `saving the registry failed` warning — degraded, not broken, as [contracts/container-image.md](contracts/container-image.md) claims

**Checkpoint** — **G1**: everything provable without GitHub is proven. Scenarios 1–4 green.

---

## Phase 8: Publication (requires GitHub — the irreversible part)

**Purpose**: Everything above runs on one machine and can be re-run freely. Everything here
happens once. An immutable release with the wrong version is a permanent entry in the
project's history, so these tasks are deliberately separate and deliberately slow.

- [ ] T045 [!] Re-run T001's history check immediately before publishing, then create the public repository and push (FR-005, SC-010)
- [ ] T046 [!] Decide and create the first tag: `v0.1.0`, chosen so the project starts pre-1.0 rather than accepting semantic-release's `1.0.0` default, which would claim a stability commitment this project does not intend yet (research.md §7, FR-017)
- [ ] T047 [US2] Run quickstart scenario 5: open a pull request with a misformatted file, confirm it fails, blocks, and names the file; fix it and confirm it passes. **G2**
- [ ] T048 [US2] Enable branch protection on the main branch requiring the checks to pass, which is what turns a red result into an actual block (FR-008)
- [ ] T049 [!] [US3] Land a `feat:` commit and **watch the release job run**, rather than checking the outcome afterwards. The ordering of tag creation and GoReleaser invocation is the most likely thing to be wrong and it is visible in the log while it happens. Expect version `0.2.0`. **G3**
- [ ] T050 [US4] Verify the published release carries all seven assets and that `sha256sum -c checksums.txt --ignore-missing` passes on a downloaded archive (FR-018, FR-019, SC-006)
- [ ] T051 [US5] Verify `docker buildx imagetools inspect ghcr.io/mmichaelb/haigosmart:0.2.0` lists both architectures, and pull it without authenticating (FR-022)
- [ ] T052 Verify the release is not a draft and became visible only after the image was pushed (FR-026)
- [ ] T053 Verify immutability: re-run the release job for the published version, confirm it fails on the existing tag, and confirm the release assets and the image digest for that version are unchanged (FR-016, FR-025)
- [ ] T054 Verify the partial-failure path deliberately: point the image name at a repository the token cannot write, run a release, confirm it stays a **draft** and invisible on the releases page with a red run. Then revert. This is the one FR-026 behaviour that only appears under failure, so it has to be caused rather than waited for

---

## Phase 9: Documentation & Polish

- [ ] T055 [P] [US6] Rewrite `README.md`: what the project is, the canonical name, the three ways to get it (install, download, pull), and the licence (FR-027)
- [ ] T056 [P] [US6] Add `.github/CONTRIBUTING.md` covering the commit convention, what the checks enforce, and — the part people miss — that a squash merge means the **pull request title** is what the analyser reads (FR-028, contracts/commit-messages.md)
- [ ] T057 [P] [US6] Add `docs/releasing.md`: how the pipeline works, why the two tools share one job, how to intervene when a release fails, and the honest cost of `scratch` — there is no shell, so `docker exec` gives you nothing and diagnosis is the record stream
- [ ] T058 [US6] Update `docs/deploying.md` so its instructions match what is published: download and container paths alongside the locally built binary, and the container's `/data` volume (FR-029)
- [ ] T059 [P] [US6] Update `docs/homeassistant.md` and `docs/operating.md` for any changed install or run instructions
- [ ] T060 [US6] Run quickstart scenario 9: all three user paths, timed, using only the documentation. **G4** (SC-011)
- [ ] T061 Update the Status section below with what passed, what was found, and anything that behaved differently from the plan

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies — measurement only
- **Foundational (Phase 2)**: blocks everything. The rename must land before any phase that writes Go files
- **US1 (Phase 3)**, **US2 (Phase 4)**: independent of each other, both need Phase 2
- **US3 (Phase 5)**: needs Phase 2. Verifiable by dry run without US4 or US5
- **US4 (Phase 6)**: needs Phase 2. Its `publishCmd` wiring is US3's T019, so US3 before US4 in the file, though the goreleaser config can be built first
- **US5 (Phase 7)**: needs US4 — the image packages the binary US4 builds
- **Publication (Phase 8)**: needs US1 through US5. Irreversible
- **Documentation (Phase 9)**: needs Phase 8, because it documents what was actually published

### The one ordering that is not negotiable

T004–T008 before everything else. A rename applied after new Go files exist is a rename
applied twice.

### Parallel opportunities

- T002, T003 together
- T009, T010 together
- T017 alongside any other US2 task
- T035 alongside T036–T037
- T055, T056, T057, T059 together — four separate documents

---

## Parallel Example: Phase 9

```bash
Task: "Rewrite README.md with the canonical name and the three ways to get it"
Task: "Add .github/CONTRIBUTING.md with the commit convention"
Task: "Add docs/releasing.md explaining the pipeline"
Task: "Update docs/homeassistant.md and docs/operating.md"
```

---

## Implementation Strategy

### MVP

Phases 1–3. The project is public, licensed, and installable by its canonical name. That
alone is a deliverable: someone can use it. Nothing about releases exists yet.

### Incremental delivery

1. Phases 1–2 → the module answers to its public name, suite green
2. Phase 3 → **MVP**: installable and licensed
3. Phase 4 → changes are checked
4. Phase 5 → versions are decided automatically, publishing nothing yet
5. Phase 6 → binaries build locally
6. Phase 7 → images build locally. **G1**: everything provable offline is proven
7. Phase 8 → publish, once, watching
8. Phase 9 → document what exists

Stopping after any step leaves something coherent. Stopping mid-phase-8 does not, which is
why it is one phase rather than spread through the others.

### What has no test, and why

The release pipeline gets no unit test. A double for "GitHub published a release" would agree
with whatever I assumed about GitHub, and feature 003 shipped exactly two defects that were
invisible to that kind of double — the Home Assistant availability bug and the SIGPIPE exit,
both found within minutes of real hardware. So verification here is execution: `goreleaser
check`, a full snapshot build, and a real release watched live. T025 is the only test task
because `-version` is the only new Go behaviour.

---

## Notes

- `[!]` marks tasks that cannot be undone. T045, T046, and T049 are one-way doors
- T043 and T054 test failure modes that are otherwise silent — neither will surface on its own
- Commit with conventional messages from T018 onward; the pipeline reads them
- The verification tasks are not optional passes at the end of each phase. They are how this
  feature is tested at all

---

## Status

**61 tasks, 0 complete.** Generated 2026-08-28, not yet started.

Gates: **G1** (local, scenarios 1–4) at T044 · **G2** (checks enforced) at T047 ·
**G3** (first automated release, one-way) at T049 · **G4** (documentation) at T060.
