// Package bulb holds the domain types for a single smart bulb: its identity,
// its capabilities, its light state, and the driver seam used to command it.
package bulb

import (
	"fmt"
	"time"
)

// Mode is the operating mode a bulb is in. Aigo CCT bulbs report LightMode 0
// for normal white operation; scene modes are reported but not driven by us.
type Mode uint8

// Operating modes reported by the bulb.
const (
	ModeWhite Mode = iota // normal white / colour-temperature operation
	ModeScene             // a stored scene is running
)

func (m Mode) String() string {
	if m == ModeScene {
		return "scene"
	}
	return "white"
}

// LightState is a bulb's light configuration in normalised units. The wire
// encodings live in internal/protocol; nothing above that package deals in
// device units.
//
// Brightness and ColorTemp are both percentages because that is what the
// hardware actually uses: the Aigo CCT firmware takes ColorTemperature as
// 0-100 (0 = warmest, 100 = coolest), not as Kelvin. See
// specs/001-local-bulb-server/contracts/bulb-protocol.md section 8.
type LightState struct {
	Power      bool      `json:"power"`
	Brightness uint8     `json:"brightness"` // 1-100; the hardware never accepts 0
	ColorTemp  uint8     `json:"color_temp"` // 0-100, 0 = warmest
	Mode       Mode      `json:"mode"`
	ReportedAt time.Time `json:"reported_at"`
}

// FieldChange records one field that differed between two states. It is the
// unit the event feed renders.
type FieldChange struct {
	Field string
	From  string
	To    string
}

func (c FieldChange) String() string { return fmt.Sprintf("%s %s→%s", c.Field, c.From, c.To) }

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Diff reports the fields in which next differs from s, in a stable order.
// ReportedAt is deliberately excluded: every report carries a new timestamp,
// and reporting that as a change would make every keep-alive look like an event.
func (s LightState) Diff(next LightState) []FieldChange {
	var out []FieldChange
	if s.Power != next.Power {
		out = append(out, FieldChange{"power", onOff(s.Power), onOff(next.Power)})
	}
	if s.Brightness != next.Brightness {
		out = append(out, FieldChange{"brightness", fmt.Sprint(s.Brightness), fmt.Sprint(next.Brightness)})
	}
	if s.ColorTemp != next.ColorTemp {
		out = append(out, FieldChange{"temp", fmt.Sprint(s.ColorTemp), fmt.Sprint(next.ColorTemp)})
	}
	if s.Mode != next.Mode {
		out = append(out, FieldChange{"mode", s.Mode.String(), next.Mode.String()})
	}
	return out
}

// Capabilities is what a bulb can actually do. Known distinguishes "this bulb
// has no colour" from "we never found out" — without it, the zero value silently
// claims a bulb is white-only, which is the wrong answer to guess.
type Capabilities struct {
	Known         bool  `json:"known"`
	Color         bool  `json:"color"`
	ColorTemp     bool  `json:"color_temp"`
	MinBrightness uint8 `json:"min_brightness"`
}

// SupportsColorTemp reports whether a colour-temperature command should be
// refused up front. Like SupportsColor, an undetermined bulb is given the
// benefit of the doubt.
func (c Capabilities) SupportsColorTemp() bool { return !c.Known || c.ColorTemp }

// SupportsColor reports whether a colour command should be refused up front.
// An undetermined bulb is given the benefit of the doubt and the command is
// attempted, so an unknown capability never masquerades as a definite "no".
func (c Capabilities) SupportsColor() bool { return !c.Known || c.Color }
