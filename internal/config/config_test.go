package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sample returns a valid value for a flag, chosen from its Go type rather than
// from a list of names. That is what lets the tests below cover every setting
// including ones added after they were written.
func sample(f *flag.Flag) string {
	if f.Name == "lamps" {
		return "a1b2c3d4=envlamp"
	}
	switch f.Value.(flag.Getter).Get().(type) {
	case bool:
		return "true"
	case int:
		return "4321"
	case time.Duration:
		return "7s"
	default:
		return "env-" + f.Name
	}
}

// settings returns the loaded configuration as one map keyed by flag name,
// which is exactly what the startup record contains.
func settings(t *testing.T, c Config) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("starting", "config", c)

	var rec struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("startup record is not JSON: %v\n%s", err, buf.String())
	}
	out := make(map[string]string, len(rec.Config))
	for k, v := range rec.Config {
		switch tv := v.(type) {
		case string:
			out[k] = tv
		case bool:
			out[k] = "true"
			if !tv {
				out[k] = "false"
			}
		case float64:
			out[k] = strconv.FormatFloat(tv, 'f', -1, 64)
		default:
			t.Fatalf("setting %s has unexpected type %T", k, v)
		}
	}
	return out
}

// TestEveryFlagHasAnEnvironmentVariable walks the flag set rather than a list of
// names, so a setting added later without a working variable fails here rather
// than in production.
func TestEveryFlagHasAnEnvironmentVariable(t *testing.T) {
	for _, f := range allFlags() {
		want := sample(f)
		env := map[string]string{EnvName(f.Name): want}

		c, err := Load(nil, lookup(env))
		if err != nil {
			t.Fatalf("%s via %s: %v", f.Name, EnvName(f.Name), err)
		}
		got := settings(t, c)[f.Name]
		if f.Name == "mqtt-password" {
			if got != "(set)" {
				t.Errorf("mqtt-password renders as %q, want %q", got, "(set)")
			}
			continue
		}
		if f.Name == "command-timeout" {
			want = "7s" // rendered by Duration.String, not by the raw input
		}
		if got != want {
			t.Errorf("%s: set %s=%q, config holds %q", f.Name, EnvName(f.Name), want, got)
		}
	}
}

func TestEnvName(t *testing.T) {
	for in, want := range map[string]string{
		"listen":                "HAIGOSMART_LISTEN",
		"v":                     "HAIGOSMART_V",
		"mqtt-broker":           "HAIGOSMART_MQTT_BROKER",
		"mqtt-discovery-prefix": "HAIGOSMART_MQTT_DISCOVERY_PREFIX",
		"ct-min-kelvin":         "HAIGOSMART_CT_MIN_KELVIN",
	} {
		if got := EnvName(in); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPrecedence pins FR-011: default, then environment, then flag.
func TestPrecedence(t *testing.T) {
	t.Run("neither: today's default", func(t *testing.T) {
		c, err := Load(nil, lookup(nil))
		if err != nil {
			t.Fatal(err)
		}
		if c.Listen != ":1883" {
			t.Errorf("listen = %q, want the unchanged default %q", c.Listen, ":1883")
		}
		if len(c.Overrides) != 0 {
			t.Errorf("overrides = %v, want none", c.Overrides)
		}
	})

	t.Run("environment only", func(t *testing.T) {
		c, err := Load(nil, lookup(map[string]string{"HAIGOSMART_LISTEN": ":18830"}))
		if err != nil {
			t.Fatal(err)
		}
		if c.Listen != ":18830" {
			t.Errorf("listen = %q, want %q", c.Listen, ":18830")
		}
		if len(c.Overrides) != 0 {
			t.Errorf("overrides = %v, want none: nothing was overridden", c.Overrides)
		}
	})

	t.Run("flag beats environment, and says so once", func(t *testing.T) {
		env := map[string]string{"HAIGOSMART_LISTEN": ":18830", "HAIGOSMART_MQTT_PREFIX": "fromenv"}
		c, err := Load([]string{"-listen", ":18831"}, lookup(env))
		if err != nil {
			t.Fatal(err)
		}
		if c.Listen != ":18831" {
			t.Errorf("listen = %q, want the flag's %q", c.Listen, ":18831")
		}
		if c.MQTTPrefix != "fromenv" {
			t.Errorf("mqtt-prefix = %q, want the variable's %q", c.MQTTPrefix, "fromenv")
		}
		if len(c.Overrides) != 1 || c.Overrides[0] != "listen" {
			t.Fatalf("overrides = %v, want exactly [listen]", c.Overrides)
		}
		// The value may be a credential, so an override report names the
		// setting and stops there.
		for _, o := range c.Overrides {
			if strings.Contains(o, ":1883") {
				t.Errorf("override %q carries a value", o)
			}
		}
	})

	t.Run("flag alone is not an override", func(t *testing.T) {
		c, err := Load([]string{"-listen", ":18831"}, lookup(nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Overrides) != 0 {
			t.Errorf("overrides = %v, want none: nothing was overridden", c.Overrides)
		}
	})
}

// TestPasswordNeverRenders is the whole of SC-006. It greps everything the
// process could emit about a configuration, not one record's shape, because
// that is how a leak actually escapes.
func TestPasswordNeverRenders(t *testing.T) {
	const secret = "hunter2-do-not-print"
	env := map[string]string{
		"HAIGOSMART_MQTT_PASSWORD": secret,
		"HAIGOSMART_MQTT_BROKER":   "127.0.0.1:1883",
		"HAIGOSMART_CT_MIN_KELVIN": "9000", // also forces an invalid configuration
	}
	c, err := Load(nil, lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if c.MQTTPassword != secret {
		t.Fatalf("password was not loaded; this test would pass for the wrong reason")
	}

	var buf bytes.Buffer
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		log.Log(t.Context(), level, "starting", "config", c)
	}
	// Validation errors mention settings; one of them is the password's
	// neighbour in the same struct.
	if err := c.Validate(); err != nil {
		buf.WriteString(err.Error())
	}
	buf.WriteString(c.LogValue().String())

	if strings.Contains(buf.String(), secret) {
		t.Errorf("the password appears in output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "(set)") {
		t.Errorf("the password's presence is not reported at all; (set) was expected")
	}
}

func TestPasswordUnsetRendersUnset(t *testing.T) {
	c, err := Load(nil, lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := settings(t, c)["mqtt-password"]; got != "(unset)" {
		t.Errorf("mqtt-password renders as %q, want %q", got, "(unset)")
	}
}

// TestValidate covers each rule in data-model.md. Every message must name the
// setting and the value received, because a message that does not is a message
// the operator has to guess from.
func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		wantAll []string
	}{
		{
			name:    "inverted kelvin range",
			env:     map[string]string{"HAIGOSMART_CT_MIN_KELVIN": "7000"},
			wantAll: []string{"ct-min-kelvin", "7000", "6500"},
		},
		{
			name:    "non-positive command timeout",
			env:     map[string]string{"HAIGOSMART_COMMAND_TIMEOUT": "0s"},
			wantAll: []string{"command-timeout", "0s"},
		},
		{
			name:    "unparseable listen address",
			env:     map[string]string{"HAIGOSMART_LISTEN": "not-an-address"},
			wantAll: []string{"listen", "not-an-address"},
		},
		{
			name:    "headless with no lamps",
			env:     map[string]string{"HAIGOSMART_HEADLESS": "true"},
			wantAll: []string{"lamps", "HAIGOSMART_LAMPS"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Load(nil, lookup(tc.env))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			err = c.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %v", tc.env)
			}
			for _, want := range tc.wantAll {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestLoadRejectsUnparseableEnvironmentValues: a bad variable is a startup
// failure naming the variable, not a silently ignored value.
func TestLoadRejectsUnparseableEnvironmentValues(t *testing.T) {
	for _, tc := range []struct{ variable, value string }{
		{"HAIGOSMART_CT_MIN_KELVIN", "warm"},
		{"HAIGOSMART_COMMAND_TIMEOUT", "soon"},
		{"HAIGOSMART_HEADLESS", "maybe"},
	} {
		_, err := Load(nil, lookup(map[string]string{tc.variable: tc.value}))
		if err == nil {
			t.Errorf("%s=%q was accepted", tc.variable, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.variable) || !strings.Contains(err.Error(), tc.value) {
			t.Errorf("error %q names neither the variable nor the value", err)
		}
	}
}

func TestDefaultsAreUnchanged(t *testing.T) {
	c, err := Load(nil, lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"listen":                ":1883",
		"headless":              "false",
		"v":                     "false",
		"log":                   "",
		"command-timeout":       "5s",
		"lamps":                 "",
		"mqtt-broker":           "",
		"mqtt-username":         "",
		"mqtt-client-id":        "haigosmart",
		"mqtt-prefix":           "haigosmart",
		"mqtt-discovery-prefix": "homeassistant",
		"ct-min-kelvin":         "2700",
		"ct-max-kelvin":         "6500",
	} {
		if got := settings(t, c)[name]; got != want {
			t.Errorf("default %s = %q, want %q", name, got, want)
		}
	}
	if err := c.Validate(); err != nil {
		t.Errorf("the default configuration does not validate: %v", err)
	}
}

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}
