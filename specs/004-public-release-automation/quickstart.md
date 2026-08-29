# Quickstart: Public Release Automation

**Feature**: 004-public-release-automation | **Date**: 2026-08-28

How to prove this feature works. Scenarios 1–4 run entirely on this machine. Scenarios 5–9
need the repository to exist on GitHub, and are therefore the part that can only be verified
once — publication is not repeatable.

Gate names (G1…) are referenced by `tasks.md`.

## Prerequisites

- Go 1.27, GoReleaser 2.18+, Docker with Buildx, Node 20+ (for scenarios 3 and 5+)
- For scenario 2 and beyond: a running Docker daemon
- For scenarios 5–9: the repository pushed to `github.com/mmichaelb/haigosmart`

---

## Scenario 1 — The rename is complete and changed nothing (FR-001…FR-003, SC-002)

```bash
head -1 go.mod                      # module github.com/mmichaelb/haigosmart
grep -rn '"haigosmart/' --include='*.go' . ; echo "exit $?"   # expect no matches
go build ./... && go vet ./... && go test ./... -race -count=1
git diff -- '*_test.go' | grep -v '^[-+].*haigosmart' | grep '^[-+]' ; echo "exit $?"
```

**Expect**: the module line names the canonical path; no stale import survives; the suite is
green; and the last command finds nothing — every line changed in a test file is an import
line. SC-002 says no test may be edited to accommodate the rename, and that command is how
the claim is checked rather than asserted.

Then confirm all six targets still compile:

```bash
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -o /dev/null ./cmd/haigosmartd \
    && echo "ok $t" || echo "FAIL $t"
done
```

**Expect**: six `ok` lines. All six passed before this plan was written, so a failure here is
a regression introduced by the feature, not a pre-existing limit.

## Scenario 2 — The full artefact set builds locally (FR-018…FR-022)

```bash
goreleaser check
goreleaser release --snapshot --clean
```

**Expect**: `dist/` holds six archives — four `.tar.gz`, two `.zip` — plus `checksums.txt`,
and two images are built. Verify the ones that matter:

```bash
ls dist/*.tar.gz dist/*.zip dist/checksums.txt
tar -xzf dist/haigosmart_*_linux_amd64.tar.gz -O haigosmartd > /dev/null && echo "archive ok"
unzip -l dist/haigosmart_*_windows_amd64.zip | grep -q haigosmartd.exe && echo "windows ok"
IMG=$(jq -r '.[] | select(.type=="Docker Image") | .name' dist/artifacts.json | head -1)
docker run --rm "$IMG" -version
```

**Expect**: each archive contains the binary plus `README.md` and `LICENSE`; the Windows
archive holds `haigosmartd.exe`; and the image prints a snapshot version rather than `dev` —
proving the ldflags reached the binary inside the image, not only the one in `dist/`.

Read the image tag from `dist/artifacts.json` rather than assuming it: snapshot tags are
templated from the snapshot version, and guessing the name is how this scenario ends up
testing an image from a previous run.

Then check the image is what was asked for:

```bash
docker image inspect "$IMG" \
  --format '{{ len .RootFS.Layers }} layers, {{ .Size }} bytes, user={{ .Config.User }}'
docker history "$IMG"
```

**Expect**: **two** layers, roughly 10.6 MB, user `65534`. The layers are the binary and an
empty `/data` owned by `65534` — `scratch` has no filesystem to inherit from, so without that
directory a mounted volume is created root-owned and the server cannot write it. A third layer
means something else got in.

## Scenario 3 — The version decision is correct in both directions (FR-012, FR-014, SC-009)

Without publishing anything:

```bash
npx semantic-release --dry-run --no-ci
```

**Expect**: on a history whose newest commits are `docs:` or `spec:` only, it reports that
there is nothing to release and exits without a version. Add a `feat:` commit and repeat:
it now names the next minor version.

Both directions matter. The first is SC-009 — documentation changes must not fill the version
history with empty releases — and it is the one an implementation is most likely to get wrong
by making every push release something.

## Scenario 4 — The container behaves exactly like the unattended binary (FR-024)

```bash
docker run --rm -p 18830:1883 -v /tmp/hgdata:/data \
  -e HAIGOSMART_LISTEN=:1883 \
  -e HAIGOSMART_LAMPS="a1b2c3d4=headlamp" \
  -e TZ=Europe/Berlin \
  "$IMG" > out.jsonl &
sleep 3
jq -e '.time and .since and .level and .msg' out.jsonl > /dev/null && echo "records ok"
head -1 out.jsonl | jq -r '.time'
docker stop $(docker ps -q --filter ancestor="$IMG")
echo "exit $?"
```

**Expect**: every line is a JSON record with feature 003's fields; the timestamp is Berlin
local time, **not** UTC — that is the `timetzdata` check, and it is the one that fails
silently if the build tag is dropped; `/tmp/hgdata` contains `registry.json` and `tls.key`;
and `docker stop` exits 0 within a second rather than waiting out the ten-second grace period.

Also confirm the failure mode with no volume:

```bash
docker run --rm -e HAIGOSMART_LAMPS="a1=x" "$IMG"
```

**Expect**: it serves normally, with one `saving the registry failed` warning. Degraded, not
broken — the contract in [contracts/container-image.md](contracts/container-image.md) says a
volume is recommended rather than required, and this is where that claim is tested.

**G1** — scenarios 1–4 green on this machine. Everything provable without GitHub is proven
here; what remains genuinely needs a published repository.

---

## Scenario 5 — Checks run on a pull request and block it (FR-007…FR-010, SC-003)

Open a pull request with a deliberately misformatted file.

**Expect**: the check reports failure on the pull request within ten minutes; the log names
the offending file rather than only exiting non-zero; merging is blocked. Fix the formatting
and the same pull request goes green.

Then open one from a fork and confirm the checks still run, with no secret available to it
and no release job triggered.

**G2** — checks are enforced.

## Scenario 6 — The first release (FR-017, research §7)

This is a decision, not an observation. Before the first automated run, tag `v0.1.0`
deliberately, so the project starts pre-1.0 rather than accepting semantic-release's `1.0.0`
default. Then land a `feat:` commit on the main line.

**Expect**: a release appears with no human action after the merge; its version is `0.2.0`;
its notes list the change; all seven assets are attached; and the image manifest resolves for
both architectures.

**G3** — the first automated release. Watch it end to end rather than checking the outcome
afterwards: the ordering of tag creation and GoReleaser invocation is the single most likely
thing to be wrong, and it is visible in the job log while it is happening.

## Scenario 7 — A published release is complete, and a broken one is invisible (FR-026)

```bash
gh release view v0.2.0 --json assets --jq '.assets[].name'
docker buildx imagetools inspect ghcr.io/mmichaelb/haigosmart:0.2.0
```

**Expect**: seven asset names; the manifest lists `linux/amd64` and `linux/arm64`.

Then verify the draft behaviour, which is the part that only shows up under failure. The
honest way to test it is to break the image push deliberately once — for example by pointing
the image name at a repository the token cannot write — and confirm the release stays a
draft, invisible on the releases page, with a red run.

## Scenario 8 — Immutability (FR-016, FR-025)

Re-run the release job for an already-published version.

**Expect**: it fails because the tag exists. Nothing is overwritten: the release keeps its
assets and the image digest for that version tag is unchanged.

```bash
docker buildx imagetools inspect ghcr.io/mmichaelb/haigosmart:0.2.0 --format '{{.Manifest.Digest}}'
```

Compare before and after. Identical, or the requirement is not met.

## Scenario 9 — A user's path, start to finish (SC-001, SC-006, SC-007, SC-011)

Three independent paths, each timed, none using knowledge that is not on the front page:

```bash
go install github.com/mmichaelb/haigosmart/cmd/haigosmartd@latest   # under 2 minutes
```

```bash
curl -LO https://github.com/mmichaelb/haigosmart/releases/latest/download/haigosmart_0.2.0_linux_amd64.tar.gz
curl -LO https://github.com/mmichaelb/haigosmart/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

```bash
docker run --rm -e HAIGOSMART_LAMPS="…" ghcr.io/mmichaelb/haigosmart:latest
```

**Expect**: each works from the documentation alone, on both architectures for the image.

**G4** — the documentation gate: someone who has not seen this project reaches a running
instance by whichever of the three paths they prefer, without asking a question (SC-011).

---

## What each gate is really for

| Gate | Proves | Can it be re-run? |
|---|---|---|
| G1 | The build, the artefacts, and the image are right | Yes, freely |
| G2 | No unchecked change can merge | Yes |
| G3 | Releases happen with no human action | **No** — the first release happens once |
| G4 | The documented paths are the real paths | Yes |

G3 is the one to slow down for. Everything before it is reversible on a local machine;
publishing a version is not, and an immutable release with the wrong version number is a
permanent entry in the project's history.
