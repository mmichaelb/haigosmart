package hass

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/lights"
)

// commandPayload is what Home Assistant sends. Pointer fields distinguish "set
// this to zero" from "do not touch this", which is the difference between a
// command that dims a lamp and one that also turns it off.
type commandPayload struct {
	State      *string `json:"state"`
	Brightness *int    `json:"brightness"`
	ColorTempK *int    `json:"color_temp_kelvin"`
	// Mireds are the older convention; accepted so an older Home Assistant, or
	// a hand-written automation, still works.
	ColorTempMired *int `json:"color_temp"`
}

// DeviceIDFromCommandTopic extracts the lamp a command was addressed to.
func (c Config) DeviceIDFromCommandTopic(topic string) (string, bool) {
	cfg := c.withDefaults()
	prefix := cfg.Prefix + "/light/"
	if !strings.HasPrefix(topic, prefix) || !strings.HasSuffix(topic, "/set") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(topic, prefix), "/set")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// ParseCommand turns a Home Assistant command into a service change.
//
// Capability and range checking are deliberately not done here: lights.Service
// owns them, so the terminal and Home Assistant cannot drift apart on what a
// lamp will accept.
func (c Config) ParseCommand(payload []byte, caps bulb.Capabilities) (lights.Change, error) {
	var p commandPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return lights.Change{}, fmt.Errorf("decoding command: %w", err)
	}
	cfg := c.withDefaults()

	var change lights.Change
	if p.State != nil {
		on := strings.EqualFold(*p.State, "ON")
		change.Power = &on
	}
	if p.Brightness != nil {
		pct := BrightnessFromHA(*p.Brightness)
		change.Brightness = &pct
	}
	switch {
	case p.ColorTempK != nil:
		pct := cfg.PercentFromKelvin(*p.ColorTempK)
		change.ColorTemp = &pct
	case p.ColorTempMired != nil && *p.ColorTempMired > 0:
		// Mireds are the reciprocal of Kelvin, scaled by a million.
		pct := cfg.PercentFromKelvin(1_000_000 / *p.ColorTempMired)
		change.ColorTemp = &pct
	}

	if change.IsEmpty() {
		return change, fmt.Errorf("command asked for nothing")
	}
	return change, nil
}
