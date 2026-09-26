# Feature Specification: Automatic Bulb Reconnect After Server Restart

**Feature Branch**: `[006-bulb-reconnect-restart]`

**Created**: 2026-09-26

**Status**: Draft

**Input**: User description: "Bulbs should automatically reconnect on a server restart. At the moment, if the server restarts, the bulb does not reconnect automatically and one has to shut the power to the bulb and repower it so the bulb reconnects to the server. The shutdown needs to be somehow communicated to the bulb" (assessed via `.specify/assessments/automatic-reconnect-on-server-restart/`; verdict: go, chosen approach: close bulb connections in a way that mimics a power-cut so the bulb's own reconnect logic triggers, since bulb firmware is closed-source and cannot be modified)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Bulbs come back after a routine server restart (Priority: P1)

An operator restarts the haigosmartd server (deploy, config change, upgrade). Previously-connected bulbs reconnect and respond to commands again without the operator touching any bulb's power.

**Why this priority**: This is the entire point of the feature — it directly replaces today's only workaround (manually power-cycling every bulb after every restart), which is the reported pain.

**Independent Test**: Connect one or more bulbs to a running server, restart the server process, and verify each bulb resumes responding to on/off or color commands within a bounded time — without anyone touching the bulb's power.

**Acceptance Scenarios**:

1. **Given** a bulb is connected and controllable, **When** the operator restarts the server normally (graceful stop, e.g. `SIGTERM`/`SIGINT`), **Then** the bulb reconnects and is controllable again within the target reconnect window, with no manual power-cycle.
2. **Given** a bulb is connected and controllable, **When** the server process is stopped and a new one started in its place, **Then** the bulb is recognized under its existing name (not re-added as a new device) once reconnected.

---

### User Story 2 - Operator can tell whether the fix is working (Priority: P2)

Because the underlying bulb firmware is closed-source and its exact reconnect behavior was never independently verified before this feature shipped (see assessment evidence gap), the operator needs a way to see, after the fact, whether restarts are actually triggering bulb reconnects or not — so the open question isn't left unanswered indefinitely.

**Why this priority**: The assessment explicitly could not validate the fix's effect ahead of time (evidence strength was rated weak; the closed-source firmware's behavior can't be tested in isolation). Without visibility after shipping, the team has no way to learn whether the chosen approach actually works.

**Independent Test**: Restart the server with bulbs connected, then check the server's own records/logs to confirm whether each bulb reconnected and how long it took, without relying on manually watching each bulb.

**Acceptance Scenarios**:

1. **Given** the server has just restarted, **When** an operator checks the server's event history, **Then** they can see, per bulb, whether and when it reconnected after the restart.
2. **Given** a bulb fails to reconnect automatically after a restart, **When** an operator checks the server's event history, **Then** the record clearly shows the bulb as still disconnected (not silently missing).

---

### Edge Cases

- What happens when a bulb was already disconnected (not just server-restart-affected) before the restart — it must not be reported as a "failed reconnect" it never attempted.
- What happens when a restart happens during an in-progress command to a bulb — the bulb's prior state (on/off, color) should not be lost or corrupted by the disconnect/reconnect cycle.
- What happens when a bulb never reconnects at all after a restart (e.g. firmware truly doesn't retry) — the operator-visible record must distinguish "still trying" from "gave up," rather than leaving the bulb's status ambiguous forever.
- What happens on repeated/rapid restarts (e.g. a crash loop) — reconnect tracking must not accumulate stale or duplicate history per bulb.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST close each bulb's connection on server shutdown in a manner intended to trigger the bulb's own existing automatic-reconnect behavior, rather than leaving the bulb in a state where it waits indefinitely or requires manual power-cycling.
- **FR-002**: System MUST recognize a bulb that reconnects after a restart as the same previously-known device (same name/identity), not as a new, unnamed device requiring re-setup.
- **FR-003**: System MUST record, per bulb, whether and when it reconnected following a server restart, so this can be reviewed after the fact.
- **FR-004**: System MUST make a bulb's reconnect status after a restart distinguishable from a bulb that was already disconnected for unrelated reasons.
- **FR-005**: System MUST NOT lose or corrupt a bulb's last-known state (e.g., on/off, brightness, color) purely as a result of the restart-driven disconnect/reconnect cycle.
- **FR-006**: System MUST behave the same way across all supported deployment platforms (Windows, Linux, macOS) — the reconnect-triggering shutdown behavior is not platform-specific and applies uniformly to every supported build target.

### Key Entities

- **Bulb connection record**: Represents one bulb's link to the server — includes its known identity/name (already tracked today) plus, newly, its connection state transitions around a restart (disconnected-at, reconnected-at) so reconnect behavior is observable after the fact.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a server restart, previously-connected bulbs resume responding to commands without any manual power-cycling, in the large majority of cases.
- **SC-002**: An operator can determine, from the server's own records, within a minute of a restart, which bulbs reconnected and which did not — without inspecting any individual bulb by hand.
- **SC-003**: A bulb that reconnects after a restart retains its existing name/identity 100% of the time (never reappears as a duplicate or unnamed device).
- **SC-004**: Reports of "bulb unreachable after restart" from operators decrease compared to before this feature shipped.

## Assumptions

- Bulb firmware is closed-source and cannot be modified; this feature works only by changing how the server itself ends the connection, on the theory (assessed but not pre-verified — see `.specify/assessments/automatic-reconnect-on-server-restart/decision.md`) that firmware already knows how to recover from an abrupt connection loss (as it does for a power cut) but may not recover the same way from today's graceful shutdown.
- Because the underlying evidence gap could not be closed before building (the assessment's evidence strength was rated weak, and further pre-validation was confirmed not achievable), this feature explicitly includes its own after-the-fact observability (User Story 2 / FR-003 / FR-004) as the substitute validation mechanism — success is confirmed by watching real restarts, not by a pre-ship test.
- "Reconnect within a bounded/target window" reuses the existing informal expectation already documented for this project (roughly on the order of a minute) rather than introducing a new SLA; exact timing is not user-configurable in this feature.
- Existing bulb naming/registry persistence across restarts already works and is not being changed by this feature — only the connection-close behavior and reconnect visibility are new.
