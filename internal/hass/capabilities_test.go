package hass

import (
	"reflect"
	"testing"

	"haigosmart/internal/bulb"
)

// Every row of the capability mapping in data-model.md. This is User Story 2:
// what the entity claims decides what Home Assistant renders.
func TestColorModes(t *testing.T) {
	tests := []struct {
		name string
		caps bulb.Capabilities
		want []string
	}{
		{
			name: "captured cct lamp: warmth, no colour wheel",
			caps: bulb.Capabilities{Known: true, ColorTemp: true},
			want: []string{ModeColorTemp},
		},
		{
			name: "colour and warmth",
			caps: bulb.Capabilities{Known: true, Color: true, ColorTemp: true},
			want: []string{ModeColorTemp, ModeRGB},
		},
		{
			name: "colour only",
			caps: bulb.Capabilities{Known: true, Color: true},
			want: []string{ModeRGB},
		},
		{
			name: "neither",
			caps: bulb.Capabilities{Known: true},
			want: []string{ModeBrightness},
		},
		{
			// The case feature 001 went out of its way to distinguish. A lamp we
			// could not classify must claim only what is certain — not "no
			// colour", which would be an assertion we cannot support.
			name: "undetermined claims only what is certain",
			caps: bulb.Capabilities{Known: false},
			want: []string{ModeBrightness},
		},
		{
			name: "undetermined ignores stale capability bits",
			caps: bulb.Capabilities{Known: false, Color: true, ColorTemp: true},
			want: []string{ModeBrightness},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ColorModes(tc.caps); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ColorModes() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A white-only lamp must never advertise a colour mode anywhere, which is the
// user-visible promise: no colour wheel.
func TestWhiteOnlyLampNeverClaimsColour(t *testing.T) {
	modes := ColorModes(bulb.Capabilities{Known: true, ColorTemp: true})
	for _, m := range modes {
		if m == ModeRGB {
			t.Fatalf("a white-only lamp advertised %q", m)
		}
	}
}

func TestSupportsColorTempEntity(t *testing.T) {
	tests := []struct {
		caps bulb.Capabilities
		want bool
	}{
		{bulb.Capabilities{Known: true, ColorTemp: true}, true},
		{bulb.Capabilities{Known: true, Color: true, ColorTemp: true}, true},
		{bulb.Capabilities{Known: true, Color: true}, false},
		{bulb.Capabilities{Known: true}, false},
		{bulb.Capabilities{Known: false}, false},
	}
	for _, tc := range tests {
		if got := SupportsColorTempEntity(tc.caps); got != tc.want {
			t.Errorf("SupportsColorTempEntity(%+v) = %v, want %v", tc.caps, got, tc.want)
		}
	}
}
