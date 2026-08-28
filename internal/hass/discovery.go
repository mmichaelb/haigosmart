package hass

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

// discoveryPayload is the Home Assistant MQTT light discovery config. The JSON
// schema light is used because it expresses this lamp in two topics rather than
// five, and because supported_color_modes is what makes the entity's controls
// match the hardware.
type discoveryPayload struct {
	Schema           string            `json:"schema"`
	UniqueID         string            `json:"unique_id"`
	ObjectID         string            `json:"object_id"`
	Name             string            `json:"name"`
	StateTopic       string            `json:"state_topic"`
	CommandTopic     string            `json:"command_topic"`
	Brightness       bool              `json:"brightness"`
	BrightnessScale  int               `json:"brightness_scale"`
	ColorModes       []string          `json:"supported_color_modes"`
	MinKelvin        int               `json:"min_kelvin,omitempty"`
	MaxKelvin        int               `json:"max_kelvin,omitempty"`
	AvailabilityMode string            `json:"availability_mode"`
	Availability     []availabilityRef `json:"availability"`
	Device           discoveryDevice   `json:"device"`
}

type availabilityRef struct {
	Topic string `json:"topic"`
}

type discoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

// Discovery builds the payload announcing a lamp to Home Assistant.
//
// The name here is the *default*. Home Assistant keys the entity on unique_id
// and treats a name set by the owner inside Home Assistant as an override, so
// republishing after a rename in the terminal updates the default without
// stamping on the owner's choice, and the entity's history survives.
func (c Config) Discovery(b bulb.Bulb) ([]byte, error) {
	cfg := c.withDefaults()
	p := discoveryPayload{
		Schema:           "json",
		UniqueID:         cfg.UniqueID(b.DeviceID),
		ObjectID:         b.DeviceID,
		Name:             b.Name,
		StateTopic:       cfg.StateTopic(b.DeviceID),
		CommandTopic:     cfg.CommandTopic(b.DeviceID),
		Brightness:       true,
		BrightnessScale:  haBrightnessMax,
		ColorModes:       ColorModes(b.Capabilities),
		AvailabilityMode: "all",
		Availability: []availabilityRef{
			{Topic: cfg.StatusTopic()},
			{Topic: cfg.AvailabilityTopic(b.DeviceID)},
		},
		Device: discoveryDevice{
			Identifiers:  []string{cfg.UniqueID(b.DeviceID)},
			Name:         b.Name,
			Manufacturer: "Aigo",
			Model:        modelName(b),
			SWVersion:    b.FirmwareVersion,
			ViaDevice:    cfg.Prefix,
		},
	}
	// Kelvin bounds belong only on a lamp that actually has warmth; sending them
	// for a brightness-only lamp would advertise a control it does not have.
	if SupportsColorTempEntity(b.Capabilities) {
		p.MinKelvin = cfg.MinKelvin
		p.MaxKelvin = cfg.MaxKelvin
	}
	out, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encoding discovery for %s: %w", b.DeviceID, err)
	}
	return out, nil
}

// modelName turns the firmware family into something a person would recognise
// on a device card. "aigo_light_cct" is a firmware token, not a product name,
// and Home Assistant shows the model prominently. The raw string is still
// carried in sw_version for anyone debugging.
func modelName(b bulb.Bulb) string {
	switch family := b.Model(); {
	case family == "":
		return "Smart Bulb"
	case strings.Contains(family, "rgbcct"), strings.Contains(family, "rgbw"):
		return "Colour and White Smart Bulb"
	case strings.Contains(family, "rgb"):
		return "Colour Smart Bulb"
	case strings.Contains(family, "cct"):
		return "Tunable White Smart Bulb"
	default:
		return "Smart Bulb"
	}
}

// bridgeSensorPayload announces the server itself as a device.
//
// Every lamp's discovery names the server as its via_device, and Home Assistant
// resolves that against a device's identifiers. Without a device carrying those
// identifiers it invents a placeholder, which is where "Unnamed device" comes
// from. MQTT discovery creates a device only as a side effect of an entity, so
// the server needs one entity of its own — a connectivity sensor, which is worth
// having anyway: it is how Home Assistant shows whether the server is running.
type bridgeSensorPayload struct {
	Name        string          `json:"name"`
	UniqueID    string          `json:"unique_id"`
	ObjectID    string          `json:"object_id"`
	StateTopic  string          `json:"state_topic"`
	DeviceClass string          `json:"device_class"`
	PayloadOn   string          `json:"payload_on"`
	PayloadOff  string          `json:"payload_off"`
	EntityCat   string          `json:"entity_category"`
	Device      discoveryDevice `json:"device"`
}

// BridgeConfigTopic is where the server's own discovery is published.
func (c Config) BridgeConfigTopic() string {
	cfg := c.withDefaults()
	return fmt.Sprintf("%s/binary_sensor/%s/status/config", cfg.DiscoveryPrefix, cfg.Prefix)
}

// BridgeDiscovery announces the server as a device with a connectivity sensor.
//
// The sensor deliberately has no availability block. Its whole job is to report
// when the server is gone, and an entity that goes unavailable at exactly that
// moment would report nothing at all.
func (c Config) BridgeDiscovery(version string) ([]byte, error) {
	cfg := c.withDefaults()
	p := bridgeSensorPayload{
		Name:        "Status",
		UniqueID:    cfg.Prefix + "_bridge_status",
		ObjectID:    cfg.Prefix + "_status",
		StateTopic:  cfg.StatusTopic(),
		DeviceClass: "connectivity",
		PayloadOn:   Online,
		PayloadOff:  Offline,
		EntityCat:   "diagnostic",
		Device: discoveryDevice{
			Identifiers:  []string{cfg.Prefix},
			Name:         "haigosmart",
			Manufacturer: "haigosmart",
			Model:        "Bulb Server",
			SWVersion:    version,
		},
	}
	out, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encoding bridge discovery: %w", err)
	}
	return out, nil
}
