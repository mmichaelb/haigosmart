package hass

import (
	"encoding/json"
	"fmt"

	"haigosmart/internal/bulb"
)

// statePayload is what Home Assistant reads to know a lamp's condition. It is
// published only from what the lamp itself reported — never from remembered
// state, which would assert something unverified (FR-010).
type statePayload struct {
	State      string `json:"state"`
	Brightness int    `json:"brightness"`
	ColorMode  string `json:"color_mode,omitempty"`
	ColorTempK int    `json:"color_temp_kelvin,omitempty"`
}

// State builds the payload for a lamp's current reported state.
func (c Config) State(b bulb.Bulb) ([]byte, error) {
	cfg := c.withDefaults()
	p := statePayload{
		State:      "OFF",
		Brightness: BrightnessToHA(b.State.Brightness),
	}
	if b.State.Power {
		p.State = "ON"
	}
	// Warmth fields belong only on a lamp that advertises warmth. Sending them
	// otherwise would contradict the capabilities the entity declared.
	if SupportsColorTempEntity(b.Capabilities) {
		p.ColorMode = ModeColorTemp
		p.ColorTempK = cfg.KelvinFromPercent(b.State.ColorTemp)
	}
	out, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encoding state for %s: %w", b.DeviceID, err)
	}
	return out, nil
}

// Availability is the payload saying whether a lamp is reachable. "Off" and
// "not answering" are different facts, and an entity that conflates them will
// mislead an automation.
func Availability(b bulb.Bulb) string {
	if b.Status == bulb.Connected {
		return Online
	}
	return Offline
}
