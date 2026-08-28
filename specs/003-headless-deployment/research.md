# Research: Headless Deployment

**Feature**: 003-headless-deployment | **Date**: 2026-08-28

## 1. Configuration: spf13/viper or the standard library

**Decision**: Hand-written `internal/config`, built on `flag` and `os.Getenv`. No new
dependency. **Reviewed and accepted by the operator on 2026-08-28**, with the revisit
trigger named: if file-based configuration is ever introduced, viper becomes worth its
cost and should be reconsidered then. The package exposes one function — `Load(args []string, env func(string) string)
(Config, error)` — so the choice is reversible in one file.

**Rationale**: The spec's Q1 answer fixed configuration to environment variables only. That
removes almost everything viper is for:

| What viper provides | Used here? |
|---|---|
| YAML/TOML/JSON/INI/HCL config files | No — Q1 chose environment only |
| Watching files and reloading live | No — a settings change is a restart, which is how the orchestration platform expects to apply one |
| Remote backends (etcd, Consul) | No |
| Precedence merging across many sources | Barely — three sources, and `flag.FlagSet` already gives last-writer-wins for free |
| `AutomaticEnv` + prefix | Yes, and it is four lines of `os.Getenv` over `flag.VisitAll` |
| Unmarshalling into a struct | Yes, but the lamp list needs a custom parser regardless |

What remains is a defaults table, an environment lookup, and validation. `flag.FlagSet` is
already the defaults table: it holds the name, the default, the type conversion, and the
help text for every setting, and the settings documentation is generated from the same
declaration. Adding viper would mean maintaining that table twice.

The mechanism, in full:

1. Declare every setting once on a `flag.FlagSet`, exactly as `main.go` does today.
2. `fs.VisitAll` — for each flag, look up `HAIGOSMART_` + the flag name uppercased with
   `-` → `_`. If the variable is set, `fs.Set(name, value)`. A parse failure here is a
   startup error naming the variable, the value, and the expected form (FR-013).
3. `fs.Parse(args)` — command-line values overwrite whatever the environment put there, so
   FR-011's precedence falls out of the ordering rather than out of a precedence engine.
4. `fs.Visit` afterwards lists the flags actually given on the command line; the
   intersection with the variables seen in step 2 is exactly the set of overrides to
   record, which is the other half of FR-011.

The constitution requires a one-line justification for a new dependency ("what stdlib or
existing dependency was insufficient"). For viper the honest answer would be "nothing was
insufficient", which is the answer that fails the test. Feature 001's stated constraint —
"do not use complex dependencies, focus on the standard library, only use libs where
necessary" — points the same way.

**Alternatives considered**:

- **spf13/viper** (as the spec suggested). Rejected for the reasons above: ~15 modules
  (`afero`, `cast`, `mapstructure`, `pflag`, `fsnotify`, three file-format parsers, and
  their transitive set) to replace ~120 lines, with the features that justify that cost all
  switched off. **If this judgement is wrong the swap is one file** — `Load` keeps its
  signature and `Config` keeps its shape; nothing else in the tree knows how it was filled.
- **`kelseyhightower/envconfig`** — much closer to the right size, struct tags instead of a
  table. Rejected only because it still cannot supply flag precedence (FR-011), so `flag`
  would remain alongside it and the settings table would exist twice anyway.
- **`caarlos0/env`** — same shape, same reason.
- **A config file format of our own** — rejected by Q1.

## 2. Environment variable naming and the lamp set

**Decision**: `HAIGOSMART_` prefix, name derived mechanically from the existing flag:
`-mqtt-broker` → `HAIGOSMART_MQTT_BROKER`. The lamp set is one variable,
`HAIGOSMART_LAMPS`, holding comma-separated `deviceID=name` pairs.

**Rationale**: A mechanical derivation means no second table and no possibility of a flag
and its variable drifting apart. Every setting is addressable both ways, which is what makes
"start with an empty command line" (SC-001) and "override one thing for a debugging run"
both work.

For the lamp set, one variable holding the whole list beats indexed variables
(`HAIGOSMART_LAMP_0_ID`, `..._0_NAME`) on every axis that matters here: it is one line in a
manifest, it is what a person would write by hand, and a household has a handful of lamps,
not hundreds. The parser reports a bad entry by its position and its content rather than
skipping it, because a silently dropped lamp is a dark room with no explanation (FR-010).

Validation, all at startup, all refusing to run rather than repairing (FR-013):

| Rule | Message names |
|---|---|
| Entry contains exactly one `=` | the entry, its position |
| Identifier non-empty | the position |
| Name non-empty | the identifier |
| No duplicate identifier | both positions |
| No duplicate name | both identifiers |
| Name is not a terminal verb (`on`, `off`, `bri`, …) | the name, the reason |

The verb rule already exists in `internal/control` for interactive renames; reusing it keeps
a lamp from being configured with a name that the terminal could not then address, which
would be a difference in behaviour between the two modes for no reason.

**Alternatives considered**: JSON in one variable (`[{"id":...,"name":...}]`) — unambiguous,
but nobody wants to write JSON inside YAML, and quoting mistakes would be the most common
failure. Indexed variables — verbose, and the gaps-in-the-sequence question has no good
answer. A separate lamps file — rejected by Q1.

## 3. Record format: compact time and the difference

**Decision**: `log/slog`'s `JSONHandler`, one record per line. `time` is formatted
`2006-01-02 15:04:05.000` in local time; every record additionally carries `since`, the
elapsed time from process start, formatted by `time.Duration.String()` truncated to
milliseconds.

```json
{"time":"2026-08-28 14:03:12.123","since":"1m12.345s","level":"INFO","msg":"bulb connected","kind":"connected","device":"a1b2c3d4","name":"headlamp"}
```

**Rationale**: The spec asked for a compact format carrying the date, the time, and the time
difference. Dropping the `T` and the zone offset from RFC3339 takes the timestamp from 30
characters to 23 while staying sortable, unambiguous, and readable without mental
arithmetic. The year stays: these records outlive the session that produced them.

`since` is measured from process start rather than from the previous record. Two reasons.
It is well defined under concurrency — records are produced from many goroutines, and a
"difference from the previous record" would depend on which goroutine won a race, making the
same run print different numbers. And what it answers is the question actually asked of a
restarting service: *how far into this run did that happen?* An interval between two records
is then a subtraction the reader can do, and a log collector can do exactly, from two values
that do not depend on ordering.

**Alternatives considered**:

- **Delta from the previous record** (`journalctl -o short-monotonic` style). Rejected for
  the race above; it also needs a mutex on every record for a number that is then wrong
  whenever anything is dropped or reordered downstream.
- **Unix epoch seconds.** Machine-friendliest, but unreadable when the only tool available
  is `kubectl logs`, and the spec asked for a date and a time.
- **Both packed into one field** (`"time":"2026-08-28 14:03:12.123 +1m12.345s"`). Fewer
  bytes, but it makes a structured field carry two values, which every collector then has to
  split apart again.
- **UTC instead of local.** Correct for a fleet; wrong for one household where the operator
  compares the record against the moment a lamp visibly changed. Local, with the offset
  recoverable from the host, is the better trade here. Noted as the first thing to revisit
  if this ever runs anywhere but home.

## 4. Where the records go, and what happens when nobody is listening

**Decision**: Headless mode writes to `os.Stdout` and nothing else; interactive mode keeps
writing to a file, unchanged. Both use the same handler and the same format. A write failure
on the record stream terminates the process with a message on standard error.

**Rationale**: A container's log stream is its standard output; that is the whole convention.
The existing rule — never write records to the terminal while the interface is drawn — is
untouched, because it was right for the reason it was written.

The failure behaviour is the spec's edge case: an unattended server whose output goes
nowhere is not running, it is only consuming electricity. `slog` discards handler write
errors by design, so the check lives in a writer wrapper: one failed write to stdout, one
line on stderr, exit non-zero. The orchestration platform's restart policy is then in charge,
which is the correct place for that decision.

## 5. Where admission control belongs

**Decision**: `server.Server` gains `Admit func(deviceID string) bool`. It is consulted in
the session once the CONNECT has been parsed and the identity is known, before the CONNACK.
A refused lamp receives CONNACK return code `0x05` (*connection refused, not authorized*) and
the connection is closed. Nil means admit everything, which is exactly today's behaviour.

**Rationale**: The decision needs the device identity, which does not exist before CONNECT
is parsed, and it must happen before the server does anything that outlives the connection —
before `Upsert`, so that nothing about a rejected lamp is ever written to the registry
(FR-017). That leaves exactly one point in the flow, and it is the one where MQTT already
has a vocabulary for this answer. Sending `0x05` rather than dropping the socket means the
lamp is told, and anyone reading a packet capture sees a refusal instead of a mystery.

A predicate rather than a set keeps `internal/server` from importing `internal/config`,
which would point the dependency arrows the wrong way for a package that is otherwise
configuration-agnostic.

**Rate limiting** (FR-017a): a rejected lamp reconnects forever, so an unrate-limited
rejection record is an unbounded log. The server already has this exact machinery for
takeover warnings (`noteTakeover`) — the same shape is reused: record the first rejection per
device identifier immediately, then at most one per five minutes. The record says how many
attempts it covers so the suppression is visible rather than silent.

**Alternatives considered**: refusing at the TCP layer by address — impossible, the identity
is in the CONNECT. Accepting and serving but never persisting (option B in the spec's Q3) —
rejected by the operator. Holding unknown lamps in memory as pending (option C) — rejected:
an unbounded in-memory set fed by anything that reaches the port.

## 6. Making the configured lamp set authoritative

**Decision**: `Registry.Declare(deviceID, name)` creates a disconnected entry for a
configured lamp that has none, and renames one whose stored name disagrees. It runs at
startup, after the registry file is loaded, before the listener opens. Lamps in the file but
not in the configuration are left in the file, excluded from the admission set, and reported
once at startup.

**Rationale**: This is what Q2's "configuration is authoritative" means in practice, and it
is what makes Home Assistant correct from the first second: the lamps are published from the
registry snapshot, so a declared lamp appears immediately — present, named, and unavailable —
rather than materialising whenever it happens to connect. A restart therefore changes nothing
visible in Home Assistant, and a lost registry file changes nothing at all except state the
lamps re-report on connecting.

Leaving unconfigured lamps in the file rather than deleting them keeps the interactive mode's
history intact — the operator can switch to the terminal and still see everything ever
adopted — while the admission set ensures the unattended instance will not serve them. The
startup report exists because the failure it catches is silent otherwise: one lamp dropped
from a manifest by a bad edit, and the only symptom is a room that stops responding.

## 7. Reporting a registry file that cannot be written

**Decision**: `Store` gains an `OnError func(error)` hook. The first background save failure
is reported at warning level; later ones go to debug.

**Rationale**: Background save failures are currently discarded (`_ = s.Flush()`), which was
harmless when the file was authoritative and the process was interactive — the shutdown save
would report the problem to someone watching. A read-only mount makes every save fail
forever, so reporting each one would flood the stream, and reporting none would hide a real
misconfiguration. First loud, rest quiet, is the shape used elsewhere in this codebase for
exactly this situation. The instance keeps running, because under Q2 the file is a cache and
a cache that cannot be written is a degradation, not a failure.
