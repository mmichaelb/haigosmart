// Package hass publishes the lamps to an MQTT broker using Home Assistant's
// discovery convention, and accepts commands back.
//
// The owner runs the broker; this package is a client of it. The lamps never
// talk to the broker at all — only this server does — so no broker problem can
// put a lamp back on the vendor's cloud, and none can stop the terminal from
// controlling a lamp.
package hass

import "fmt"

// Config holds everything the bridge needs that is not a lamp.
type Config struct {
	// DiscoveryPrefix is Home Assistant's discovery root, "homeassistant" by
	// default.
	DiscoveryPrefix string
	// Prefix is this server's own topic root.
	Prefix string
	// MinKelvin and MaxKelvin bound the warmth scale. The lamps report a
	// percentage and never state their Kelvin endpoints; 2700-6500 is correct
	// for the aigo_light_cct_v4.0.0.
	MinKelvin int
	MaxKelvin int
}

// DefaultConfig returns the settings for the captured hardware.
func DefaultConfig() Config {
	return Config{
		DiscoveryPrefix: "homeassistant",
		Prefix:          "haigosmart",
		MinKelvin:       2700,
		MaxKelvin:       6500,
	}
}

// withDefaults fills in anything the caller left blank, so a zero Config is
// still usable rather than producing topics with empty path segments.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.DiscoveryPrefix == "" {
		c.DiscoveryPrefix = d.DiscoveryPrefix
	}
	if c.Prefix == "" {
		c.Prefix = d.Prefix
	}
	if c.MinKelvin <= 0 {
		c.MinKelvin = d.MinKelvin
	}
	if c.MaxKelvin <= 0 {
		c.MaxKelvin = d.MaxKelvin
	}
	return c
}

// ConfigTopic is where a lamp's discovery payload is published. An empty
// retained payload here removes the device from Home Assistant.
func (c Config) ConfigTopic(deviceID string) string {
	cfg := c.withDefaults()
	return fmt.Sprintf("%s/light/%s/%s/config", cfg.DiscoveryPrefix, cfg.Prefix, deviceID)
}

// StateTopic carries a lamp's current state.
func (c Config) StateTopic(deviceID string) string {
	return fmt.Sprintf("%s/light/%s/state", c.withDefaults().Prefix, deviceID)
}

// CommandTopic is where Home Assistant sends changes.
func (c Config) CommandTopic(deviceID string) string {
	return fmt.Sprintf("%s/light/%s/set", c.withDefaults().Prefix, deviceID)
}

// AvailabilityTopic reports whether one lamp is reachable.
func (c Config) AvailabilityTopic(deviceID string) string {
	return fmt.Sprintf("%s/light/%s/availability", c.withDefaults().Prefix, deviceID)
}

// CommandFilter subscribes to every lamp's command topic at once.
func (c Config) CommandFilter() string {
	return fmt.Sprintf("%s/light/+/set", c.withDefaults().Prefix)
}

// StatusTopic reports whether this server is running. Its "offline" value is
// registered as the MQTT last will, which is the only thing that reports a
// crash or a pulled cable.
func (c Config) StatusTopic() string { return c.withDefaults().Prefix + "/status" }

// UniqueID is the entity identity Home Assistant keys on. It must never change,
// or dashboards and automations break.
func (c Config) UniqueID(deviceID string) string { return c.withDefaults().Prefix + "_" + deviceID }

// Availability payload values.
const (
	Online  = "online"
	Offline = "offline"
)
