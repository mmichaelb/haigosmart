// Package events carries everything worth showing or logging: bulb state
// changes, connection transitions, command results, and protocol errors.
package events

import (
	"fmt"
	"strings"
	"time"

	"haigosmart/internal/bulb"
)

// Kind classifies an event.
type Kind uint8

// Event kinds.
const (
	StateChanged Kind = iota
	Connected
	Disconnected
	Discovered
	CommandResult
	ProtocolError
	DuplicateID
	// Renamed covers both adoption and a later rename. It exists because a lamp
	// adopted while already connected produces no other event, and anything
	// watching the registry would never learn the lamp had become controllable.
	Renamed
)

// Event is a single thing that happened, ready to render or log.
type Event struct {
	At       time.Time
	Kind     Kind
	DeviceID string
	Name     string
	Changed  []bulb.FieldChange
	Detail   string
}

// Label is the display name for the event's bulb, falling back to the device id
// when the bulb has no name yet.
func (e Event) Label() string {
	if e.Name != "" {
		return e.Name
	}
	if e.DeviceID != "" {
		return e.DeviceID
	}
	return "server"
}

// Text renders the event body in the single format defined by
// specs/001-local-bulb-server/contracts/tui-commands.md.
func (e Event) Text() string {
	switch e.Kind {
	case Connected:
		return "connected"
	case Disconnected:
		if e.Detail != "" {
			return "disconnected (" + e.Detail + ")"
		}
		return "disconnected"
	case Discovered:
		return "discovered — name it to control it"
	case CommandResult:
		return "command failed: " + e.Detail
	case ProtocolError:
		return "protocol error: " + e.Detail
	case DuplicateID:
		return "WARNING duplicate device id, also seen from " + e.Detail
	case Renamed:
		return e.Detail
	default:
		parts := make([]string, 0, len(e.Changed))
		for _, c := range e.Changed {
			parts = append(parts, c.String())
		}
		if len(parts) == 0 {
			return "state reported (no change)"
		}
		return strings.Join(parts, "  ")
	}
}

// Line is the full feed line: timestamp, bulb, text.
func (e Event) Line() string {
	return fmt.Sprintf("%s  %-14s %s", e.At.Format("15:04:05"), e.Label(), e.Text())
}
