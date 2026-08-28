# Data Model: Local Replacement Server for Aigo Smart Bulbs

**Feature**: 001-local-bulb-server | **Date**: 2026-08-27

Entities as they exist in memory and on disk. Field types are Go types because this
document drives implementation; the persisted shape is in
[contracts/registry-file.md](./contracts/registry-file.md).

Fields marked **(capture)** cannot be finalised until gate G1 — their existence is certain,
their type or range comes from the protocol.

---

## Bulb

`internal/bulb.Bulb` — one physical bulb known to the server. Spec: FR-003, FR-005, FR-019.

| Field | Type | Notes |
|---|---|---|
| `DeviceID` | `string` | Stable identifier reported by the bulb. Primary key. **(capture)** — its source field and encoding come from the handshake |
| `Name` | `string` | Operator-assigned. Defaults to `DeviceID` until set. Unique among bulbs; the registry rejects a duplicate rather than silently reassigning |
| `Status` | `Status` | See state machine below |
| `State` | `LightState` | Last state **reported by the bulb**, authoritative over any commanded value (FR-019) |
| `Desired` | `*LightState` | Last commanded state, nil when none pending. Kept only to detect and display divergence; never displayed as if it were the truth |
| `FirstSeen` | `time.Time` | When first registered |
| `LastSeen` | `time.Time` | Last message of any kind, including keep-alives |
| `RemoteAddr` | `string` | Current or last peer address, for diagnostics |
| `Capabilities` | `Capabilities` | What this bulb supports (FR-010 / white-only bulbs) |

### LightState

| Field | Type | Notes |
|---|---|---|
| `Power` | `bool` | |
| `Brightness` | `uint8` | Normalised 0–100 at this layer. The protocol's native range is converted in `internal/protocol` so nothing above it deals in device units **(capture)** |
| `Color` | `RGB` | `{R, G, B uint8}`. Normalised; the wire colour model (RGB / HSV / HSL) is converted in the codec **(capture)** |
| `ColorTempK` | `uint16` | Kelvin, 0 when unsupported or not in white mode **(capture)** |
| `Mode` | `Mode` | `ModeColor` or `ModeWhite`; some bulbs treat these as exclusive **(capture)** |
| `ReportedAt` | `time.Time` | When the bulb reported this state |

### Capabilities

| Field | Type | Notes |
|---|---|---|
| `Color` | `bool` | False for white-only models; colour commands to these return `ErrUnsupported`, surfaced as "unsupported", never silently dropped (spec assumption) |
| `ColorTemp` | `bool` | |
| `MinBrightness` | `uint8` | Some bulbs cannot reach 0 without switching off |
| `Known` | `bool` | False when capabilities could not be determined. Drives the `Unknown` behaviour below — never leave the caller guessing whether `Color: false` means "no colour" or "we never found out" |

**Source**: populated by `internal/protocol` at registration from the device-metadata
message, per [contracts/bulb-protocol.md](./contracts/bulb-protocol.md) §5a **(capture)**.
If the protocol carries no capability data, they are inferred on first contact from which
fields the bulb's state report actually contains, and that inference is persisted. The zero
value is never used as if it were an answer: a bulb whose capabilities could not be
determined is recorded as `Unknown` and its colour commands are attempted rather than
pre-refused, so an unset field can never silently take the white-only branch.

### Status state machine

```text
                 handshake ok, ID unknown
   (new conn) ─────────────────────────────► Discovered
                                                  │ operator names/adopts it
                 handshake ok, ID known           ▼
   (new conn) ─────────────────────────────► Connected ◄──┐
                                                  │       │ reconnect
                            missed keep-alives    │       │ (same DeviceID → same entry, FR-005)
                            or socket closed      ▼       │
                                            Disconnected ─┘
```

- `Discovered` → surfaced to the operator (FR-017); state-changing commands are rejected
  until adopted. **`name` is the adopt action** (contracts/tui-commands.md §Adoption):
  naming a discovered bulb registers it, persists it, and moves it to `Connected`.
  `list` and `info` work on discovered bulbs; nothing else does.
- `Connected` → `Disconnected` on socket close **or** on missing N keep-alive intervals,
  N and the interval taken from step 14 of the capture sequence.
- A returning bulb always rejoins its existing entry by `DeviceID`; a new entry is never
  created for a known ID (FR-005).
- Two bulbs presenting the same `DeviceID` (spec edge case): both connections are kept and
  distinguished by `RemoteAddr`; the registry raises a `DuplicateID` event and the TUI warns.
  Neither entry is overwritten.

---

## Command

`internal/control.Command` — one operator intent. Spec: FR-008 to FR-012.

| Field | Type | Notes |
|---|---|---|
| `Target` | `string` | Name or `DeviceID` as typed. Resolution order: exact name, then exact `DeviceID`, then unique case-insensitive prefix of either. Ambiguity is an error listing the candidates, never a guess |
| `Action` | `Action` | `ActionOn`, `ActionOff`, `ActionBrightness`, `ActionColor`, `ActionColorTemp`, `ActionName` |
| `Arg` | `any` | Typed per action; validated before dispatch |
| `IssuedAt` | `time.Time` | |
| `Outcome` | `Outcome` | `Pending` → `Accepted` \| `Failed` |
| `Err` | `error` | Set on `Failed`; message states what failed and how to fix it (Constitution III) |

### Validation (applied before anything reaches the network)

| Action | Rule | Error when violated |
|---|---|---|
| any | target resolves to exactly one bulb | `unknown bulb %q` / `ambiguous target %q: matches %v` |
| any except `ActionName` | target is `Connected` | `bulb %q is not connected (last seen %s)` |
| `ActionBrightness` | `0 <= v <= 100` | `brightness must be 0-100, got %d` |
| `ActionColor` | parses as `#RRGGBB` or a known colour name | `colour must be #RRGGBB or a name, got %q` |
| `ActionColor` | `Capabilities.Color` | `bulb %q does not support colour` |
| `ActionColorTemp` | within the bulb's Kelvin range | `colour temperature must be %d-%dK` |
| `ActionName` | non-empty, not already in use | `name %q already used by %s` |
| any except `ActionName` | target is not `Discovered` (i.e. already adopted) | `%s: not adopted yet. run \`name %s <a-name>\` first` |

Commands have a per-command timeout (default 3 s, under SC-003's 1 s target with headroom).
On timeout the outcome is `Failed`, not an indefinite wait (spec edge case).

---

## Event

`internal/events.Event` — anything worth showing or logging. Spec: FR-013, FR-014, FR-018.

| Field | Type | Notes |
|---|---|---|
| `At` | `time.Time` | |
| `Kind` | `Kind` | `StateChanged`, `Connected`, `Disconnected`, `Discovered`, `CommandResult`, `ProtocolError`, `DuplicateID` |
| `DeviceID` | `string` | Empty for server-level events |
| `Name` | `string` | Display name at emit time |
| `Changed` | `[]FieldChange` | `{Field, From, To string}` — only fields that actually changed |
| `Detail` | `string` | Error text or command result |

Every event goes to `log/slog` unconditionally — that is the complete record SC-008
requires. The TUI feed additionally receives it over a buffered channel with a drop-oldest
policy, so a stalled UI cannot block a bulb's read loop (SC-009). Events
missing from the *display* are counted and that count is shown in the feed header; they are
never missing from the record.

---

## Registry

`internal/registry.Registry` — the authoritative in-memory set of bulbs, guarded by a
`sync.RWMutex`, persisted per [contracts/registry-file.md](./contracts/registry-file.md).

Persisted: `DeviceID`, `Name`, `Capabilities`, `FirstSeen`, `LastSeen`, last known `State`.
Not persisted: `Status` (always starts `Disconnected` — nothing is assumed still online
across a restart), `Desired`, `RemoteAddr`, live connection handles.

Writes are debounced (~2 s) and coalesced so a burst of state reports produces one file
write, then flushed unconditionally on shutdown.
