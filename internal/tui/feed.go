package tui

import (
	"strings"

	"haigosmart/internal/control"
	"haigosmart/internal/events"
)

// feed is a bounded scrollback of rendered lines, newest at the bottom.
type feed struct {
	lines    []string
	capacity int
	height   int
	// offset is how far the operator has scrolled back from the newest line.
	offset int
	// evicted counts lines dropped from the display. They remain in the log.
	evicted int
}

func newFeed(capacity int) *feed {
	return &feed{capacity: capacity, height: 20}
}

func (f *feed) resize(height int) {
	if height < 1 {
		height = 1
	}
	f.height = height
	f.clampOffset()
}

func (f *feed) add(line string) {
	for _, l := range strings.Split(line, "\n") {
		f.lines = append(f.lines, l)
	}
	if over := len(f.lines) - f.capacity; over > 0 {
		f.lines = f.lines[over:]
		f.evicted += over
	}
	// Adding a line while scrolled back keeps the operator's position rather
	// than yanking them to the bottom mid-read.
	if f.offset > 0 {
		f.clampOffset()
	}
}

func (f *feed) addEvent(e events.Event) { f.add(e.Line()) }

func (f *feed) addResult(r control.Result) { f.add(r.Line()) }

func (f *feed) addPrompt(line string) { f.add("> " + line) }

// scroll moves the view: negative is back into history, positive is forward.
func (f *feed) scroll(delta int) {
	f.offset -= delta
	f.clampOffset()
}

func (f *feed) clampOffset() {
	maxOffset := max(len(f.lines)-f.height, 0)
	if f.offset > maxOffset {
		f.offset = maxOffset
	}
	if f.offset < 0 {
		f.offset = 0
	}
}

// visible returns the lines to render, oldest first.
func (f *feed) visible() []string {
	end := len(f.lines) - f.offset
	if end < 0 {
		end = 0
	}
	start := max(end-f.height, 0)
	return f.lines[start:end]
}

// scrolledBack reports whether the operator is looking at history rather than
// the newest events.
func (f *feed) scrolledBack() bool { return f.offset > 0 }
