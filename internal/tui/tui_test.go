package tui

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"haigosmart/internal/bulb"
	"haigosmart/internal/control"
	"haigosmart/internal/events"
	"haigosmart/internal/lights"
	"haigosmart/internal/registry"
)

func newModel(t *testing.T) (*Model, *registry.Registry, *events.Bus) {
	t.Helper()
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	sub := bus.Subscribe(64)
	t.Cleanup(sub.Close)
	m := New(control.New(lights.New(reg, bus), reg), reg, sub, func() {})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, reg, bus
}

// typeLine types a line, submits it, and then pumps the command tea.Cmd the way
// the Bubble Tea runtime would. Commands run off the update loop now, so a test
// that only pressed Enter would never see the result.
func typeLine(m *Model, line string) *Model {
	for _, r := range line {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	next, _ = m.Update(msg)
	return next.(*Model)
}

func TestSubmitEchoesAndAnswers(t *testing.T) {
	m, _, _ := newModel(t)
	m = typeLine(m, "list")
	view := m.View()
	if !strings.Contains(view, "> list") {
		t.Error("the submitted command should be echoed in the feed")
	}
	if !strings.Contains(view, "no bulbs yet") {
		t.Errorf("expected the empty-registry answer, got:\n%s", view)
	}
	if m.input.Value() != "" {
		t.Error("the prompt should clear after submitting")
	}
}

func TestUnknownCommandShowsGuidance(t *testing.T) {
	m, _, _ := newModel(t)
	m = typeLine(m, "dim kitchen")
	if !strings.Contains(m.View(), "unknown command") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestHistoryNavigation(t *testing.T) {
	m, _, _ := newModel(t)
	m = typeLine(m, "list")
	m = typeLine(m, "help")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(*Model)
	if m.input.Value() != "help" {
		t.Errorf("first up = %q, want help", m.input.Value())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(*Model)
	if m.input.Value() != "list" {
		t.Errorf("second up = %q, want list", m.input.Value())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	if m.input.Value() != "help" {
		t.Errorf("down = %q, want help", m.input.Value())
	}
}

func TestHistoryOnEmptyHistoryIsHarmless(t *testing.T) {
	m, _, _ := newModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(*Model).input.Value(); got != "" {
		t.Errorf("input = %q, want empty", got)
	}
}

func TestEventsAppearInTheFeed(t *testing.T) {
	m, _, _ := newModel(t)
	e := events.Event{
		At: time.Now(), Kind: events.StateChanged, Name: "kitchen",
		Changed: []bulb.FieldChange{{Field: "power", From: "off", To: "on"}},
	}
	next, _ := m.Update(eventMsg(e))
	m = next.(*Model)
	view := m.View()
	if !strings.Contains(view, "kitchen") || !strings.Contains(view, "power off→on") {
		t.Errorf("view:\n%s", view)
	}
}

func TestResizeKeepsViewCoherent(t *testing.T) {
	m, _, _ := newModel(t)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 200, Height: 60}, {Width: 40, Height: 10},
		{Width: 20, Height: 4}, {Width: 10, Height: 3}, {Width: 1, Height: 1},
	} {
		next, _ := m.Update(size)
		m = next.(*Model)
		m.Update(eventMsg(events.Event{At: time.Now(), Kind: events.Connected, Name: "a-very-long-bulb-name-here"}))
		view := m.View()
		for _, line := range strings.Split(view, "\n") {
			// Display cells, not bytes: a box-drawing character is three bytes
			// wide and one cell wide, and it is cells that corrupt a display.
			if w := lipgloss.Width(line); w > size.Width {
				t.Errorf("at width %d a line occupies %d cells: %q", size.Width, w, line)
			}
		}
	}
}

func TestFeedEvictsOldestAndCounts(t *testing.T) {
	f := newFeed(10)
	f.resize(5)
	for i := range 100 {
		f.add(string(rune('a'+i%26)) + "-line")
	}
	if got := len(f.lines); got != 10 {
		t.Errorf("feed holds %d lines, want its capacity of 10", got)
	}
	if f.evicted != 90 {
		t.Errorf("evicted = %d, want 90", f.evicted)
	}
	if got := len(f.visible()); got != 5 {
		t.Errorf("visible = %d lines, want the 5 that fit", got)
	}
}

func TestFeedScrolling(t *testing.T) {
	f := newFeed(100)
	f.resize(5)
	for i := range 20 {
		f.add(string(rune('a' + i)))
	}
	if f.scrolledBack() {
		t.Error("a fresh feed is not scrolled back")
	}
	newest := f.visible()
	if newest[len(newest)-1] != "t" {
		t.Errorf("newest line = %q, want the last one added", newest[len(newest)-1])
	}

	f.scroll(-5) // page back
	if !f.scrolledBack() {
		t.Error("scrolling back should be reported so the operator knows")
	}
	back := f.visible()
	if back[len(back)-1] == newest[len(newest)-1] {
		t.Error("scrolling back did not move the view")
	}

	f.scroll(100) // forward past the end
	if f.scrolledBack() {
		t.Error("scrolling forward past the end should return to the newest")
	}
	// Scrolling back past the beginning must not panic or go negative.
	f.scroll(-1000)
	if f.offset < 0 {
		t.Errorf("offset = %d, want it clamped at or above zero", f.offset)
	}
	_ = f.visible()
}

func TestStatusBarReportsDroppedEvents(t *testing.T) {
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	sub := bus.Subscribe(1) // deliberately tiny
	defer sub.Close()
	m := New(control.New(lights.New(reg, bus), reg), reg, sub, func() {})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	// Nothing reads the subscription, so the queue overflows.
	for range 50 {
		bus.Publish(events.Event{At: time.Now(), Kind: events.Connected, Name: "kitchen"})
	}
	if sub.Dropped() == 0 {
		t.Fatal("expected the display queue to drop events")
	}
	if !strings.Contains(m.statusBar(), "dropped from view (all logged)") {
		t.Errorf("the status bar must surface drops, got %q", m.statusBar())
	}
}

func TestStatusBarCountsBulbs(t *testing.T) {
	m, reg, _ := newModel(t)
	reg.Upsert("aaa", "addr", bulb.Capabilities{Known: true}, time.Now())
	b, _ := reg.Upsert("bbb", "addr", bulb.Capabilities{Known: true}, time.Now())
	if _, err := reg.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	bar := m.statusBar()
	if !strings.Contains(bar, "2 bulbs") || !strings.Contains(bar, "1 connected") || !strings.Contains(bar, "1 discovered") {
		t.Errorf("status bar = %q", bar)
	}
}

func TestQuitCancelsTheServer(t *testing.T) {
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	sub := bus.Subscribe(8)
	defer sub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m := New(control.New(lights.New(reg, bus), reg), reg, sub, cancel)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	typeLine(m, "quit")
	select {
	case <-ctx.Done():
	default:
		t.Error("quitting must shut the server down, not just close the display")
	}
}

func TestCtrlCQuits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	sub := bus.Subscribe(8)
	defer sub.Close()
	m := New(control.New(lights.New(reg, bus), reg), reg, sub, cancel)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case <-ctx.Done():
	default:
		t.Error("ctrl+c must shut down cleanly rather than killing the process mid-write")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 0, "hello"},
		{"hello", 1, "h"},
	}
	for _, tc := range tests {
		if got := truncate(tc.in, tc.width); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

// A command that waits on hardware must not freeze the interface. Bulbs that
// fade take seconds to confirm, so the prompt and the event feed have to keep
// working while one is outstanding (FR-015).
func TestSlowCommandDoesNotBlockTheInterface(t *testing.T) {
	m, _, bus := newModel(t)

	// Submit without pumping the command, leaving it in flight.
	for _, r := range "on kitchen" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)
	if cmd == nil {
		t.Fatal("submitting should hand the command off to the runtime")
	}
	if m.inFlight != 1 {
		t.Errorf("inFlight = %d, want 1", m.inFlight)
	}
	if !strings.Contains(m.statusBar(), "in flight") {
		t.Error("the status bar should show that something is happening")
	}

	// The operator can keep typing while it runs.
	for _, r := range "list" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(*Model)
	}
	if m.input.Value() != "list" {
		t.Errorf("input = %q; keystrokes were dropped while a command was in flight", m.input.Value())
	}

	// Events keep arriving and rendering.
	bus.Publish(events.Event{At: time.Now(), Kind: events.Connected, Name: "kitchen"})
	next, _ = m.Update(eventMsg(events.Event{At: time.Now(), Kind: events.Connected, Name: "kitchen"}))
	m = next.(*Model)
	if !strings.Contains(m.View(), "connected") {
		t.Error("the feed stopped updating while a command was in flight")
	}

	// When the result finally lands, the counter clears.
	next, _ = m.Update(resultMsg{line: "on kitchen", result: control.Result{Kind: control.ResultError, Text: "boom"}})
	m = next.(*Model)
	if m.inFlight != 0 {
		t.Errorf("inFlight = %d after the result, want 0", m.inFlight)
	}
	if !strings.Contains(m.View(), "boom") {
		t.Error("the result should reach the feed")
	}
}
