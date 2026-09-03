---

description: "Task list template for feature implementation"
---

# Tasks: Bubble Tea v2 Dependency Upgrade

**Input**: Design documents from `/specs/005-bubbletea-v2-upgrade/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: No new tests requested (FR-004 requires existing tests to keep passing,
unweakened — no new test tasks are generated, only adaptation of existing ones).

**Organization**: Tasks are grouped by user story per spec.md priorities (P1/P2/P3).
This migration is code-shared across all three files that import the libraries, so
the compile-fix work sits in Foundational (nothing is testable pre-compile); each
story phase is then its own independent verification pass.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)

## Phase 1: Setup

**Purpose**: Pin the new major versions

- [X] T001 Bump dependencies to their v2 major versions: `go get charm.land/bubbletea/v2@latest charm.land/bubbles/v2@latest charm.land/lipgloss/v2@latest && go mod tidy`, updating `go.mod` and `go.sum` at the repo root

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Make the codebase compile against v2. No user story can be verified
until this phase is done — the package won't build otherwise.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T002 Update import paths from `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` to `charm.land/{bubbletea,bubbles,lipgloss}/v2` in `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/tui_test.go`, and `cmd/haigosmartd/main.go`
- [X] T003 [P] Change `Model.View()` from `View() string` to `View() tea.View` in `internal/tui/view.go`, wrapping the existing rendered string (per research.md decision on the v2 View interface)
- [X] T004 [P] Move the `tea.WithAltScreen()` program option to the declarative `View.AltScreen = true` field in `cmd/haigosmartd/main.go` (`tea.NewProgram` call at line ~193), per research.md
- [X] T005 Rewrite key handling in `internal/tui/model.go` (`Update` and `handleKey`, lines ~121-163) from `tea.KeyMsg` struct fields (`.Type`, `.Runes`) to v2's `tea.KeyPressMsg` interface (`.Code`, `.Text`, `.Mod`), preserving every existing case: ctrl+c/esc quit, enter submit, up/down history, pgup/pgdn scroll
- [X] T006 Update `tea.KeyMsg{Type: ..., Runes: ...}` literals in `internal/tui/tui_test.go` to the v2 `tea.KeyPressMsg` construction matching T005's field names, without changing any test assertions
- [X] T007 Run `go build ./...`, fix any remaining v2 API breaks the compiler surfaces in `internal/tui/model.go` or `internal/tui/view.go` (e.g. `bubbles/textinput` or `lipgloss` methods that changed), per research.md's compile-driven verification approach

**Checkpoint**: `go build ./...` succeeds — user story verification can begin

---

## Phase 3: User Story 1 - Operator runs the TUI without regressions (Priority: P1) 🎯 MVP

**Goal**: Confirm the TUI's rendering and every keybinding behave exactly as before the upgrade

**Independent Test**: Run `go run ./cmd/haigosmartd`, walk every screen/state, compare against pre-upgrade behavior

- [X] T008 [US1] Execute the manual TUI walkthrough in `specs/005-bubbletea-v2-upgrade/quickstart.md` section 3 (initial render, submit, unknown command, history, scrolling, resize, ctrl+c quit, esc quit); fix any regression found in `internal/tui/model.go` or `internal/tui/view.go`, or document it as an intentional deviation per FR-006 in `specs/005-bubbletea-v2-upgrade/spec.md`

**Checkpoint**: TUI behavior verified identical to pre-upgrade (SC-001)

---

## Phase 4: User Story 2 - Maintainer keeps a clean, passing build (Priority: P2)

**Goal**: Standard build/lint/test commands stay green

**Independent Test**: `go build ./...`, `go vet ./...`, `go test ./... -race` all succeed

- [X] T009 [US2] Run `go vet ./...` and `go test ./... -race`, fix any failure or flagged issue without weakening existing assertions in `internal/tui/tui_test.go`

**Checkpoint**: Full build/vet/test-race gate green (SC-002)

---

## Phase 5: User Story 3 - Project stays on a supported, current dependency line (Priority: P3)

**Goal**: `go.mod` cleanly reflects v2, with no v1 remnants anywhere

**Independent Test**: Inspect `go.mod` and grep the codebase for old v1 import paths

- [X] T010 [US3] Verify `go.mod` require lines list only the v2 module paths (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`) and run `grep -rn "charmbracelet/bubbletea\"\|charmbracelet/bubbles\"\|charmbracelet/lipgloss\"" --include="*.go" .` to confirm zero matches, per `specs/005-bubbletea-v2-upgrade/quickstart.md` section 2

**Checkpoint**: Zero v1 references remain (SC-003)

---

## Phase 6: Polish

**Purpose**: Final cross-cutting confirmation

- [X] T011 Run the full `specs/005-bubbletea-v2-upgrade/quickstart.md` end-to-end (sections 1-3) as a last confirmation before merge; ensure any deviation logged in T008 is reflected in the spec's Assumptions or a PR migration note per constitution Principle III

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on T001 — BLOCKS all user stories (nothing compiles until it's done)
- **User Stories (Phase 3-5)**: All depend on Foundational (Phase 2) completion; independent of each other, can run in any order or in parallel
- **Polish (Phase 6)**: Depends on Phases 3-5 being complete

### Within Foundational

- T002 (import paths) must land before T003, T004, T005 (they reference the new package)
- T003, T004, T005 touch different files (`view.go`, `main.go`, `model.go`) — parallelizable after T002
- T006 depends on T005 (must mirror model.go's chosen field names)
- T007 (compile-fix pass) runs last, after T002-T006

### Parallel Opportunities

- T003 and T004 can run in parallel (different files, no shared dependency beyond T002)
- Once Phase 2's checkpoint (`go build ./...` succeeds) is reached, T008, T009, T010 can run in parallel — they touch different verification surfaces (manual walkthrough, automated tests, dependency grep) with no file conflicts

---

## Parallel Example: Foundational Phase

```bash
# After T002 (import paths) lands:
Task: "Change Model.View() to View() tea.View in internal/tui/view.go"
Task: "Move tea.WithAltScreen() to View.AltScreen in cmd/haigosmartd/main.go"
```

## Parallel Example: User Story Verification

```bash
# After Phase 2 checkpoint (go build ./... succeeds):
Task: "Manual TUI walkthrough per quickstart.md section 3"
Task: "Run go vet ./... && go test ./... -race"
Task: "Grep for zero v1 import paths per quickstart.md section 2"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001)
2. Complete Phase 2: Foundational (T002-T007) — CRITICAL, blocks everything
3. Complete Phase 3: User Story 1 (T008) — TUI behavior parity confirmed
4. **STOP and VALIDATE**: this alone proves the upgrade didn't break the product's primary surface

### Incremental Delivery

1. Setup + Foundational → codebase compiles against v2
2. US1 (T008) → TUI parity confirmed → this is the MVP signal
3. US2 (T009) → build/test gate green → safe to open a PR
4. US3 (T010) → dependency hygiene confirmed → nothing left half-migrated
5. Polish (T011) → final end-to-end confirmation → ready to merge

## Notes

- [P] tasks touch different files with no unmet dependency
- Every task in Phase 2 is a compile-correctness step; verify with `go build ./...` after each before moving on, rather than batching all edits blind
- Commit after each task or logical group, per repo convention
- Avoid speculative rewrites: only change `bubbles`/`lipgloss` call sites the compiler actually flags (T007), per research.md
