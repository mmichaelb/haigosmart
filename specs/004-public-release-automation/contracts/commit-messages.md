# Contract: Commit Messages

**Feature**: 004-public-release-automation

Commit messages are the input to the version number. This is the whole interface between
writing a change and shipping it, which is why it is a contract rather than a style note.

## Format

```text
<type>[optional scope][!]: <subject>

[optional body]

[optional footer]
```

## Types and their effect

| Type | Release | Meaning |
|---|---|---|
| `feat` | **minor** | New behaviour a user can see |
| `fix` | **patch** | A defect corrected |
| `perf` | **patch** | Faster or lighter, same behaviour |
| `docs` | none | Documentation only |
| `test` | none | Tests only |
| `refactor` | none | Internal change, no behaviour difference |
| `chore` | none | Dependencies, tooling, housekeeping |
| `ci` | none | Pipeline changes |
| `style` | none | Formatting only |
| `spec` | none | Spec Kit artefacts under `specs/` |

A `!` before the colon, or a `BREAKING CHANGE:` footer, forces a **major** bump regardless of
type.

## Examples

```text
feat(config): accept a lamp set from the environment
fix(server): report state on reconnect even when nothing changed
feat(hass)!: rename the availability topic

BREAKING CHANGE: existing Home Assistant entities must be rediscovered.
docs: describe the container path in deploying.md
spec: add feature 004 planning artefacts
```

## What happens to a message that does not match

Nothing bad, and nothing clever. The analyser ignores it: no version bump, no failed merge,
no guess.

This is deliberate. A pipeline that failed the merge would make an English-usage rule into a
build gate; one that guessed would eventually guess `major` on a typo fix. The cost of
ignoring is real and should be known before it bites: **a fix committed as
`fixed the timeout bug` does not ship.** It sits on the main line, released only when some
later conventional commit triggers a release — and then it appears in that release's notes
without having named itself.

If it matters, the recovery is an empty commit:

```bash
git commit --allow-empty -m "fix: report the state correctly on reconnect"
```

## Scope

Optional and free-form. Package names are the natural choice — `config`, `server`, `hass`,
`registry` — because they are what release notes read best against.

## Squash merges

The pull request title becomes the commit message on the main line, so **the pull request
title is what the analyser reads.** A conventionally-formatted commit inside a branch whose
pull request is titled `Update stuff` releases nothing. Worth checking before merging, and
called out in `CONTRIBUTING.md` for that reason.

## Why this convention

It is what the analyser reads without configuration, it is widely known, and — the property
that actually earns it a place here — it makes "no release" the default. FR-014 and SC-009
require that documentation and specification changes produce no version, and with this
convention that is the behaviour you get by writing nothing special.
