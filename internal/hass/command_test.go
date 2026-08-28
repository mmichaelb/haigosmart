package hass

import (
	"testing"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/lights"
)

func cctCaps() bulb.Capabilities {
	return bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}
}

func TestParseCommand(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		name    string
		payload string
		check   func(*testing.T, lights.Change)
	}{
		{
			name: "power on", payload: `{"state":"ON"}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Power == nil || !*c.Power {
					t.Error("power not set on")
				}
				if c.Brightness != nil || c.ColorTemp != nil {
					t.Error("fields the command did not carry were set")
				}
			},
		},
		{
			name: "power off", payload: `{"state":"OFF"}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Power == nil || *c.Power {
					t.Error("power not set off")
				}
			},
		},
		{
			name: "lowercase state is accepted", payload: `{"state":"on"}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Power == nil || !*c.Power {
					t.Error("lowercase state was not understood")
				}
			},
		},
		{
			// The case a partial command exists for: dimming must not also
			// change power.
			name: "brightness alone", payload: `{"brightness":128}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Power != nil {
					t.Error("a brightness-only command touched power")
				}
				if c.Brightness == nil || *c.Brightness != 50 {
					t.Errorf("brightness = %v, want 50", c.Brightness)
				}
			},
		},
		{
			name: "warmth in kelvin", payload: `{"color_temp_kelvin":4600}`,
			check: func(t *testing.T, c lights.Change) {
				if c.ColorTemp == nil || *c.ColorTemp != 50 {
					t.Errorf("warmth = %v, want 50", c.ColorTemp)
				}
			},
		},
		{
			name: "warmth in mireds still works", payload: `{"color_temp":370}`,
			check: func(t *testing.T, c lights.Change) {
				// 1000000/370 is about 2702K, essentially the warmest end.
				if c.ColorTemp == nil || *c.ColorTemp > 2 {
					t.Errorf("warmth = %v, want the warmest end", c.ColorTemp)
				}
			},
		},
		{
			name: "everything at once", payload: `{"state":"ON","brightness":255,"color_temp_kelvin":6500}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Power == nil || !*c.Power {
					t.Error("power missing")
				}
				if c.Brightness == nil || *c.Brightness != 100 {
					t.Errorf("brightness = %v, want 100", c.Brightness)
				}
				if c.ColorTemp == nil || *c.ColorTemp != 100 {
					t.Errorf("warmth = %v, want 100", c.ColorTemp)
				}
			},
		},
		{
			name: "brightness zero is a real value, not an omission", payload: `{"brightness":0}`,
			check: func(t *testing.T, c lights.Change) {
				if c.Brightness == nil || *c.Brightness != 0 {
					t.Errorf("brightness = %v, want an explicit 0", c.Brightness)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.ParseCommand([]byte(tc.payload), cctCaps())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestParseCommandRejectsRubbish(t *testing.T) {
	cfg := DefaultConfig()
	for _, payload := range []string{
		``, `not json`, `{`, `[]`, `{}`, `{"unrelated":true}`, `null`,
	} {
		if _, err := cfg.ParseCommand([]byte(payload), cctCaps()); err == nil {
			t.Errorf("payload %q should have been refused", payload)
		}
	}
}

func TestDeviceIDFromCommandTopic(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		topic string
		want  string
		ok    bool
	}{
		{"haigosmart/light/703e975dc388/set", "703e975dc388", true},
		{"haigosmart/light/703e975dc388/state", "", false},
		{"haigosmart/light//set", "", false},
		{"haigosmart/light/a/b/set", "", false},
		{"other/light/abc/set", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := cfg.DeviceIDFromCommandTopic(tc.topic)
		if ok != tc.ok || got != tc.want {
			t.Errorf("DeviceIDFromCommandTopic(%q) = %q,%v want %q,%v", tc.topic, got, ok, tc.want, tc.ok)
		}
	}
}
