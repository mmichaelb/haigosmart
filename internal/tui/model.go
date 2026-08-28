// Package tui is the terminal interface: a live event feed above a command
// prompt. It is the only package that imports Bubble Tea, so the interface can
// be replaced without touching any logic.
package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmichaelb/haigosmart/internal/control"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/registry"
)

// feedCapacity is how many lines the feed keeps. Older lines scroll away from
// the display; they are never lost from the log.
const feedCapacity = 500

// Model is the Bubble Tea model.
type Model struct {
	ctrl *control.Controller
	reg  *registry.Registry
	sub  *events.Subscription

	input  textinput.Model
	feed   *feed
	width  int
	height int

	history    []string
	historyPos int
	// inFlight counts commands waiting on hardware, so the operator can see
	// that something is happening rather than wondering if the key registered.
	inFlight int

	quitting bool
	cancel   context.CancelFunc
}

// eventMsg carries one bus event into the Bubble Tea loop.
type eventMsg events.Event

// resultMsg carries a finished command back into the Bubble Tea loop.
//
// Commands run off the update loop because they wait on hardware: a bulb that
// fades takes seconds to confirm, and running that inline would freeze the
// prompt and the event feed for the duration (FR-015).
type resultMsg struct {
	line   string
	result control.Result
}

// runCommand executes a command in the background and reports the outcome.
func runCommand(ctrl *control.Controller, line string) tea.Cmd {
	return func() tea.Msg {
		return resultMsg{line: line, result: ctrl.Execute(context.Background(), line)}
	}
}

// New returns a model ready for tea.NewProgram.
func New(ctrl *control.Controller, reg *registry.Registry, sub *events.Subscription, cancel context.CancelFunc) *Model {
	in := textinput.New()
	in.Placeholder = "type a command, or `help`"
	in.Prompt = "> "
	in.Focus()
	in.CharLimit = 256

	m := &Model{
		ctrl: ctrl, reg: reg, sub: sub,
		input: in, feed: newFeed(feedCapacity), cancel: cancel,
		historyPos: -1,
	}
	m.feed.addResult(control.Result{Kind: control.ResultInfo, Text: "ready. `help` lists commands, `list` shows bulbs"})
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitForEvent(m.sub))
}

// waitForEvent turns the next bus event into a message. Because it blocks in a
// goroutine rather than on the update loop, a burst of events cannot stall the
// keyboard, and a paused UI cannot stall a bulb's read loop.
func waitForEvent(sub *events.Subscription) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-sub.Events()
		if !ok {
			return nil
		}
		return eventMsg(e)
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(msg.Width-4, 1)
		m.feed.resize(m.visibleFeedLines())
		return m, nil

	case eventMsg:
		m.feed.addEvent(events.Event(msg))
		return m, waitForEvent(m.sub)

	case resultMsg:
		m.inFlight--
		if msg.result.Quit {
			return m.quit()
		}
		if msg.result.Text != "" {
			m.feed.addResult(msg.result)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return m.quit()

	case tea.KeyEnter:
		line := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		if line == "" {
			return m, nil
		}
		m.history = append(m.history, line)
		m.historyPos = -1
		m.feed.addPrompt(line)
		m.inFlight++
		return m, runCommand(m.ctrl, line)

	case tea.KeyUp:
		m.recall(1)
		return m, nil
	case tea.KeyDown:
		m.recall(-1)
		return m, nil
	case tea.KeyPgUp:
		m.feed.scroll(-m.visibleFeedLines() / 2)
		return m, nil
	case tea.KeyPgDown:
		m.feed.scroll(m.visibleFeedLines() / 2)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	if m.cancel != nil {
		m.cancel()
	}
	return m, tea.Quit
}

// recall walks the command history: 1 is older, -1 is newer.
func (m *Model) recall(direction int) {
	if len(m.history) == 0 {
		return
	}
	switch {
	case m.historyPos < 0 && direction > 0:
		m.historyPos = len(m.history) - 1
	case m.historyPos >= 0:
		m.historyPos -= direction
	}
	switch {
	case m.historyPos < 0:
		m.historyPos = -1
		m.input.SetValue("")
		return
	case m.historyPos >= len(m.history):
		m.historyPos = len(m.history) - 1
	}
	m.input.SetValue(m.history[m.historyPos])
	m.input.CursorEnd()
}

// visibleFeedLines is the feed height: everything except the status bar, the
// separator and the prompt. It never goes below one line, so a very small
// terminal still renders something coherent.
func (m *Model) visibleFeedLines() int {
	return max(m.height-3, 1)
}
