# Research: Bubble Tea v2 Dependency Upgrade

## Decision: Target versions and import paths

- **Decision**: Move `bubbletea`, `bubbles`, `lipgloss` to their v2 major versions under the new module path root `charm.land/*/v2` (upstream moved off `github.com/charmbracelet/*` for v2). Use `go get` to pin the latest stable v2 tag of each at implementation time (lipgloss v2 has stable tags through at least v2.0.6; bubbletea/bubbles v2 availability confirmed via alpha/stable tags — pin whatever `go get charm.land/bubbletea/v2@latest` resolves to when the upgrade lands, not a hardcoded version here).
- **Rationale**: Matches the official upgrade path documented by upstream; staying on the new canonical module path avoids being stuck on an abandoned mirror.
- **Alternatives considered**: Pin to a specific alpha/beta tag — rejected, spec (SC-004, Assumptions) calls for the current stable major line, not a pre-release.

## Decision: `Model.View()` signature change

- **Decision**: Change `View() string` → `View() tea.View` in `internal/tui/view.go`. Build the same string as today, wrap it as `tea.NewView(s)` (or equivalent v2 constructor), and move `tea.WithAltScreen()` (currently a `tea.NewProgram` option in `cmd/haigosmartd/main.go:193`) onto `view.AltScreen = true` per the v2 declarative-View pattern.
- **Rationale**: This is the single required interface change for `tea.Model` compliance in v2; the guide is explicit that program options for terminal modes move to `View` struct fields.
- **Alternatives considered**: None — this is a mandatory interface change, not a style choice.

## Decision: Key message handling rewrite

- **Decision**: Rewrite `handleKey` and the type switch in `Update` (`internal/tui/model.go:121-163`) from `tea.KeyMsg` (struct, `.Type`/`.Runes`) to v2's `tea.KeyPressMsg` (`.Code`, `.Text`, `.Mod`). Map each existing case:
  - `tea.KeyCtrlC, tea.KeyEsc` → check `msg.String() == "ctrl+c"` / `"esc"` (or the v2 `Code`-based equivalent for ESC)
  - `tea.KeyEnter` → v2 enter code
  - `tea.KeyUp` / `tea.KeyDown` / `tea.KeyPgUp` / `tea.KeyPgDown` → v2 equivalents
  - Rune input (used implicitly by falling through to `m.input.Update(msg)`) continues to work since `textinput.Model.Update` handles its own key parsing once bubbles is on v2.
- **Rationale**: `tea.KeyMsg` is now an interface in v2; the concrete struct fields the code switches on no longer exist under those names. This is the highest-risk change for User Story 1 (keybinding parity) and User Story 2 (existing tests use the old struct literal `tea.KeyMsg{Type: tea.KeyRunes, ...}` in `tui_test.go`, which must be updated to compile).
- **Alternatives considered**: Keep a v1 compatibility shim — rejected; adds a permanent maintenance surface for a one-time migration, contradicts the "no lingering v1" success criterion (SC-003).

## Decision: `tea.NewProgram` / `Program.Run` call site

- **Decision**: `cmd/haigosmartd/main.go:193-195` already uses `program.Run()`, which is unchanged in v2 (`p.Start()` was the removed name, not `Run()`). Only the `tea.WithAltScreen()` option needs to move to the `View` struct as noted above.
- **Rationale**: Confirmed by reading the current call site — it's already on the v2-compatible method name.
- **Alternatives considered**: N/A.

## Decision: bubbles `textinput` v2 API

- **Decision**: `internal/tui/model.go` uses `textinput.New()`, `.Placeholder`, `.Prompt`, `.Focus()`, `.CharLimit`, `.Width`, `.Value()`, `.SetValue()`, `.CursorEnd()`, `.Update(msg)`, `.View()`, and `textinput.Blink`. Treat these as needing a compile-and-fix pass: bubbles v2 is expected to keep this surface largely intact (it's the most stable component), but confirm each symbol still exists once the dependency is pinned, and adjust only what the compiler flags.
- **Rationale**: No upstream changelog for `textinput` specifically was available offline; the safest approach is compile-driven verification rather than guessing an API that may not have changed.
- **Alternatives considered**: Pre-emptively rewrite all `textinput` usage — rejected, unnecessary churn if the API is unchanged (ponytail: don't fix what isn't broken).

## Decision: `lipgloss` v2 styling API (`internal/tui/view.go`)

- **Decision**: `lipgloss.NewStyle().Bold(true)`, `.Faint(true)`, `.Render(...)`, and `lipgloss.Width(...)` are core, stable lipgloss APIs predating v1 and are expected to carry over unchanged in v2 (the fetched v1→v2 guide covers bubbletea's message/program API, not a lipgloss styling rewrite). Verify by compiling after the version bump; fix only what breaks.
- **Rationale**: The migration guide's scope is bubbletea-centric; no evidence of a lipgloss styling-method rename surfaced. Confirms via compile rather than blind rewrite.
- **Alternatives considered**: None.

## Decision: Test suite migration (`internal/tui/tui_test.go`)

- **Decision**: Update every `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}` and `tea.KeyMsg{Type: tea.KeyEnter}` (etc.) literal to the v2 `tea.KeyPressMsg` equivalent, matching whatever field names `internal/tui/model.go` ends up using after its own migration. No test behavior/assertions change — only the message construction syntax.
- **Rationale**: FR-004 requires existing tests to keep passing unweakened; this is a mechanical adaptation to the new message type, not a test redesign.
- **Alternatives considered**: None.

## Decision: Verification strategy

- **Decision**: Verification is compile (`go build ./...`), `go vet ./...`, `go test ./... -race`, plus a manual TUI walkthrough (start `haigosmartd`, exercise every keybinding: type, enter, up/down history, pgup/pgdn scroll, ctrl+c/esc quit, resize) since Bubble Tea has no headless UI-testing harness in this project beyond the existing `Update`/`View` unit tests already in `tui_test.go`.
- **Rationale**: Matches constitution Principle II (race-enabled test gate) and spec SC-001/SC-002 exactly; no new tooling needed.
- **Alternatives considered**: Golden-file screenshot testing — rejected as new infrastructure disproportionate to a dependency-parity upgrade (ponytail: no new abstraction for a one-off migration).
