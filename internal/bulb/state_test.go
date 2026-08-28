package bulb

import (
	"testing"
	"time"
)

func TestDiff(t *testing.T) {
	base := LightState{Power: true, Brightness: 40, ColorTemp: 20, Mode: ModeWhite}
	tests := []struct {
		name string
		next LightState
		want []string
	}{
		{name: "no change", next: base},
		{
			name: "timestamp alone is not a change",
			next: LightState{Power: true, Brightness: 40, ColorTemp: 20, ReportedAt: time.Now()},
		},
		{
			name: "power", next: LightState{Power: false, Brightness: 40, ColorTemp: 20},
			want: []string{"power off"},
		},
		{
			name: "brightness", next: LightState{Power: true, Brightness: 80, ColorTemp: 20},
			want: []string{"brightness 80"},
		},
		{
			name: "temperature", next: LightState{Power: true, Brightness: 40, ColorTemp: 50},
			want: []string{"temp 50"},
		},
		{
			name: "mode", next: LightState{Power: true, Brightness: 40, ColorTemp: 20, Mode: ModeScene},
			want: []string{"mode scene"},
		},
		{
			name: "everything at once",
			next: LightState{Power: false, Brightness: 1, ColorTemp: 100, Mode: ModeScene},
			want: []string{"power off", "brightness 1", "temp 100", "mode scene"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := base.Diff(tc.next)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d changes %v, want %d", len(got), got, len(tc.want))
			}
			for i, c := range got {
				// The order is stable, so index comparison is meaningful.
				if want := tc.want[i]; c.Field+" "+c.To != want {
					t.Errorf("change %d = %q, want %q", i, c.Field+" "+c.To, want)
				}
			}
		})
	}
}

func TestFieldChangeString(t *testing.T) {
	c := FieldChange{Field: "power", From: "off", To: "on"}
	if got := c.String(); got != "power off→on" {
		t.Errorf("String() = %q", got)
	}
}

func TestCapabilitiesBenefitOfTheDoubt(t *testing.T) {
	tests := []struct {
		name          string
		caps          Capabilities
		wantColor     bool
		wantColorTemp bool
	}{
		{
			name: "undetermined attempts everything",
			caps: Capabilities{Known: false},
			// An unset field must never masquerade as a definite "no".
			wantColor: true, wantColorTemp: true,
		},
		{
			name:      "known white-only refuses colour",
			caps:      Capabilities{Known: true, Color: false, ColorTemp: true},
			wantColor: false, wantColorTemp: true,
		},
		{
			name:      "known full colour",
			caps:      Capabilities{Known: true, Color: true, ColorTemp: true},
			wantColor: true, wantColorTemp: true,
		},
		{
			name:      "known colour-only",
			caps:      Capabilities{Known: true, Color: true, ColorTemp: false},
			wantColor: true, wantColorTemp: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.caps.SupportsColor(); got != tc.wantColor {
				t.Errorf("SupportsColor() = %v, want %v", got, tc.wantColor)
			}
			if got := tc.caps.SupportsColorTemp(); got != tc.wantColorTemp {
				t.Errorf("SupportsColorTemp() = %v, want %v", got, tc.wantColorTemp)
			}
		})
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Connected, "connected"},
		{Discovered, "discovered"},
		{Disconnected, "disconnected"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Status(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestAdopted(t *testing.T) {
	if (&Bulb{Status: Discovered}).Adopted() {
		t.Error("a discovered bulb is not adopted")
	}
	for _, s := range []Status{Connected, Disconnected} {
		if !(&Bulb{Status: s}).Adopted() {
			t.Errorf("a %v bulb has been adopted", s)
		}
	}
}

func TestModeString(t *testing.T) {
	if ModeWhite.String() != "white" || ModeScene.String() != "scene" {
		t.Error("mode names are part of the display contract")
	}
}
