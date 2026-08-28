# Contract: TUI Command Grammar

**Feature**: 001-local-bulb-server | Spec: FR-007 to FR-012, FR-016, FR-017

One grammar, one error shape, one event-line format across the whole interface
(Constitution III). Breaking any of the below is a user-facing contract change and needs a
migration note.

## Layout

```text
┌──────────────────────────────────────────────────────────────┐
│ haigosmart · 4 bulbs · 3 connected · 1 discovered            │  status bar
├──────────────────────────────────────────────────────────────┤
│ 14:02:11  kitchen      connected                             │
│ 14:02:11  kitchen      power off→on  brightness 40→80        │  event feed
│ 14:02:19  desk         power on→off  (wall switch)           │  (scrolls, newest at bottom)
│ 14:03:02  ??-a41f2c    discovered — name it to control it    │
├──────────────────────────────────────────────────────────────┤
│ > on kitchen                                                 │  command prompt
└──────────────────────────────────────────────────────────────┘
```

The prompt stays responsive while events arrive (FR-015). Resizing reflows; it never
corrupts the display (spec edge case).

## Commands

| Command | Arguments | Effect |
|---|---|---|
| `list` | — | Table of all bulbs: name, id, status, power, brightness, colour, last seen |
| `on` | `<target>` | Power on (FR-008) |
| `off` | `<target>` | Power off (FR-008) |
| `bri` | `<target> <0-100>` | Set brightness (FR-009) |
| `color` | `<target> <#RRGGBB\|name>` | Set colour (FR-010) |
| `temp` | `<target> <kelvin>` | Set colour temperature |
| `name` | `<target> <new-name>` | Assign a display name (FR-011) |
| `info` | `<target>` | Full detail for one bulb: capabilities (colour, colour temperature, brightness floor), current state, and any commanded/reported divergence |
| `help` | `[command]` | Syntax summary; on-screen help is sufficient to complete a first task (SC-007) |
| `quit` | — | Flush the registry and exit cleanly |

`<target>` is a name or device ID. Resolution: exact name → exact ID → unique
case-insensitive prefix. Ambiguity is an error listing candidates, never a guess.

### Adoption

A bulb that has connected but is not yet in the registry has status `discovered`. It is
visible to `list` and `info`, but state-changing commands are refused until it is adopted.

**`name` is the adopt action.** Naming a `discovered` bulb registers it, persists it, and
moves it to `connected`; from then on it is controllable and survives restarts. There is no
separate `adopt` command — one verb, because a bulb worth keeping is a bulb worth naming.

```text
> on ??-a41f2c
error   ??-a41f2c: not adopted yet. run `name ??-a41f2c <a-name>` first
> name ??-a41f2c kitchen
ok      kitchen: adopted (was ??-a41f2c)
> on kitchen
ok      kitchen: on
```

Renaming an already-adopted bulb uses the same command and is not an adoption:

```text
ok      kitchen: renamed from kitchen-1
```

Adoption is per-bulb and never automatic — an unknown device appearing on the network is
surfaced, not silently trusted (FR-017).

Colour names accepted: the standard CSS basic set (`red`, `green`, `blue`, `white`,
`warmwhite`, `yellow`, `cyan`, `magenta`, `orange`, `purple`). Anything else must be hex.

### Keys

| Key | Action |
|---|---|
| `Enter` | Submit |
| `↑` / `↓` | Command history |
| `PgUp` / `PgDn` | Scroll the event feed |
| `Ctrl+C` | Same as `quit` — flush and exit, never a hard kill mid-write |

## Output shapes

Exactly three, used everywhere:

```text
ok      <what happened>
error   <what failed>: <why>. <how to fix it>
info    <neutral statement>
```

Examples — these are the contract, not illustrations:

```text
ok      kitchen: on
ok      kitchen: brightness 80
error   unknown bulb "kitchn": no bulb by that name or id. run `list` to see registered bulbs
error   desk: not connected (last seen 14:02:19). check the bulb has power
error   brightness must be 0-100, got 150
error   ambiguous target "k": matches kitchen, kids-room. use a longer prefix
error   hall: does not support colour. this bulb accepts `on`, `off`, `bri`, `temp`
error   unknown command "dim". commands: list on off bri color temp name info help quit
```

Every error names what failed and what to do about it (Constitution III). No bare codes.
A malformed command changes no bulb state (FR-016).

## Event feed lines

```text
HH:MM:SS  <name-or-id>  <event text>
```

| Kind | Text form |
|---|---|
| `Connected` | `connected` |
| `Disconnected` | `disconnected (<reason>)` |
| `Discovered` | `discovered — name it to control it` |
| `StateChanged` | `power off→on  brightness 40→80` (only changed fields, space-separated) |
| `CommandResult` | `command failed: <reason>` (failures only; successes already echo at the prompt) |
| `ProtocolError` | `protocol error: <detail>` |
| `DuplicateID` | `WARNING duplicate device id, also seen from <addr>` |

When the feed buffer overflows, the status bar shows `… N events dropped from view (all
logged)` — dropped from the display only; the log keeps everything.

## CLI flags (`cmd/haigosmartd`)

| Flag | Default | Purpose |
|---|---|---|
| `-listen` | `:<protocol port>` | Bulb listener address |
| `-registry` | `$XDG_CONFIG_HOME/haigosmart/registry.json` | Registry file path |
| `-log` | stderr-to-file when the TUI owns the terminal | Structured log destination |
| `-headless` | `false` | Run without the TUI (log only) — for running under systemd |
| `-v` | `false` | Debug logging, including per-frame protocol traces |
