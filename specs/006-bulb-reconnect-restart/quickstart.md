# Quickstart: Validate Automatic Bulb Reconnect After Server Restart

Two validations: one proves the code change (mechanism), runnable in CI/locally with no real hardware; one is a manual real-hardware check for the unresolved firmware-behavior question (see `research.md` Decision 3), to be run whenever real bulbs are available.

## 1. Mechanism validation (automated, no hardware needed)

**Prerequisites**: Go 1.27 toolchain, this repo checked out on the `006-bulb-reconnect-restart` branch.

**Run**:

```sh
go test ./internal/server/... -run TestSessionCloseOnShutdown -race -v
```

**Expected outcome**: A test connects a client over a real TCP loopback listener, triggers the server's shutdown-close path, and asserts the client's next read returns a reset-style error (not `io.EOF`) — proving `session.Close()` now performs a hard reset on the shutdown path per `contracts/shutdown-close-behavior.md`. (Exact test name is chosen during `/speckit-tasks`/implementation; update this command if it differs.)

**Also run the full suite** to confirm no regression to existing session/server tests (`TestReconnectRejoinsSameEntry`, `TestPowerCycleReconnectIsNotADuplicate`, and the rest of `internal/server`):

```sh
go test ./... -race
```

## 2. Real-hardware observation (manual, requires at least one real bulb)

This is the substitute for pre-ship validation that the assessment determined isn't achievable (see `decision.md`) — it's how the open question ("does this actually help?") gets answered, after the fact, in a real deployment.

**Prerequisites**: `haigosmartd` running headless or interactively (per `docs/deploying.md`) with at least one bulb already connected and named.

**Steps**:

1. Confirm the bulb is connected and controllable (e.g. `list` in the TUI, or check the event feed for a recent `Connected`/`Discovered` entry for its device id).
2. Restart the server process (`SIGTERM`/`SIGINT`, or however the deployment normally restarts — see `docs/deploying.md` Shutdown section).
3. Watch the event feed/log for that device id:
   - Expect a `Disconnected` entry with detail `"server shutting down"` at shutdown (unchanged from today).
4. Without touching the bulb's power, wait up to the informal target window (~60s, per the existing quickstart at `specs/001-local-bulb-server/quickstart.md:66`) and watch for a `Connected`/`Discovered` entry for the same device id.

**Expected outcome (what's being validated)**:

- If the `Connected`/`Discovered` entry appears without any manual power-cycle: the fix worked for this bulb/firmware — record the observation (model/firmware version if known, and elapsed time) so it can be compared across bulb models later (SC-001, SC-004).
- If it does **not** appear within a reasonable window even after a few minutes: the RST-close hypothesis did not resolve the issue for this bulb/firmware, and the event feed's `Disconnected` entry with no following `Connected` clearly distinguishes "still trying" from a bulb that was never touched by the restart at all — satisfying FR-004 and the "gave up vs. still trying" edge case in spec.md, and feeding back into whether a different concept option (B or C's alternative, or a fresh assessment) is warranted.
