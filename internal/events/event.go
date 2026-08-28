// Package events carries everything worth showing or logging: bulb state
// changes, connection transitions, command results, and protocol errors.
package events

import (
	"fmt"
	"strings"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
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
	// Rejected is a bulb that connected while the server was running unattended
	// and was not in the configured lamp set. It is a first-class event rather
	// than a bare log line so both surfaces describe it in the same words.
	Rejected

	// numKinds is one past the last kind. It exists so a test can walk every
	// kind and fail when a new one arrives without a log message, a level, and a
	// display string. Keep it last.
	numKinds
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
	case Rejected:
		return "rejected: not in the configured lamp set"
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
