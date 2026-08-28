package hass

import "testing"

func TestKelvinMapping(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		pct  uint8
		want int
	}{
		{0, 2700},   // warmest
		{100, 6500}, // coolest
		{50, 4600},
	}
	for _, tc := range tests {
		if got := cfg.KelvinFromPercent(tc.pct); got != tc.want {
			t.Errorf("KelvinFromPercent(%d) = %d, want %d", tc.pct, got, tc.want)
		}
	}
}

func TestKelvinClampsRatherThanWraps(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.PercentFromKelvin(1000); got != 0 {
		t.Errorf("below the range = %d, want 0", got)
	}
	if got := cfg.PercentFromKelvin(10000); got != 100 {
		t.Errorf("above the range = %d, want 100", got)
	}
	if got := cfg.KelvinFromPercent(200); got != 6500 {
		t.Errorf("percent above 100 = %d, want the maximum", got)
	}
}

// A value must not drift on every exchange between Home Assistant and the lamp.
func TestKelvinRoundTripIsStable(t *testing.T) {
	cfg := DefaultConfig()
	for pct := 0; pct <= 100; pct++ {
		k := cfg.KelvinFromPercent(uint8(pct))
		back := cfg.PercentFromKelvin(k)
		if int(back) != pct {
			t.Errorf("percent %d became %d after a round trip through %dK", pct, back, k)
		}
		// And a second pass must not move further.
		if again := cfg.PercentFromKelvin(cfg.KelvinFromPercent(back)); again != back {
			t.Errorf("percent %d kept drifting: %d then %d", pct, back, again)
		}
	}
}

func TestBrightnessConversion(t *testing.T) {
	tests := []struct {
		pct  uint8
		want int
	}{{0, 0}, {50, 127}, {80, 204}, {100, 255}}
	for _, tc := range tests {
		if got := BrightnessToHA(tc.pct); got != tc.want {
			t.Errorf("BrightnessToHA(%d) = %d, want %d", tc.pct, got, tc.want)
		}
	}
	if got := BrightnessToHA(200); got != 255 {
		t.Errorf("out-of-range percent = %d, want 255", got)
	}
}

// The bug this guards against: a Home Assistant slider at 128 becoming 50,
// converting back to 127, and creeping downward on every exchange.
func TestBrightnessRoundTripIsStable(t *testing.T) {
	for pct := 0; pct <= 100; pct++ {
		ha := BrightnessToHA(uint8(pct))
		if back := BrightnessFromHA(ha); int(back) != pct {
			t.Errorf("percent %d became %d after a round trip through %d", pct, back, ha)
		}
	}
	for ha := 0; ha <= 255; ha++ {
		pct := BrightnessFromHA(ha)
		again := BrightnessFromHA(BrightnessToHA(pct))
		if again != pct {
			t.Errorf("HA value %d settled on %d then drifted to %d", ha, pct, again)
		}
	}
}

func TestBrightnessBounds(t *testing.T) {
	if got := BrightnessFromHA(-5); got != 0 {
		t.Errorf("negative = %d, want 0", got)
	}
	if got := BrightnessFromHA(999); got != 100 {
		t.Errorf("above the scale = %d, want 100", got)
	}
}

// A zero Config must still produce usable topics rather than ones with empty
// path segments.
func TestZeroConfigGetsDefaults(t *testing.T) {
	var cfg Config
	if got := cfg.StateTopic("abc"); got != "haigosmart/light/abc/state" {
		t.Errorf("state topic from a zero config = %q", got)
	}
	if got := cfg.KelvinFromPercent(0); got != 2700 {
		t.Errorf("warmest from a zero config = %d, want 2700", got)
	}
}
