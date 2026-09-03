# Feature Specification: Bubble Tea v2 Dependency Upgrade

**Feature Branch**: `005-bubbletea-v2-upgrade`

**Created**: 2026-09-03

**Status**: Draft

**Input**: User description: "I want to update bubbletea, bubbles and lipgloss to the major v2 version in order to keep receiving updates and using the latest available software."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Operator runs the TUI without regressions (Priority: P1)

An operator launches `haigosmartd`'s terminal UI (`internal/tui`) to monitor and control bulbs, exactly as they did before the upgrade. All screens, keybindings, colors, and layout behave identically to the pre-upgrade version.

**Why this priority**: The TUI is the primary user-facing surface built on these libraries. Any visible regression (broken layout, dead keybinding, crash) directly breaks the product's usability — this is the core risk the upgrade introduces.

**Independent Test**: Build `haigosmartd`, run the TUI in a terminal, and walk through every screen/state it exposes, confirming rendering and input handling match documented/expected behavior.

**Acceptance Scenarios**:

1. **Given** the TUI is built against the new dependency versions, **When** an operator starts it in a standard terminal, **Then** the initial view renders with the same layout and styling as before the upgrade.
2. **Given** the TUI is running, **When** an operator uses existing keybindings to navigate/act, **Then** each keybinding produces the same result it did before the upgrade.
3. **Given** the TUI is running in a resized or non-standard terminal, **When** the terminal is resized, **Then** the TUI adapts without crashing or corrupting output.

---

### User Story 2 - Maintainer keeps a clean, passing build (Priority: P2)

A maintainer pulls the upgraded code, and the project builds, lints, and passes its full test suite without needing to know the upgrade happened.

**Why this priority**: The constitution requires `gofmt`/`go vet`/lint and a green `go test ./... -race` on every PR; a major dependency bump that breaks these blocks all other work on the repo.

**Independent Test**: Run `go build ./...`, `go vet ./...`, and `go test ./... -race` on the upgraded branch; all MUST succeed.

**Acceptance Scenarios**:

1. **Given** the dependencies are upgraded to their v2 major versions, **When** the maintainer runs the standard build/lint/test commands, **Then** all commands complete successfully with no new failures.
2. **Given** existing TUI unit tests (`internal/tui/tui_test.go`), **When** they run against the upgraded libraries, **Then** they pass without being weakened or skipped to accommodate the upgrade.

---

### User Story 3 - Project stays on a supported, current dependency line (Priority: P3)

The project's `go.mod` reflects the current major versions of `bubbletea`, `bubbles`, and `lipgloss`, so future security patches and feature updates from upstream continue to apply cleanly.

**Why this priority**: This is the underlying motivation for the request (staying current), but it has no independent user-facing value until Stories 1 and 2 are satisfied — it's a bookkeeping/maintenance outcome, not a behavior change.

**Independent Test**: Inspect `go.mod`/`go.sum` and confirm all three libraries are pinned to their v2 major-version module paths with no lingering v1 references anywhere in the module graph.

**Acceptance Scenarios**:

1. **Given** the upgrade is complete, **When** a maintainer inspects `go.mod`, **Then** `bubbletea`, `bubbles`, and `lipgloss` all resolve to their v2 major versions.
2. **Given** the upgrade is complete, **When** a maintainer searches the codebase for the old v1 import paths, **Then** no references remain.

### Edge Cases

- What happens if the v2 APIs remove or rename a feature the TUI currently relies on (e.g., a specific message type, style method, or component)? The equivalent v2 behavior MUST be identified and substituted so user-visible behavior is unchanged.
- How does the system handle terminals with limited color support (e.g., no true-color) after the upgrade, given v2's color-handling changes? Output MUST remain readable and MUST NOT crash the program.
- What happens to any custom styling or components in `internal/tui` that depended on v1-specific behavior not preserved in v2? These MUST be adapted so the visual/interactive result matches pre-upgrade behavior, or any intentional difference MUST be called out and approved.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The project's dependency graph MUST reference the v2 major versions of `bubbletea`, `bubbles`, and `lipgloss`, with no remaining v1 imports anywhere in the codebase.
- **FR-002**: The terminal UI in `internal/tui` MUST continue to render all existing screens/views with unchanged layout, styling, and content after the upgrade.
- **FR-003**: The terminal UI MUST continue to respond to all existing keybindings and user interactions with the same outcomes as before the upgrade.
- **FR-004**: All existing automated tests covering the TUI and any other affected code MUST continue to pass after the upgrade, without weakening assertions or skipping tests to work around the change.
- **FR-005**: The project MUST continue to build cleanly (`go build`, `go vet`) and pass lint checks after the upgrade.
- **FR-006**: Any behavior change forced by the v2 API redesign (message types, command signatures, styling API, etc.) that is user-visible MUST be documented as an intentional deviation rather than silently shipped.

### Key Entities

- **Dependency Declaration**: The `go.mod`/`go.sum` entries pinning `bubbletea`, `bubbles`, and `lipgloss` to specific versions; after this change, all three point at their v2 major-version module paths.
- **TUI Application**: The `internal/tui` package (model, view, and associated tests) that consumes these libraries and whose behavior is the primary thing being preserved across the upgrade.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of existing TUI screens and keybindings behave identically (same visible output, same action taken) before and after the upgrade, verified by manual walkthrough.
- **SC-002**: `go build ./...`, `go vet ./...`, and `go test ./... -race` all pass with zero failures after the upgrade, matching pre-upgrade pass rate.
- **SC-003**: Zero references to the v1 import paths of `bubbletea`, `bubbles`, or `lipgloss` remain in the codebase after the upgrade.
- **SC-004**: Any unavoidable user-visible behavior change introduced by the v2 redesign is documented in the change's notes, with zero undocumented visible differences.

## Assumptions

- "Major v2 version" refers to the current v2 major-version releases of `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/bubbles`, and `github.com/charmbracelet/lipgloss` (currently on v1.x in this project).
- This is a dependency/maintenance upgrade, not a redesign: the goal is behavior parity in the TUI, not new features or a changed look-and-feel.
- The affected surface is limited to `internal/tui` (model.go, view.go, tui_test.go) and `cmd/haigosmartd/main.go`, based on current usage of these libraries in the codebase.
- Standard terminal environments (as already supported today) remain the target; no new terminal compatibility requirements are introduced by this upgrade.
- No end-user-facing documentation beyond in-repo change notes is expected, since the TUI's behavior is intended to remain unchanged.
- **Verification note**: The sandboxed environment this upgrade was implemented in has no `/dev/tty`, so the interactive walkthrough in `quickstart.md` section 3 could not be run directly (`bubbletea: could not open TTY`). Behavior parity for SC-001 was instead verified through the existing `internal/tui/tui_test.go` suite (unchanged assertions, only message-construction syntax updated for v2), which already exercises submit/echo, unknown-command guidance, history navigation, scrolling, resize, and both quit paths (ctrl+c, esc) against the real `Model.Update`/`View` code. A human with a real terminal should still run the manual walkthrough once before merge.
