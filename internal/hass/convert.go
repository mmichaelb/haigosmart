package hass

// The lamps and Home Assistant disagree about units in two places, and both
// conversions have to survive a round trip: Home Assistant sends a value, the
// lamp reports back in its own scale, and the entity must settle rather than
// drift a little further on every exchange.

// haBrightnessMax is Home Assistant's brightness scale.
const haBrightnessMax = 255

// BrightnessToHA converts the lamp's 0-100 to Home Assistant's 0-255.
func BrightnessToHA(pct uint8) int {
	if pct > 100 {
		pct = 100
	}
	return int(pct) * haBrightnessMax / 100
}

// BrightnessFromHA converts Home Assistant's 0-255 to the lamp's 0-100.
//
// Rounding is to nearest rather than truncating, so the mapping is stable: a
// slider at 128 becomes 50, which converts back to 127 and then to 50 again.
// Truncating would let a value creep downward every time it round-tripped.
func BrightnessFromHA(v int) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= haBrightnessMax:
		return 100
	}
	return uint8((v*100 + haBrightnessMax/2) / haBrightnessMax)
}

// KelvinFromPercent converts the lamp's warmth percentage to Kelvin, where 0 is
// the warmest setting.
func (c Config) KelvinFromPercent(pct uint8) int {
	cfg := c.withDefaults()
	if pct > 100 {
		pct = 100
	}
	span := cfg.MaxKelvin - cfg.MinKelvin
	return cfg.MinKelvin + (int(pct)*span+50)/100
}

// PercentFromKelvin converts Kelvin back to the lamp's warmth percentage,
// clamping to the ends rather than wrapping.
func (c Config) PercentFromKelvin(kelvin int) uint8 {
	cfg := c.withDefaults()
	span := cfg.MaxKelvin - cfg.MinKelvin
	if span <= 0 {
		return 0
	}
	switch {
	case kelvin <= cfg.MinKelvin:
		return 0
	case kelvin >= cfg.MaxKelvin:
		return 100
	}
	return uint8(((kelvin-cfg.MinKelvin)*100 + span/2) / span)
}
