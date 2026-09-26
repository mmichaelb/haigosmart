# Contract: Bulb Session Close on Server Shutdown

This is an internal behavioral contract (`internal/server` package), not a network protocol or CLI contract — there is no new external interface in this feature, so this is the only contract document needed.

## Before this feature

On server shutdown (`SIGTERM`/`SIGINT` → context cancellation), `session.Close()` (`internal/server/session.go:59-68`) calls the underlying `net.Conn.Close()` directly. This produces a graceful TCP close (FIN/ACK) from the bulb's point of view.

## After this feature

`session.Close()` sets `SO_LINGER=0` on the underlying `*net.TCPConn` immediately before calling `Close()`, when the connection is a TCP connection (real deployments; a test double or non-TCP `net.Conn` skips this step harmlessly). This produces a hard reset (RST) from the bulb's point of view instead of a graceful close.

## What does NOT change (explicit non-goals of this contract)

- **Local-side error reporting is unchanged.** `reportDisconnect` (`internal/server/session.go:305-323`) still classifies a shutdown-triggered close as `"server shutting down"` via `errors.Is(err, net.ErrClosed)` / `errors.Is(err, context.Canceled)` — these checks observe the *local* close, which behaves identically regardless of `SO_LINGER`. The operator-visible event text does not change.
- **A bulb-initiated disconnect, an idle timeout, and a protocol error close are unaffected in practice**, even though they share the same `session.Close()`/read-loop teardown code as the shutdown path: those paths end with the *bulb* having already dropped the TCP connection (`"closed by bulb"`) or the server timing out on silence — there's no live graceful FIN for `SetLinger(0)` to change in the first place, since the peer either already closed or already went quiet. `session.Close()` is also called during a same-device takeover (`internal/server/session.go:271`) to drop the old, already-abandoned socket; setting `SO_LINGER=0` there is likewise a no-observable-difference change, since nothing is listening on that stale connection any more. Rather than threading a "why am I closing" flag through `Close()` for no behavioral benefit, this feature sets `SO_LINGER=0` unconditionally inside `session.Close()` — the one method already shared by every path that ends a session.
- **Other bulbs' connections are unaffected** (existing guarantee, `specs/001-local-bulb-server` FR-016) — this change is per-session, not global.
- **No new wire-level message is introduced.** The bulb-facing protocol (vendor MQTT variant) gains no new packet type; the contract operates entirely at the TCP layer, beneath the protocol this project speaks to the bulb.

## Verification

A test client connected over a real TCP loopback listener observes, after this change, that a server-initiated shutdown close surfaces as a reset-style read error (e.g. `ECONNRESET`) rather than `io.EOF`. This is the testable boundary of this contract — see `research.md`'s Testability note for why firmware-level reconnect behavior itself is out of reach for automated verification.
