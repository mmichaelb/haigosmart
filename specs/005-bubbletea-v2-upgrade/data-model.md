# Data Model: Bubble Tea v2 Dependency Upgrade

No new data entities. This is a dependency-version migration with no new persisted
state, no new domain concepts, and no schema change.

## Existing entities touched (unchanged shape, changed underlying types)

- **`Model`** (`internal/tui/model.go`): unchanged fields; its `View()` method's
  return type changes from `string` to `tea.View` (v2 requirement).
- **`eventMsg` / `resultMsg`**: unchanged; these are project-defined message types,
  not part of the bubbletea API surface being migrated.

## Dependency declaration (the actual "entity" of this feature)

| Field | Before | After |
|---|---|---|
| bubbletea module path | `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| bubbletea version | v1.3.10 | latest stable v2.x |
| bubbles module path | `github.com/charmbracelet/bubbles` | `charm.land/bubbles/v2` |
| bubbles version | v1.0.0 | latest stable v2.x |
| lipgloss module path | `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| lipgloss version | v1.1.0 | latest stable v2.x |

Recorded in `go.mod`/`go.sum` only; no runtime data model change.
