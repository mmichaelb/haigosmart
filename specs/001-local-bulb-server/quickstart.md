# Quickstart & Validation: Local Replacement Server for Aigo Smart Bulbs

**Feature**: 001-local-bulb-server | **Date**: 2026-08-27

Two halves. Part A is the capture you run **now**, before any protocol code exists —
it unblocks gate G1. Part B is how the finished server is validated against the spec's
success criteria.

---

## Part A — produce the capture (do this first)

**Prerequisites**: mitmproxy installed; ability to redirect the bulb's traffic (you have
this); mitmproxy's CA installed on the phone; at least one Aigo bulb, ideally two.

1. Read [research.md](./research.md) §1 and start both mitmdump processes.
2. Start the packet capture in §1.5 as insurance.
3. Work through the interaction sequence in [research.md](./research.md) §2 **in order,
   with the pauses**, logging wall-clock time per step. About 15 minutes.
4. Stop the captures and generate the readable dumps (§1.4).
5. Confirm what you have:

```bash
ls -la captures/
wc -l captures/bulb.jsonl captures/app.jsonl
head -3 captures/bulb.jsonl
grep -o '"server":"[^"]*"' captures/bulb.jsonl | sort -u   # the hostnames to redirect later
```

**Success looks like**: `bulb.jsonl` is non-empty; at least one message appears within
seconds of capture step 1 (bulb power-on); distinct messages line up with steps 4–11;
periodic identical small messages appear during step 14 (the keep-alive).

**If `bulb.jsonl` is empty but the bulb worked**: the redirect did not take, or TLS
interception failed. Check `bulb.pcap` — if it shows a TLS handshake that the bulb aborted,
the firmware pins its certificate. Go to [research.md](./research.md) §4.

6. Hand back `captures/bulb.jsonl`, `captures/app.jsonl`, and your step-time log. Gate G1
   is the analysis of those files into
   [contracts/bulb-protocol.md](./contracts/bulb-protocol.md).

---

## Part B — validating the built server

### Build and run

```bash
go build ./cmd/haigosmartd
./haigosmartd -listen :<protocol-port>
```

Point the bulbs' cloud hostname (from step 5 above) at this machine, then power-cycle a bulb.

### Validation scenarios

Each maps to a spec success criterion or requirement. Run in order; each is independently meaningful.

| # | Scenario | Steps | Expected | Covers |
|---|---|---|---|---|
| 1 | Bulb registers | Power on a bulb with the server running | It appears in `list` within 30 s, status `discovered`, then `connected` after `name` | SC-002, FR-001, FR-017 |
| 2 | No cloud traffic | `sudo tcpdump -i <iface> host <bulb-ip> and not net <lan>` for 10 min | Zero packets | SC-001, FR-002 |
| 3 | Control round trip | `on kitchen`, `off kitchen`, `bri kitchen 50`, `color kitchen #ff0000` | Bulb visibly changes within 1 s of each; prompt echoes `ok` | SC-003, FR-008 to FR-010 |
| 4 | External change is seen | Toggle the bulb at the wall switch | A `power on→off` event appears in the feed within 5 s, unprompted | SC-004, FR-013, FR-019 |
| 5 | Error shapes | `on nosuchbulb`, `bri kitchen 500`, `dim kitchen` | Three errors in the documented shape; no bulb changes | FR-016, FR-012, Constitution III |
| 6 | Restart persistence | `quit`, restart, wait | Named bulbs reappear with their names and reconnect within 60 s, no re-naming | SC-006, FR-004, FR-005 |
| 7 | Reconnect | Cut power to a bulb, restore it | `disconnected` then `connected` events; same registry entry, not a duplicate | FR-005, FR-014 |
| 8 | Soak | 30 fakebulb instances (or real bulbs) for 7 days | No unexplained disconnections; RSS stable | SC-005, FR-020 |
| 9 | Event completeness | Drive 500 state changes through fakebulb, count events in the log | 500 logged, correctly attributed; the not-shown count in the status bar exactly matches what the feed omitted | SC-008 |
| 9a | Feed never blocks control | Burst events at the TUI while typing a command | Keystrokes accepted throughout; the command dispatches without perceptible delay | SC-009 |
| 11 | Adoption | Power on an unknown bulb, try `on <id>`, then `name <id> kitchen`, then `on kitchen` | First refused with the adoption hint; `name` reports `adopted`; control then works and survives a restart | FR-011, FR-017 |
| 12 | Capabilities | Run `info` on a colour bulb and on a white-only bulb; send `color` to each | `info` shows the correct capabilities; colour succeeds on one and reports `does not support colour` on the other | FR-010 |
| 10 | Cold usability | Hand the running TUI to someone who has not seen it | They list bulbs and change one within 2 minutes using `help` alone | SC-007 |

### Without hardware

Scenarios 3, 5, 7, 8, 9, 9a, 11, and 12 run entirely against `internal/bulb/fakebulb`:

```bash
go test ./... -race
go test ./internal/server -run TestSoak -count=1 -timeout 30m   # scenario 8, shortened
```

Scenarios 1, 2, 4, 6, and 10 need a physical bulb and an operator.

### Definition of done for this feature

- All fourteen scenarios pass.
- `go test ./... -race` green; `gofmt -l .` empty; `go vet ./...` clean.
- [contracts/bulb-protocol.md](./contracts/bulb-protocol.md) is fully filled in, with
  fixtures under `internal/protocol/testdata/`.
- `docs/` explains the redirect setup so the operator can rebuild it from scratch.
