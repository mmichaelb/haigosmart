package protocol

import (
	"encoding/json"
	"strings"

	"haigosmart/internal/bulb"
)

// DecodeOTAVersion extracts the firmware version the bulb reports on
// /ota/device/inform/{pk}/{dn}, e.g. "aigo_light_cct_v4.0.0".
func DecodeOTAVersion(payload []byte) string {
	var msg struct {
		Params struct {
			Version string `json:"version"`
		} `json:"params"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return ""
	}
	return msg.Params.Version
}

// CapabilitiesFromVersion derives what a bulb can do from its firmware version
// string. The model token is embedded there: the captured device reports
// "aigo_light_cct_v4.0.0", where cct means correlated colour temperature —
// a white-only bulb with no colour channel at all.
//
// An unrecognised or empty version yields Known=false rather than a confident
// "no colour". The difference matters: a bulb we could not classify has its
// colour commands attempted, while a bulb we know is white-only has them
// refused with a clear message.
func CapabilitiesFromVersion(version string) bulb.Capabilities {
	caps := bulb.Capabilities{MinBrightness: 1}
	v := strings.ToLower(version)
	switch {
	case v == "":
		return caps // Known stays false
	case strings.Contains(v, "rgbcct"), strings.Contains(v, "rgbw"):
		caps.Known, caps.Color, caps.ColorTemp = true, true, true
	case strings.Contains(v, "rgb"):
		caps.Known, caps.Color = true, true
	case strings.Contains(v, "cct"):
		caps.Known, caps.ColorTemp = true, true
	}
	return caps
}

// RefineFromReport corroborates capabilities using the properties the bulb
// actually reported. It only ever adds information: a colour-temperature
// property proves ColorTemp support, and a bulb we could not classify from its
// version can still be classified from what it reports.
func RefineFromReport(caps bulb.Capabilities, post PropertyPost) bulb.Capabilities {
	if post.ColorTemp != nil {
		caps.ColorTemp = true
		if !caps.Known {
			// It reports colour temperature and nothing colour-related; that is
			// enough to call it a CCT bulb.
			caps.Known = true
		}
	}
	return caps
}
