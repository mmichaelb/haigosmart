# Quickstart: Validating the Bubble Tea v2 Upgrade

## Prerequisites

- Go toolchain matching `go.mod` (currently 1.27)
- Terminal emulator to run the interactive TUI in

## 1. Automated checks

```sh
go build ./...
go vet ./...
go test ./... -race
```

All three MUST pass (SC-002). `go test ./internal/tui/... -race -v` isolates the
TUI package if a failure needs narrowing down.

## 2. Dependency check

```sh
go list -m github.com/charmbracelet/bubbletea github.com/charmbracelet/bubbles github.com/charmbracelet/lipgloss 2>&1
grep -rn "charmbracelet/bubbletea\"\|charmbracelet/bubbles\"\|charmbracelet/lipgloss\"" --include="*.go" .
```

First command should report "not a known dependency" (v1 paths gone from `go.mod`).
Second command MUST return no matches — confirms SC-003 (zero v1 import paths left).

## 3. Manual TUI walkthrough (SC-001)

Run the interactive mode (non-headless):

```sh
go run ./cmd/haigosmartd
```

Exercise every existing interaction and confirm it behaves as before the upgrade:

1. **Initial render**: status bar, separator rule, empty feed, prompt at bottom.
2. **Type and submit**: type `list`, press Enter — command echoes in the feed,
   answer appears, prompt clears.
3. **Unknown command**: type `dim kitchen`, press Enter — "unknown command" guidance
   shown.
4. **History**: submit two different commands, press Up twice (recalls both, oldest
   first), then Down (recalls the more recent one).
5. **Scrolling**: with enough feed lines to overflow the terminal, press PgUp/PgDn —
   feed scrolls, status bar shows "scrolled back" when applicable.
6. **Resize**: resize the terminal window — layout reflows, no corrupted lines, no
   crash.
7. **Quit**: press `ctrl+c` — process exits cleanly, registry save happens (check
   log file for the shutdown record).
8. Re-run and press `esc` instead — same clean-quit behavior.

Any deviation from pre-upgrade behavior found here must be logged per FR-006 (documented
intentional deviation) or fixed before merge.
