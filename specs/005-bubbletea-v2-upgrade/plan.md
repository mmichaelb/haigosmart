# Implementation Plan: Bubble Tea v2 Dependency Upgrade

**Branch**: `005-bubbletea-v2-upgrade` | **Date**: 2026-09-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-bubbletea-v2-upgrade/spec.md`

## Summary

Move `bubbletea`, `bubbles`, and `lipgloss` from their v1 major versions to their
v2 major versions (new module path `charm.land/*/v2`), preserving the TUI's exact
current behavior. The required code changes are: `Model.View() string` →
`View() tea.View`, moving `tea.WithAltScreen()` from a program option to a `View`
struct field, and rewriting the `tea.KeyMsg` struct-field key handling in
`internal/tui/model.go` (and its mirror in `internal/tui/tui_test.go`) to v2's
`tea.KeyPressMsg` interface (`.Code`/`.Text`/`.Mod` instead of `.Type`/`.Runes`).
`bubbles/textinput` and `lipgloss` styling calls are expected to be source-compatible
and are verified by compilation rather than rewritten speculatively.

## Technical Context

**Language/Version**: Go 1.27 (pinned in `go.mod`)

**Primary Dependencies**: `charm.land/bubbletea/v2` (was `github.com/charmbracelet/bubbletea` v1.3.10), `charm.land/bubbles/v2` (was `github.com/charmbracelet/bubbles` v1.0.0), `charm.land/lipgloss/v2` (was `github.com/charmbracelet/lipgloss` v1.1.0)

**Storage**: N/A (no data model change)

**Testing**: `go test ./... -race` (existing suite in `internal/tui/tui_test.go`, adapted to v2 message types, no new tests required — this is a compatibility migration)

**Target Platform**: Existing target: local/server terminal (Linux/macOS), unchanged by this upgrade

**Project Type**: Single Go module, CLI/TUI daemon (`cmd/haigosmartd`)

**Performance Goals**: N/A (not a merge gate per constitution v2.0.0; behavior parity is the goal, not new performance work)

**Constraints**: Zero visible behavior change in the TUI (FR-002, FR-003); zero remaining v1 import paths (FR-001, SC-003)

**Scale/Scope**: 3 files touch the libraries directly: `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/tui_test.go`, plus `cmd/haigosmartd/main.go` (one `tea.NewProgram` call site) and `go.mod`/`go.sum`

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Code Quality**: No new abstractions introduced; existing doc comments on
  touched exported symbols (`Model`, `New`, `Init`, `Update`) are preserved as-is
  since their behavior doesn't change. `gofmt`/`go vet`/lint remain required gates
  — satisfied by the plan's verification step. PASS.
- **II. Testing Standards**: `internal/tui/tui_test.go` already covers the TUI's
  behavior (submission, history, scrolling, resize, quit paths, dropped events);
  this migration adapts those tests' message construction to v2 without weakening
  assertions, and `go test ./... -race` stays the merge gate. No new exported
  behavior is being added, so no new tests are required beyond keeping existing
  ones green. PASS.
- **III. User Experience Consistency**: The explicit goal (spec FR-002/FR-003, SC-001)
  is zero user-visible change; if v2 forces an unavoidable visible difference, FR-006
  requires it to be documented as an intentional deviation with a migration note,
  matching the constitution's breaking-change documentation requirement. PASS.
- **Additional Constraints**: `go.mod` stays on Go 1.27 (no toolchain bump needed for
  these libraries); the version bump itself is the one-line justification required
  for a dependency change ("upgrading existing deps to their current major version
  for continued upstream support," not a new dependency). PASS.

No violations. Complexity Tracking section is not needed.

## Project Structure

### Documentation (this feature)

```text
specs/005-bubbletea-v2-upgrade/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md         # Phase 1 output
├── quickstart.md         # Phase 1 output
└── tasks.md              # Phase 2 output (/speckit-tasks — not created here)
```

No `contracts/` directory: this feature has no external interface (API, CLI schema,
wire protocol) change — it's an internal dependency swap behind the existing TUI
surface, which itself is not a documented external contract.

### Source Code (repository root)

```text
go.mod                        # bubbletea/bubbles/lipgloss require lines → v2
go.sum                        # regenerated

cmd/haigosmartd/
└── main.go                   # tea.NewProgram(model, tea.WithAltScreen()) call site

internal/tui/
├── model.go                  # tea.KeyMsg struct usage → tea.KeyPressMsg
├── view.go                   # View() string → View() tea.View
└── tui_test.go                # tea.KeyMsg{...} literals → v2 equivalents
```

**Structure Decision**: Existing single-module Go project layout is unchanged.
Every file that needs to change is already identified from the current import graph
(`grep` of the three library import paths found exactly these four `.go` files);
no new packages or directories are introduced.

## Complexity Tracking

*No violations — table not needed.*
