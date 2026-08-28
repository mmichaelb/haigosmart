// Package lights is the surface-neutral core: reading a lamp's state and
// changing it, with typed results that each front-end renders its own way.
//
// The terminal (internal/control) and the Home Assistant bridge (internal/hass)
// both consume this package and neither depends on the other. Nothing here may
// import a front-end — see the layering test.
package lights

import (
	"errors"
	"fmt"
)

// Errors callers match with errors.Is. They are deliberately typed rather than
// formatted: a terminal wants "brightness must be 0-100, got 150" and Home
// Assistant wants to know the value was out of range. Returning a display
// string would force one of them to parse the other's prose.
var (
	// ErrUnknownBulb means no bulb has that device id.
	ErrUnknownBulb = errors.New("no such bulb")
	// ErrNotAdopted means the bulb has connected but has not been named, so it
	// is visible but not controllable.
	ErrNotAdopted = errors.New("bulb has not been adopted")
	// ErrNotConnected means the bulb is known but is not currently reachable.
	ErrNotConnected = errors.New("bulb is not connected")
	// ErrOutOfRange means a value fell outside what the lamp accepts.
	ErrOutOfRange = errors.New("value out of range")
)

// RangeError reports a value outside an accepted range, carrying the offending
// value so a caller can name it without the service having formatted it.
type RangeError struct {
	What string
	Got  int
	Min  int
	Max  int
}

func (e RangeError) Error() string {
	return fmt.Sprintf("%s must be %d-%d, got %d", e.What, e.Min, e.Max, e.Got)
}

// Is makes errors.Is(err, ErrOutOfRange) work for a RangeError.
func (e RangeError) Is(target error) bool { return target == ErrOutOfRange }
