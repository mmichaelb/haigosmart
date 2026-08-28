package protocol

import (
	"bufio"
	"bytes"
	"testing"

	"haigosmart/internal/bulb"
)

func TestCapabilitiesFromVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bulb.Capabilities
	}{
		{
			name: "captured aigo cct bulb", version: "aigo_light_cct_v4.0.0",
			want: bulb.Capabilities{Known: true, Color: false, ColorTemp: true, MinBrightness: 1},
		},
		{
			name: "rgb model", version: "aigo_light_rgb_v1.0.0",
			want: bulb.Capabilities{Known: true, Color: true, ColorTemp: false, MinBrightness: 1},
		},
		{
			name: "rgbcct model", version: "aigo_light_rgbcct_v1.0.0",
			want: bulb.Capabilities{Known: true, Color: true, ColorTemp: true, MinBrightness: 1},
		},
		{
			name: "no version reported", version: "",
			want: bulb.Capabilities{Known: false, MinBrightness: 1},
		},
		{
			name: "unrecognised model stays unknown", version: "vendor_widget_v9",
			want: bulb.Capabilities{Known: false, MinBrightness: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CapabilitiesFromVersion(tc.version); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// An undetermined bulb must not be treated as a definite "no colour": the
// command is attempted so the bulb itself gets to answer.
func TestUnknownCapabilitiesAllowColorThrough(t *testing.T) {
	unknown := CapabilitiesFromVersion("")
	if unknown.Known {
		t.Fatal("expected Known=false")
	}
	if !unknown.SupportsColor() {
		t.Error("an undetermined bulb should have colour attempted, not pre-refused")
	}
	cct := CapabilitiesFromVersion("aigo_light_cct_v4.0.0")
	if cct.SupportsColor() {
		t.Error("a known white-only bulb should refuse colour up front")
	}
}

func TestDecodeOTAVersionFromFixture(t *testing.T) {
	raw, err := ReadPacket(bufio.NewReader(bytes.NewReader(fixture(t, "c2s_ota_inform_step1"))))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	pub, err := DecodePublish(raw.Payload, raw.QoS())
	if err != nil {
		t.Fatalf("DecodePublish: %v", err)
	}
	version := DecodeOTAVersion(pub.Payload)
	if version != "aigo_light_cct_v4.0.0" {
		t.Fatalf("version = %q, want aigo_light_cct_v4.0.0", version)
	}
	caps := CapabilitiesFromVersion(version)
	if !caps.Known || caps.Color || !caps.ColorTemp {
		t.Errorf("caps = %+v, want a known white-only colour-temperature bulb", caps)
	}
}

func TestRefineFromReportOnlyAddsInformation(t *testing.T) {
	temp := uint8(50)
	got := RefineFromReport(bulb.Capabilities{MinBrightness: 1}, PropertyPost{ColorTemp: &temp})
	if !got.ColorTemp || !got.Known {
		t.Errorf("got %+v, want colour temperature recognised from the report", got)
	}
	// A bulb already known to do colour must not lose that.
	rgb := bulb.Capabilities{Known: true, Color: true, MinBrightness: 1}
	if refined := RefineFromReport(rgb, PropertyPost{ColorTemp: &temp}); !refined.Color {
		t.Error("refining dropped a known capability")
	}
}
