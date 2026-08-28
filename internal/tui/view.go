package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

var (
	statusStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.quitting {
		return "saved. bye\n"
	}
	var b strings.Builder
	b.WriteString(statusStyle.Render(m.statusBar()))
	b.WriteByte('\n')
	b.WriteString(dimStyle.Render(strings.Repeat("─", m.rule())))
	b.WriteByte('\n')

	visible := m.feed.visible()
	for _, line := range visible {
		b.WriteString(truncate(line, m.width))
		b.WriteByte('\n')
	}
	// Pad so the prompt stays pinned to the bottom instead of drifting upward
	// while the feed fills.
	for i := len(visible); i < m.visibleFeedLines(); i++ {
		b.WriteByte('\n')
	}
	b.WriteString(truncate(m.input.View(), m.width))
	return b.String()
}

func (m *Model) statusBar() string {
	var connected, discovered int
	all := m.reg.List()
	for _, x := range all {
		switch x.Status {
		case bulb.Connected:
			connected++
		case bulb.Discovered:
			discovered++
		}
	}
	parts := []string{
		"haigosmart",
		fmt.Sprintf("%d bulbs", len(all)),
		fmt.Sprintf("%d connected", connected),
	}
	if discovered > 0 {
		parts = append(parts, fmt.Sprintf("%d discovered", discovered))
	}
	// Events missing from the display are counted here rather than hidden. They
	// are all still in the log.
	if dropped := m.sub.Dropped(); dropped > 0 {
		parts = append(parts, fmt.Sprintf("… %d events dropped from view (all logged)", dropped))
	}
	if m.inFlight > 0 {
		parts = append(parts, fmt.Sprintf("%d command(s) in flight", m.inFlight))
	}
	if m.feed.scrolledBack() {
		parts = append(parts, "scrolled back — PgDn to return")
	}
	return truncate(strings.Join(parts, " · "), m.width)
}

// rule is the width of the separator line. It follows the terminal, never
// exceeding it: a fixed minimum would overflow a very narrow window.
func (m *Model) rule() int {
	if m.width <= 0 {
		return 10 // before the first WindowSizeMsg arrives
	}
	return m.width
}

// truncate keeps a line inside the terminal width so a narrow window reflows
// instead of corrupting the display. It counts display cells rather than bytes,
// so a multibyte character is never cut in half.
func truncate(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if width == 1 {
		return string(runes[:1])
	}
	out := make([]rune, 0, width)
	used := 0
	for _, r := range runes {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		out = append(out, r)
		used += w
	}
	return string(out) + "…"
}
