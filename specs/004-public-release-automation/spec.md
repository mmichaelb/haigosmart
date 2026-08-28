# Feature Specification: Public Release Automation

**Feature Branch**: `004-public-release-automation`

**Created**: 2026-08-28

**Status**: Draft

**Input**: User description: "I want to make the repository publicly available (github.com/mmichaelb/haigosmart) so the go module has to be adjusted. furthermore, the repository will live in github, so i need github actions integration. also i want to publish the go binaries on every release and multi platform docker images automatically. the releases should happen automatically"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Someone finds the project and installs it (Priority: P1)

A person who has never seen this project lands on the public repository. They can identify
it by its canonical name, install the tool with the standard Go toolchain in one command
without cloning, and be told plainly what they are allowed to do with the code.

**Why this priority**: The identity change is the prerequisite for everything else. Until
the project is addressable by its public name, nothing downstream — installation, module
resolution, documentation links — works for anyone outside this machine. It is also the
only part of this feature that changes existing source files, so it must land first.

**Independent Test**: From a clean machine with no copy of the repository, run the
toolchain's install command against the public name and get a working binary. Verifiable
on its own, with no release automation and no container images in existence.

**Acceptance Scenarios**:

1. **Given** the project has been made public under its canonical name, **When** someone
   installs it by that name with the standard Go toolchain, **Then** the tool builds and
   runs, and its help output is unchanged from the local build.
2. **Given** the project is public, **When** someone opens the repository, **Then** the
   full GPL-3.0 text is present at the root and the platform identifies the project as
   GPL-3.0 licensed.
3. **Given** the import path has changed, **When** the existing test suite is run,
   **Then** every test passes without modification to any test's assertions.
4. **Given** the repository is public, **When** its whole history is inspected, **Then**
   it contains no credential, no packet capture, and no private key.

---

### User Story 2 - Every change is checked before it lands (Priority: P1)

A contributor opens a pull request. Formatting, vetting, and the race-enabled test suite
run automatically, and the result is visible on the pull request before anyone reviews it.

**Why this priority**: The project constitution already requires this as a merge gate —
"CI (build, vet, lint, race-enabled tests) MUST be green before merge". A public
repository accepts contributions from people whose machines are unknown, so an automated
check is the only enforcement that actually holds.

**Independent Test**: Open a pull request containing a deliberately misformatted file and
confirm the check fails and blocks; correct it and confirm the check passes. No release
machinery need exist.

**Acceptance Scenarios**:

1. **Given** a pull request whose code is misformatted, **When** the automated checks run,
   **Then** they fail and name the offending file.
2. **Given** a pull request that introduces a data race, **When** the automated checks run,
   **Then** the race-enabled test run fails.
3. **Given** a passing pull request, **When** the checks complete, **Then** the result is
   reported on the pull request itself, not only in a log someone must go find.

---

### User Story 3 - A release happens without anyone cutting it (Priority: P1)

Changes accumulate on the main line. When a change worth releasing lands, a new version is
published on its own: version number chosen, notes written from the changes it contains,
and artefacts attached. Nobody decides a number, tags anything by hand, or writes notes.

**Why this priority**: This is the operational request. The user asked for releases to
happen automatically; a manual step anywhere in the chain reintroduces the thing being
removed. Without it, US4 and US5 have nothing to hang artefacts on.

**Independent Test**: Land a change on the main line and observe a release appear with a
version and notes, without any human action after the merge.

**Acceptance Scenarios**:

1. **Given** a change that affects users lands on the main line, **When** the automation
   runs, **Then** a release is published with a version number derived from the nature of
   the change, and notes listing the changes it contains.
2. **Given** a change that affects nobody outside the repository lands (a documentation
   typo, a specification edit), **When** the automation runs, **Then** no release is
   published — the version history does not fill with empty releases.
3. **Given** two releases in sequence, **When** their version numbers are compared,
   **Then** the second is strictly greater, and the increment reflects whether the change
   was breaking, additive, or a fix.
4. **Given** a release has been published, **When** its notes are read, **Then** they
   describe what changed in terms a user of the tool understands, without anyone having
   written them by hand.

---

### User Story 4 - Downloading a binary for your machine (Priority: P2)

Someone who wants to run the tool but does not have or want a Go toolchain opens the
latest release and downloads a single executable for their operating system and processor,
along with a way to confirm it arrived intact.

**Why this priority**: It widens the audience beyond Go developers, and it is the natural
companion to US3 — a release with no artefacts is a changelog. It depends on US3 existing,
so it follows it.

**Independent Test**: From the published release page, download the artefact for one
platform, verify it against the published checksums, run it, and confirm it reports the
released version.

**Acceptance Scenarios**:

1. **Given** a release exists, **When** its assets are listed, **Then** there is an
   executable for each supported operating system and processor combination.
2. **Given** a downloaded artefact, **When** it is checked against the published checksum
   list, **Then** it matches.
3. **Given** a downloaded executable, **When** it is asked for its version, **Then** it
   reports the version of the release it came from — not a placeholder or "dev".
4. **Given** a release, **When** its artefacts are produced, **Then** they were built from
   exactly the sources at that release's commit.

---

### User Story 5 - Running it as a container on any machine (Priority: P2)

An operator running the unattended server pulls a container image by version and runs it on
the hardware they have — a workstation, a server, or a small ARM board — with the same
command in each case.

**Why this priority**: The unattended mode delivered in feature 003 exists so the server
can run as a container; this is what makes that reachable. It is P2 rather than P1 because
the binaries (US4) already give a working unattended deployment path.

**Independent Test**: On a machine of one processor architecture, pull the published image
by its version tag, run it with the environment-variable configuration, and reach a
listening server. Repeat on a machine of a different architecture with the same command.

**Acceptance Scenarios**:

1. **Given** a release has been published, **When** an image is pulled from
   `ghcr.io/mmichaelb/haigosmart` by that version, **Then** it runs on each supported
   processor architecture without the puller naming the architecture, and without
   authenticating.
2. **Given** the image is run with the configuration environment variables, **When** it
   starts, **Then** it behaves exactly as the unattended server documented for feature 003,
   including the records it writes to standard output.
3. **Given** the image is running, **When** the container is asked to stop, **Then** it
   shuts down cleanly and reports the exit status documented for the unattended server.
4. **Given** a published image, **When** its tags are listed, **Then** the version that
   produced it is identifiable, and the most recent release is also reachable under a
   moving tag.

---

### User Story 6 - Knowing how to run and how to contribute (Priority: P3)

A newcomer reads the front page and learns what the project is, how to install or pull it,
and what is expected of a contribution. An operator finds the container instructions
alongside the existing unattended-deployment guide rather than in a separate place.

**Why this priority**: Documentation follows the capability it describes. Written earlier
it describes something that does not exist yet; the earlier stories are each independently
verifiable without it.

**Independent Test**: Someone who has not seen the project follows the front page to a
running instance — installed or pulled, their choice — without asking a question.

**Acceptance Scenarios**:

1. **Given** the public repository, **When** the front page is read, **Then** it names the
   canonical project address, the install command, and the container pull command.
2. **Given** a would-be contributor, **When** they look for the rules, **Then** they find
   what the automated checks enforce and how commit messages influence the version.
3. **Given** the existing deployment guide, **When** it is read after this feature,
   **Then** its container instructions match what is actually published.

---

### Edge Cases

- **A release fails part-way**: binaries publish but the images do not, or the reverse.
  What is visible to a user must never be a release that claims artefacts it does not
  have, or an image tagged with a version that has no release.
- **The same version is produced twice**: a re-run of the automation, or a repeated merge,
  must not overwrite an already-published release or silently replace a published image.
- **The main line is broken**: a change that fails the automated checks must not produce a
  release, so a broken artefact is never published.
- **A build succeeds for one platform and fails for another**: a partial artefact set must
  be detectable rather than presented as complete.
- **Nothing user-visible has landed since the last release**: the automation must be a
  no-op rather than producing an empty version bump.
- **A first release with no predecessor**: the automation has no previous version to
  increment from and must still produce a defensible starting version.
- **A contribution arrives from outside the project**: automated checks must run on it,
  but it must not be able to publish anything or reach any credential.
- **The commit message does not follow the expected form**: the automation must behave
  predictably — it must not guess a version increment and must not fail the merge itself.
- **The import path change is applied incompletely**: a stale reference to the old path
  must fail the build rather than resolve to something unexpected.

## Requirements *(mandatory)*

### Functional Requirements

**Project identity**

- **FR-001**: The module MUST be identified by the canonical public path
  `github.com/mmichaelb/haigosmart`, and every internal reference to the previous path MUST
  be updated in the same change.
- **FR-002**: The tool MUST remain installable and buildable by that canonical path with no
  further configuration on the installing machine.
- **FR-003**: Behaviour MUST be unchanged by the identity change: the existing test suite
  passes without any test being modified to accommodate it.
- **FR-004**: The repository MUST be licensed under GPL-3.0, with the full licence text
  present at the repository root and the licence identifiable by automated tooling, applied
  before it is made public.
- **FR-005**: The repository, including its full history, MUST NOT contain any credential,
  packet capture, private key, or other material that should not be public.
- **FR-006**: The front page MUST state what the project is and how to obtain it, by the
  canonical name.

**Automated checks**

- **FR-007**: Every proposed change and every change landing on the main line MUST be
  checked automatically for formatting, vetting, and a race-enabled run of the full test
  suite, and the result MUST be visible on the proposed change.
- **FR-008**: A failing check MUST prevent the change from being merged.
- **FR-009**: Checks MUST run on changes proposed from outside the project, and such runs
  MUST NOT have access to any credential or the ability to publish.
- **FR-010**: The check results MUST name what failed specifically enough to act on without
  reproducing the failure locally first.

**Automatic releasing**

- **FR-011**: A release MUST be produced without human action once a releasable change has
  landed on the main line.
- **FR-012**: The version number MUST be derived from the nature of the changes since the
  previous release, distinguishing breaking changes, additions, and fixes, and MUST
  increase strictly with each release.
- **FR-013**: Release notes MUST be generated from the changes the release contains.
- **FR-014**: When no releasable change has landed, the automation MUST publish nothing.
- **FR-015**: A release MUST NOT be produced from a state that failed the automated checks.
- **FR-016**: Re-running the automation for an already-published version MUST NOT overwrite
  or replace what was published.
- **FR-017**: The first release MUST be produced correctly with no previous version to
  build on.

**Published artefacts**

- **FR-018**: Every release MUST carry a standalone executable for each supported operating
  system and processor combination: Linux, macOS, and Windows, each on both 64-bit Intel
  and 64-bit ARM — six in total.
- **FR-019**: Every release MUST publish a checksum for each artefact so a download can be
  verified.
- **FR-020**: A released executable MUST report the version of the release it came from
  when asked.
- **FR-021**: Artefacts MUST be built from the exact sources of the commit the release was
  cut from.
- **FR-022**: Every release MUST publish a container image to the GitHub Container Registry,
  usable on both 64-bit Intel and 64-bit ARM, selected automatically for the machine pulling
  it. The image MUST be publicly pullable without authentication.
- **FR-023**: The image MUST be tagged with the release version, and the most recent
  release MUST also be reachable under a moving tag.
- **FR-024**: The containerised server MUST behave identically to the unattended server
  documented for feature 003 — same configuration variables, same records on standard
  output, same exit statuses, same shutdown on signal.
- **FR-025**: Published images MUST be immutable: an already-published version tag is never
  replaced.
- **FR-026**: A release whose artefacts did not all publish MUST be identifiable as
  incomplete rather than presented as a finished release.

**Documentation**

- **FR-027**: Documentation MUST cover installing by the canonical path, downloading a
  binary from a release, and pulling and running the container image.
- **FR-028**: Documentation MUST state what the automated checks enforce and how commit
  messages influence the version increment, so a contributor can satisfy both before
  submitting.
- **FR-029**: The existing deployment guide MUST be updated so its instructions match what
  is actually published, rather than describing a locally built binary only.

### Key Entities

- **Release**: A published, immutable point in the project's history. Has a version number,
  notes describing what it contains, and a set of artefacts. Produced automatically from
  the changes since the previous release.
- **Version**: An ordered identifier distinguishing breaking, additive, and corrective
  changes. Derived, never chosen by a person.
- **Binary artefact**: One standalone executable for one operating system and processor
  combination, accompanied by a checksum, attached to a release.
- **Container image**: A runnable packaging of the unattended server, addressable by version
  and usable on more than one processor architecture.
- **Check run**: The automated verification of a proposed or landed change — formatting,
  vetting, race-enabled tests — whose result gates merging and releasing.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Someone with a Go toolchain and no copy of the repository installs the tool
  with a single command by its canonical name, in under two minutes.
- **SC-002**: The existing test suite passes after the identity change with zero
  modifications to any test.
- **SC-003**: Every proposed change reports a check result within ten minutes of being
  opened, and no change with a failing result can be merged.
- **SC-004**: From a releasable change landing to its release being published, no human
  action is required — zero manual steps, measured over at least three consecutive
  releases.
- **SC-005**: Every published release carries a complete artefact set: an executable for
  each supported platform, a checksum for each, and a container image for each supported
  architecture. No release is published with a partial set undeclared.
- **SC-006**: Someone downloads a binary for their platform and has it running in under two
  minutes, having verified its checksum.
- **SC-007**: An operator pulls the image by version and reaches a listening unattended
  server in under five minutes, using only the documentation, on either supported
  architecture.
- **SC-008**: Version numbers across all releases are strictly increasing and each
  increment matches the nature of the changes it contains, verifiable by reading the notes.
- **SC-009**: Over a period containing at least one documentation-only change landing on the
  main line, zero empty releases are published.
- **SC-010**: The public repository's full history contains zero credentials, captures, or
  private keys, verified before it is made public.
- **SC-011**: A newcomer follows the front page to a running instance — installed, downloaded,
  or pulled — without asking a question.

## Assumptions

- **The repository does not exist on GitHub yet.** There is no remote configured, so this
  feature covers preparing the repository for publication and the automation that runs once
  it is there. Creating the repository and pushing it is the user's action.
- **There are no existing consumers of the old import path.** The module has never been
  published, so the path change needs no deprecation or compatibility shim — it is a
  rename, applied once.
- **The history is already clean.** Inspection shows no credential, capture, or key was
  ever committed; captures are ignored and protocol fixtures are scrubbed. FR-005 is
  therefore a verification step, not a history rewrite.
- **Commit messages carry the release intent.** Automatic version selection needs a signal;
  a structured commit convention is assumed as that signal, adopted as part of this feature
  and documented for contributors (FR-028).
- **Binaries and images are published from the same release event**, so a version always
  means the same sources in both forms.
- **The container image runs the unattended server only.** The terminal interface needs a
  terminal and adoption is a deliberate act at one; the image is the headless path from
  feature 003, and adoption stays a local step.
- **The platform set is Linux, macOS, and Windows on 64-bit Intel and ARM** for binaries
  (six combinations), and Linux on 64-bit Intel and ARM for images. Windows was added on
  2026-08-28 at the user's direction during planning; the earlier draft excluded it. All six
  targets were confirmed to compile with the C toolchain disabled before the plan was
  written, so no source change is needed to reach them.
- **Release notes are generated from commit messages**, not written by hand — a hand-written
  step would reintroduce the manual action this feature removes.
- **Feature 003's unattended behaviour is the container's contract.** The image is a
  packaging change, not a behaviour change; anything that differs is a defect in the image.
- **GPL-3.0 is a deliberate copyleft choice** (answered 2026-08-28). Anyone distributing a
  modified version must publish their source under the same terms. This constrains nothing
  about the project's own dependencies — they are permissively licensed — but it does mean
  the licence header convention and the `LICENSE` file are part of the published contract.
- **Images go to the GitHub Container Registry** (answered 2026-08-28), pulled from
  `ghcr.io/mmichaelb/haigosmart`. It needs no account and no stored credential: the
  automation publishes with the token the platform already grants it, which also satisfies
  FR-009 — an outside contribution cannot reach a publishing credential because there is no
  standing one to reach.
- **The existing check workflow is a starting point, not a rewrite.** A workflow already runs
  formatting, vetting, and race-enabled tests; this feature extends the automation around it
  rather than replacing it.
