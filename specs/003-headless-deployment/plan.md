# Implementation Plan: Headless Deployment

**Branch**: `003-headless-deployment` | **Date**: 2026-08-28 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/003-headless-deployment/spec.md`

## Summary

Turn the existing `-headless` stub into a deployable mode: configuration from the
environment, JSON-lines records on standard output, and a lamp set fixed by
configuration rather than by whoever connects.

Four pieces of work, in dependency order:

1. **`internal/config`** — one table of settings with a default, an environment name,
   and a flag. Defaults → environment → flags, last writer wins. Validation before
   anything opens a socket. Credentials redacted by construction.
2. **`internal/logging`** — the record format: JSON lines to standard output in headless
   mode, compact timestamp, elapsed-since-start on every record, and a write failure
   that stops the process instead of running deaf.
3. **Admission control in `internal/server`** — an optional predicate consulted after a
   lamp identifies itself and before it is accepted. Nil in interactive mode (today's
   behaviour, unchanged); the configured lamp set in headless mode.
4. **`docs/deploying.md`** — the ordered path from a lamp on the vendor's cloud to an
   unattended instance, and the settings table.

**On the dependency question.** The spec asks for a configuration system "like
spf13/viper". I did not take it, and the reasoning is in [research.md §1](./research.md):
with configuration coming from the environment only (Q1), viper's value — file formats,
live reload, remote key-value backends, precedence merging across sources — is entirely
unused, while its cost is roughly fifteen modules for a job the standard library's `flag`
package already half does. The chosen design is ~120 lines in one file with no new
`go.mod` entry. **If you want viper regardless, it is a single-file swap**: `internal/config`
exposes `Load(args, env) (Config, error)` and nothing else, so what fills the struct is
invisible to the rest of the program. Say the word and it changes.

**Time format.** Every record carries `"time":"2026-08-28 14:03:12.123"` and
`"since":"1m12.345s"` — the compact date-and-time plus the difference the spec asked for,
measured from process start. Rationale and the rejected alternatives are in
[research.md §3](./research.md).

## Technical Context

**Language/Version**: Go 1.27, unchanged.

**Primary Dependencies**: **None added.** `go.mod` gains no line. Configuration is
`flag` + `os.Getenv`; records are `log/slog`'s JSON handler with one small wrapper.
See research.md §1 and §3.

**Storage**: The same `registry.json`, demoted to a cache. Under Q2 the configured lamp
set is authoritative: an instance whose registry file is lost or read-only starts up
serving exactly the same lamps and relearns their state from the lamps themselves.

**Testing**: `go test ./... -race`, table-driven. Configuration and record formatting are
pure functions of their inputs — an environment map in, a `Config` out; a record in, a
line out — so they test without a socket. Admission control is tested through the
existing `fakebulb`, which already connects, identifies, and expects a CONNACK.

**Target Platform**: Unchanged — one binary on the household LAN. Container packaging is
explicitly out of scope; this feature is what makes it mechanical later.

**Project Type**: Single binary, two runtime modes over the same core.

**Behavioural expectations**: Interactive mode is bit-for-bit unchanged (SC-010). An
invalid setting is refused before any listener opens (SC-005). No credential appears in
any record at any level (SC-006).

**Constraints**: No new dependency without a justification the constitution accepts. The
lamps still never reach the internet. Nothing in headless mode may write non-record bytes
to standard output.

**Scale/Scope**: One household, a handful of lamps. Two new small packages, one new
predicate in the server, one new registry method, one document.

## Constitution Check

*GATE: checked before Phase 0 research and re-checked after Phase 1 design.*
Constitution v2.0.0, three principles.

| Principle | How this plan complies | Verdict |
|---|---|---|
| I. Code Quality | No new gates needed; `gofmt`/`go vet`/CI unchanged. Doc comments on every new exported symbol. The settings table is one declaration used for defaults, environment names, flag names, and the documentation table — the alternative, four parallel lists, is exactly the duplication this principle exists to prevent | PASS |
| II. Testing Standards | `internal/config` and `internal/logging` are table-driven from the first commit. Admission control gets an integration test through `fakebulb` — a known lamp connects and is served, an unknown one is refused — because it crosses the protocol boundary. Redaction gets its own test asserting a password never appears in output, since that is a claim, not an intention | PASS |
| III. UX Consistency | The record vocabulary is the existing `events.Kind` set, not a new one: what the terminal shows and what headless mode prints describe the same events with the same words. A rejected lamp reads the same way in both. Configuration errors follow the existing shape — what failed, what was expected, how to fix it | PASS |
| Dependency constraint | Zero new dependencies; the viper question is answered in research.md §1 with the exact swap path if the judgement is wrong | PASS |

**Post-Phase-1 re-check**: PASS. One point deserves stating: the admission predicate is an
interface-shaped hook with one real implementation, which the "no speculative abstraction"
rule would normally reject. It survives because it has two implementations from day one —
`nil` (interactive: admit everything, today's behaviour preserved by construction) and the
configured set — and because the alternative is `internal/server` importing `internal/config`,
which inverts the dependency direction the rest of the tree follows.

## Project Structure

### Documentation (this feature)

```text
specs/003-headless-deployment/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── configuration.md # Every setting: name, environment variable, flag, default
│   └── log-records.md   # The JSON-lines record shape
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
cmd/haigosmartd/
└── main.go              # CHANGED: config.Load replaces the inline flag block;
                         #          admission predicate wired in headless mode

internal/
├── config/              # NEW
│   ├── config.go        #   Config struct, settings table, Load, Validate, LogValue
│   ├── lamps.go         #   the id=name list: parse, validate, collisions
│   └── *_test.go
├── logging/             # NEW
│   ├── logging.go       #   handler construction, compact time, since, fatal writer
│   └── *_test.go
├── server/
│   ├── server.go        # CHANGED: Admit predicate, rejection rate limiting
│   └── session.go       # CHANGED: consult Admit after identity, before CONNACK
├── protocol/
│   └── mqtt.go          # CHANGED: ConnackNotAuthorized (0x05)
├── registry/
│   ├── registry.go      # CHANGED: Declare(deviceID, name)
│   └── persist.go       # CHANGED: OnError hook so a read-only file is reported once
└── events/
    └── event.go         # CHANGED: Rejected kind and its wording

docs/
├── deploying.md         # NEW: first lamp → unattended instance, and the settings table
└── operating.md         # CHANGED: points at deploying.md for the unattended path
```

**Structure Decision**: Unchanged layout — `golang-standards/project-layout`, one binary
under `cmd/`, everything else `internal/`. The two new packages sit alongside the existing
ones at the same level; neither is imported by anything except `main`, which is what keeps
the core unaware of how it was configured or where its records go.

## Complexity Tracking

> No constitution violations to justify. The one judgement call that could read as a
> violation — writing a configuration loader rather than importing the library the spec
> named — is recorded here for the record, and reversed by one file if the call is wrong.

| Decision | Why | Alternative rejected because |
|---|---|---|
| Hand-written `internal/config` instead of spf13/viper | Environment-only configuration (Q1) needs a defaults table, environment lookup, and validation — about 120 lines. `flag` already supplies the table, the parsing, and the type conversion | Viper's value is in the parts not used here: file formats, watching, remote backends, multi-source precedence. Its cost is ~15 modules in `go.mod` for a project whose stated principle is stdlib-first. The swap remains one file if this proves wrong — see research.md §1 |
