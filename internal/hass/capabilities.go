package hass

import "haigosmart/internal/bulb"

// Home Assistant colour modes. A light entity declares which of these it
// supports, and Home Assistant renders exactly those controls and no others.
const (
	ModeOnOff      = "onoff"
	ModeBrightness = "brightness"
	ModeColorTemp  = "color_temp"
	ModeRGB        = "rgb"
)

// ColorModes maps a lamp's capabilities onto what the entity may claim.
//
// This is the whole of User Story 2. A white-only lamp declares
// ["color_temp"], and Home Assistant then shows a warmth slider and no colour
// wheel — not because anything was hidden, but because the entity never claimed
// a colour channel it does not have.
//
// The undetermined case is the one that matters. Feature 001 deliberately
// distinguishes "this lamp has no colour" from "we never found out", and only
// the first may be advertised as fact. An unclassified lamp falls back to
// brightness, which every lamp seen so far supports, rather than guessing in
// either direction.
func ColorModes(c bulb.Capabilities) []string {
	if !c.Known {
		return []string{ModeBrightness}
	}
	switch {
	case c.Color && c.ColorTemp:
		return []string{ModeColorTemp, ModeRGB}
	case c.Color:
		return []string{ModeRGB}
	case c.ColorTemp:
		return []string{ModeColorTemp}
	default:
		return []string{ModeBrightness}
	}
}

// SupportsColorTempEntity reports whether the entity advertises warmth, which
// decides whether Kelvin fields belong in its payloads at all.
func SupportsColorTempEntity(c bulb.Capabilities) bool {
	for _, m := range ColorModes(c) {
		if m == ModeColorTemp {
			return true
		}
	}
	return false
}
