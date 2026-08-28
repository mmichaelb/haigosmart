# Contract: lights.Service

**Feature**: 002-homeassistant-integration | Spec: FR-003 to FR-010, FR-017, FR-018

The internal boundary both front-ends consume. `internal/control` (terminal) and
`internal/hass` (Home Assistant) each depend on this and never on each other.

## Rules

1. **Addressing is by stable device id.** Name and prefix resolution stays in the terminal
   adapter, where a human is typing. An integration that guessed which lamp was meant would
   be a bug with the lights on.
2. **Errors are typed, never formatted.** A caller decides how an error reads. No method
   returns a display string.
3. **The lamp's report is authoritative.** No method writes state into the registry
   directly; state changes only when a lamp reports one. Inherited from feature 001.
4. **Validation happens once, here.** Range and capability checks are not duplicated in
   either front-end, or they will drift.
5. **Nothing here imports a front-end.** `internal/lights` must not import `control`, `tui`,
   `hass`, or Bubble Tea. A `go list` check enforces it.

## Operations

| Method | Returns | Notes |
|---|---|---|
| `Snapshot()` | `[]bulb.Bulb` | Copies, ordered by name |
| `Get(id)` | `bulb.Bulb, error` | `ErrUnknownBulb` if absent |
| `SetPower(ctx, id, on)` | `error` | |
| `SetBrightness(ctx, id, pct)` | `error` | `ErrOutOfRange` outside 0–100 |
| `SetColorTemp(ctx, id, pct)` | `error` | `ErrOutOfRange`; `bulb.ErrUnsupported` if the lamp has no warmth and capabilities are `Known` |
| `Apply(ctx, id, Change)` | `error` | Partial update; nil fields untouched |
| `Subscribe(depth)` | `*events.Subscription` | The existing bus, unchanged |

## Error contract

```go
var (
    ErrUnknownBulb = errors.New("no such bulb")
    ErrNotAdopted  = errors.New("bulb has not been adopted")
    ErrNotConnected = errors.New("bulb is not connected")
    ErrOutOfRange  = errors.New("value out of range")
)
```

Errors are wrapped with context (`fmt.Errorf("...: %w", ErrOutOfRange)`) so callers use
`errors.Is` and still get a usable message. `bulb.ErrUnsupported` and `bulb.ErrUnconfirmed`
are re-used from feature 001 rather than redefined.

**`ErrUnconfirmed` is not a failure.** It means delivered-but-not-yet-confirmed, which real
hardware produces routinely — confirmation has been observed between a fraction of a second
and nineteen seconds after the fact. Any caller treating it as failure is wrong.

## Behaviour preserved from feature 001

These are not new decisions; they are the existing behaviour, now stated where both surfaces
can see it:

- One property per command on the wire, only for values that actually changed.
- A command completes on either the bulb's acknowledgement or its state report.
- A no-op request sends nothing and succeeds immediately.
- `Desired` is recorded for divergence display only and never shown as truth.
- Commands are refused for lamps that are not adopted or not connected.

## Compatibility gate

The terminal's **behaviour** must not change; its tests may be adapted to the moved API
(gate G1, relaxed 2026-08-28). The line is between shape and substance:

- Fine: calling `svc.SetBrightness(ctx, id, 80)` where a test called `ctrl.Run(...)`;
  matching `errors.Is(err, lights.ErrOutOfRange)` instead of an error string.
- Not fine: loosening an assertion, dropping a case, or editing an expected value to make
  a test pass.

The exact command grammar and error strings in feature 001's `contracts/tui-commands.md`
are what must still come out of the terminal. With the tests no longer frozen, that file is
the reference — check against it, not against the old test.
