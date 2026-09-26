# Data Model: Automatic Bulb Reconnect After Server Restart

No new entities, fields, or persisted state. This feature changes *how* an existing connection is closed and relies entirely on data structures that already exist.

## Reused: `events.Event` (`internal/events/event.go`)

The spec's "bulb connection record" (spec.md Key Entities) maps directly onto the existing `Event` type — no schema change:

| Field | Used as |
|---|---|
| `Kind` | `Disconnected` marks the shutdown-triggered close; `Connected`/`Discovered` marks the reconnect. No new `Kind` value is added. |
| `DeviceID` | Correlates a disconnect with its later reconnect for the same bulb — this *is* the reconnect record; no separate table is needed. |
| `Detail` | Already carries `"server shutting down"` for the shutdown-triggered case (`internal/server/session.go:316`), distinguishing it from other disconnect causes (`"no keep-alive for …"`, `"closed by bulb"`) — this is what satisfies FR-004 (distinguishing a restart-caused disconnect from an unrelated one). |
| `At` | Gives the "when" half of FR-003 for both the disconnect and the eventual reconnect. |

## State transitions (unchanged, reused)

```text
connected --(server shutdown: Disconnected, detail="server shutting down")--> disconnected
disconnected --(bulb reconnects: Discovered if new, Connected if already known)--> connected
disconnected --(bulb never reconnects)--> disconnected (no further event; distinguishable from
                                                          "connected" by the last event's Kind)
```

This is the existing state machine already implemented by `internal/registry` and `internal/server/session.go`; this feature does not add a state or a transition, it only changes which network-level closure produces the existing `Disconnected` transition.

## Explicitly not modeled

- A "restart epoch" or "restart id" grouping multiple bulbs' reconnects under one restart event — no consumer needs this yet (see research.md Decision 2); an operator reading the per-device event sequence already gets the answer.
- A persisted "did this bulb ever successfully auto-reconnect" flag — the live event feed already answers this for as long as it's retained; adding persisted historical rollup is out of scope until a concrete second consumer exists.
