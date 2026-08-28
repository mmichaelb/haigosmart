package hass

import (
	"encoding/json"
	"testing"

	"haigosmart/internal/bulb"
)

// cctLamp is the captured hardware: white-only, warmth-capable.
func cctLamp() bulb.Bulb {
	return bulb.Bulb{
		DeviceID:        "703e975dc388",
		Name:            "headlamp",
		Status:          bulb.Connected,
		FirmwareVersion: "aigo_light_cct_v4.0.0",
		Capabilities:    bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1},
		State:           bulb.LightState{Power: true, Brightness: 80, ColorTemp: 20},
	}
}

// decode unmarshals into a generic map, so assertions are against the JSON Home
// Assistant will actually read rather than against our own struct.
func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("payload is not valid json: %v", err)
	}
	return m
}

func TestDiscoveryPayload(t *testing.T) {
	cfg := DefaultConfig()
	raw, err := cfg.Discovery(cctLamp())
	if err != nil {
		t.Fatal(err)
	}
	p := decode(t, raw)

	// Field by field against contracts/mqtt-discovery.md.
	tests := []struct {
		key  string
		want any
	}{
		{"schema", "json"},
		{"unique_id", "haigosmart_703e975dc388"},
		{"object_id", "703e975dc388"},
		{"name", "headlamp"},
		{"state_topic", "haigosmart/light/703e975dc388/state"},
		{"command_topic", "haigosmart/light/703e975dc388/set"},
		{"brightness", true},
		{"brightness_scale", float64(255)},
		{"availability_mode", "all"},
		{"min_kelvin", float64(2700)},
		{"max_kelvin", float64(6500)},
	}
	for _, tc := range tests {
		if got := p[tc.key]; got != tc.want {
			t.Errorf("%s = %v (%T), want %v", tc.key, got, got, tc.want)
		}
	}

	modes, ok := p["supported_color_modes"].([]any)
	if !ok || len(modes) != 1 || modes[0] != "color_temp" {
		t.Errorf("supported_color_modes = %v, want [color_temp]", p["supported_color_modes"])
	}

	avail, ok := p["availability"].([]any)
	if !ok || len(avail) != 2 {
		t.Fatalf("availability = %v, want two entries", p["availability"])
	}
	wantTopics := map[string]bool{
		"haigosmart/status":                          true,
		"haigosmart/light/703e975dc388/availability": true,
	}
	for _, entry := range avail {
		topic := entry.(map[string]any)["topic"].(string)
		if !wantTopics[topic] {
			t.Errorf("unexpected availability topic %q", topic)
		}
		delete(wantTopics, topic)
	}
	if len(wantTopics) != 0 {
		t.Errorf("missing availability topics: %v", wantTopics)
	}

	device, ok := p["device"].(map[string]any)
	if !ok {
		t.Fatal("no device block")
	}
	if device["name"] != "headlamp" || device["manufacturer"] != "Aigo" {
		t.Errorf("device = %v", device)
	}
	// Home Assistant shows the model prominently, so it has to read like a
	// product name rather than a firmware token.
	if device["model"] != "Tunable White Smart Bulb" {
		t.Errorf("model = %v, want a human-readable product name", device["model"])
	}
	if device["sw_version"] != "aigo_light_cct_v4.0.0" {
		t.Errorf("sw_version = %v", device["sw_version"])
	}
	ids, ok := device["identifiers"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "haigosmart_703e975dc388" {
		t.Errorf("identifiers = %v", device["identifiers"])
	}
}

// A lamp with no warmth must not carry Kelvin bounds, or the entity advertises a
// control the hardware does not have.
func TestDiscoveryOmitsKelvinForBrightnessOnlyLamps(t *testing.T) {
	lamp := cctLamp()
	lamp.Capabilities = bulb.Capabilities{Known: true, MinBrightness: 1}
	raw, err := DefaultConfig().Discovery(lamp)
	if err != nil {
		t.Fatal(err)
	}
	p := decode(t, raw)
	if _, ok := p["min_kelvin"]; ok {
		t.Error("a brightness-only lamp carried min_kelvin")
	}
	if _, ok := p["max_kelvin"]; ok {
		t.Error("a brightness-only lamp carried max_kelvin")
	}
	modes := p["supported_color_modes"].([]any)
	if len(modes) != 1 || modes[0] != "brightness" {
		t.Errorf("supported_color_modes = %v, want [brightness]", modes)
	}
}

func TestDiscoveryUsesConfiguredPrefixes(t *testing.T) {
	cfg := Config{DiscoveryPrefix: "ha", Prefix: "lamps", MinKelvin: 3000, MaxKelvin: 6000}
	raw, err := cfg.Discovery(cctLamp())
	if err != nil {
		t.Fatal(err)
	}
	p := decode(t, raw)
	if p["state_topic"] != "lamps/light/703e975dc388/state" {
		t.Errorf("state_topic = %v", p["state_topic"])
	}
	if p["unique_id"] != "lamps_703e975dc388" {
		t.Errorf("unique_id = %v", p["unique_id"])
	}
	if p["min_kelvin"] != float64(3000) || p["max_kelvin"] != float64(6000) {
		t.Errorf("kelvin range = %v/%v", p["min_kelvin"], p["max_kelvin"])
	}
	if got := cfg.ConfigTopic("703e975dc388"); got != "ha/light/lamps/703e975dc388/config" {
		t.Errorf("config topic = %q", got)
	}
}

// Renaming must not change the identity, or Home Assistant creates a second
// device and the history is orphaned.
func TestRenameKeepsIdentity(t *testing.T) {
	before, err := DefaultConfig().Discovery(cctLamp())
	if err != nil {
		t.Fatal(err)
	}
	renamed := cctLamp()
	renamed.Name = "hallway"
	after, err := DefaultConfig().Discovery(renamed)
	if err != nil {
		t.Fatal(err)
	}
	b, a := decode(t, before), decode(t, after)
	if b["unique_id"] != a["unique_id"] {
		t.Error("unique_id changed on rename; the entity's history would be lost")
	}
	if a["name"] != "hallway" {
		t.Errorf("name = %v, want the new one", a["name"])
	}
}

func TestStatePayload(t *testing.T) {
	tests := []struct {
		name  string
		lamp  bulb.Bulb
		check func(*testing.T, map[string]any)
	}{
		{
			name: "on with warmth",
			lamp: cctLamp(),
			check: func(t *testing.T, p map[string]any) {
				if p["state"] != "ON" {
					t.Errorf("state = %v", p["state"])
				}
				if p["brightness"] != float64(204) { // 80% of 255
					t.Errorf("brightness = %v, want 204", p["brightness"])
				}
				if p["color_mode"] != "color_temp" {
					t.Errorf("color_mode = %v", p["color_mode"])
				}
				if p["color_temp_kelvin"] != float64(3460) { // 2700 + 20% of 3800
					t.Errorf("color_temp_kelvin = %v, want 3460", p["color_temp_kelvin"])
				}
			},
		},
		{
			name: "off",
			lamp: func() bulb.Bulb { l := cctLamp(); l.State.Power = false; return l }(),
			check: func(t *testing.T, p map[string]any) {
				if p["state"] != "OFF" {
					t.Errorf("state = %v", p["state"])
				}
			},
		},
		{
			name: "brightness only lamp omits warmth",
			lamp: func() bulb.Bulb {
				l := cctLamp()
				l.Capabilities = bulb.Capabilities{Known: true, MinBrightness: 1}
				return l
			}(),
			check: func(t *testing.T, p map[string]any) {
				if _, ok := p["color_temp_kelvin"]; ok {
					t.Error("a brightness-only lamp reported a colour temperature")
				}
				if _, ok := p["color_mode"]; ok {
					t.Error("a brightness-only lamp reported a colour mode")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := DefaultConfig().State(tc.lamp)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, decode(t, raw))
		})
	}
}

func TestAvailability(t *testing.T) {
	lamp := cctLamp()
	if got := Availability(lamp); got != Online {
		t.Errorf("connected lamp = %q, want online", got)
	}
	lamp.Status = bulb.Disconnected
	if got := Availability(lamp); got != Offline {
		t.Errorf("disconnected lamp = %q, want offline", got)
	}
	lamp.Status = bulb.Discovered
	if got := Availability(lamp); got != Offline {
		t.Errorf("discovered lamp = %q, want offline", got)
	}
}

// Every lamp names the server as its via_device. Home Assistant resolves that
// against a device's identifiers, and shows "Unnamed device" when nothing
// declares them — so the server has to announce itself.
func TestBridgeDiscoveryDeclaresTheViaDevice(t *testing.T) {
	cfg := DefaultConfig()

	lampRaw, err := cfg.Discovery(cctLamp())
	if err != nil {
		t.Fatal(err)
	}
	lamp := decode(t, lampRaw)
	via := lamp["device"].(map[string]any)["via_device"]

	bridgeRaw, err := cfg.BridgeDiscovery("0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	bridge := decode(t, bridgeRaw)
	device, ok := bridge["device"].(map[string]any)
	if !ok {
		t.Fatal("the bridge payload has no device block")
	}
	ids, ok := device["identifiers"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("identifiers = %v", device["identifiers"])
	}
	if ids[0] != via {
		t.Errorf("the lamp's via_device is %v but the server declares %v; Home Assistant would show an unnamed device",
			via, ids[0])
	}
	if device["name"] != "haigosmart" {
		t.Errorf("device name = %v, want a real name", device["name"])
	}
	if device["sw_version"] != "0.2.0" {
		t.Errorf("sw_version = %v", device["sw_version"])
	}
	// The bridge device is the top of the tree, so it has no parent itself.
	if _, ok := device["via_device"]; ok {
		t.Error("the server declared a via_device of its own")
	}
}

func TestBridgeDiscoveryIsAConnectivitySensor(t *testing.T) {
	cfg := DefaultConfig()
	raw, err := cfg.BridgeDiscovery("0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	p := decode(t, raw)
	tests := []struct {
		key  string
		want any
	}{
		{"device_class", "connectivity"},
		{"state_topic", "haigosmart/status"},
		{"payload_on", "online"},
		{"payload_off", "offline"},
		{"unique_id", "haigosmart_bridge_status"},
		{"entity_category", "diagnostic"},
	}
	for _, tc := range tests {
		if got := p[tc.key]; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.key, got, tc.want)
		}
	}
	// It reports the server being gone, so it must not itself go unavailable at
	// that moment — it would report nothing at all.
	if _, ok := p["availability"]; ok {
		t.Error("the connectivity sensor declared an availability topic; it would vanish exactly when it matters")
	}
}

func TestBridgeConfigTopic(t *testing.T) {
	if got := DefaultConfig().BridgeConfigTopic(); got != "homeassistant/binary_sensor/haigosmart/status/config" {
		t.Errorf("bridge config topic = %q", got)
	}
}

func TestModelNameIsReadable(t *testing.T) {
	tests := []struct {
		firmware string
		want     string
	}{
		{"aigo_light_cct_v4.0.0", "Tunable White Smart Bulb"},
		{"aigo_light_rgb_v1.0.0", "Colour Smart Bulb"},
		{"aigo_light_rgbcct_v1.0.0", "Colour and White Smart Bulb"},
		{"", "Smart Bulb"},
		{"something_unrecognised_v1", "Smart Bulb"},
	}
	for _, tc := range tests {
		lamp := cctLamp()
		lamp.FirmwareVersion = tc.firmware
		if got := modelName(lamp); got != tc.want {
			t.Errorf("modelName(%q) = %q, want %q", tc.firmware, got, tc.want)
		}
	}
	// The raw firmware string still has to be available for debugging.
	raw, err := DefaultConfig().Discovery(cctLamp())
	if err != nil {
		t.Fatal(err)
	}
	if decode(t, raw)["device"].(map[string]any)["sw_version"] != "aigo_light_cct_v4.0.0" {
		t.Error("the raw firmware version was lost")
	}
}
