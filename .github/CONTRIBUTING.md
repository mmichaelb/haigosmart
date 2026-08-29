# Contributing

## What the checks enforce

Every pull request runs, and all of it must pass before merge:

| Check | Command | Why it blocks |
|---|---|---|
| Formatting | `gofmt -l .` must be empty | The check prints the offending files, so you never have to reproduce it locally to find out which |
| Vetting | `go vet ./...` | |
| Tests | `go test ./... -race` | Race detection is not optional — this server is concurrent by nature |
| Cross-compilation | six targets, `CGO_ENABLED=0` | Catches a change that builds on your machine and breaks a released artefact. Cheap here; expensive at release time, when it leaves a half-built draft |

Locally, `make check` covers the first three.

A pull request from a fork runs the same checks with a read-only token and no
access to any secret. Nothing a fork can do reaches a publishing path.

## Commit messages decide the version

Releases are automatic. Nobody picks a version number — it is computed from the
commit messages since the last release, so **the message is the release
mechanism**, not a formality.

```text
<type>[optional scope][!]: <subject>
```

| Type | Effect |
|---|---|
| `feat` | minor release |
| `fix`, `perf` | patch release |
| `docs`, `test`, `chore`, `ci`, `style`, `refactor`, `spec` | no release |
| any of the above with `!`, or a `BREAKING CHANGE:` footer | major release |

Examples:

```text
feat(config): accept a lamp set from the environment
fix(server): report state on reconnect even when nothing changed
docs: describe the container path in deploying.md
```

### Two things that surprise people

**A squash merge uses the pull request title.** That title is what the analyser
reads, so a perfectly-formatted commit inside a branch whose pull request is
called `Update stuff` releases nothing. Check the title before merging.

**An unrecognised message is ignored, not rejected.** Nothing fails, nothing is
guessed — but the change does not ship. A fix committed as `fixed the timeout
bug` sits on the main line until some later conventional commit triggers a
release, and then appears in that release's notes without having named itself.
If it matters:

```bash
git commit --allow-empty -m "fix: report the state correctly on reconnect"
```

**The project is past 1.0**, so a breaking change costs a major version. Write `!`
or a `BREAKING CHANGE:` footer only when a user-visible contract really changes —
a renamed Home Assistant topic, a changed record field, a removed setting.

## Code

The project constitution is in [`.specify/memory/constitution.md`](../.specify/memory/constitution.md).
The parts that come up most:

- Every exported function, type, and package carries a doc comment.
- New behaviour ships with table-driven tests. A bug fix ships with a regression
  test that **fails without the fix** — confirmed, not assumed.
- Errors are handled or wrapped with context. A silent `_ = err` needs a comment
  saying why.
- A new third-party dependency needs a line in the pull request saying what the
  standard library or an existing dependency could not do.

## Features are specified before they are built

Larger work goes through [Spec Kit](https://github.com/github/spec-kit): a spec,
a plan, then tasks, all under `specs/`. Reading an existing feature there is the
fastest way to understand why something is the way it is — the reasoning is
written down, including what was rejected.
