// Package config assembles one running instance's settings from defaults, the
// process environment, and the command line, in that order.
//
// Every setting is declared exactly once, on a flag set. The environment name is
// derived from the flag name rather than listed separately, so a setting cannot
// exist in one form and not the other, and the documentation table is generated
// from the same declaration. See
// specs/003-headless-deployment/contracts/configuration.md.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"haigosmart/internal/control"
	"haigosmart/internal/registry"
	"haigosmart/internal/server"
)

// EnvPrefix is the prefix of every environment variable this program reads.
const EnvPrefix = "HAIGOSMART_"

// EnvName is the environment variable for a flag: the prefix, then the flag
// name uppercased with dashes turned into underscores. There is no exception,
// which is what keeps the two surfaces from drifting apart.
func EnvName(flagName string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// Lamp is one entry of the configured lamp set: a lamp this instance is
// responsible for, and the name it is presented under.
type Lamp struct {
	DeviceID string
	Name     string
}

// Config is the complete description of one running instance. It is built once
// at startup and read-only thereafter.
type Config struct {
	Listen         string
	Headless       bool
	Verbose        bool
	LogPath        string
	RegistryPath   string
	CommandTimeout time.Duration
	Lamps          []Lamp

	MQTTBroker      string
	MQTTUsername    string
	MQTTPassword    string
	MQTTClientID    string
	MQTTPrefix      string
	DiscoveryPrefix string

	MinKelvin int
	MaxKelvin int

	// Overrides names the settings given on the command line that also had an
	// environment variable set. Names only: a value may be a credential.
	Overrides []string
}

// DefaultRegistryPath is the registry location when none is configured. A
// failure to locate the user's config directory is not fatal — a relative path
// in the working directory is a worse default but a working one.
func DefaultRegistryPath() string {
	if p, err := registry.DefaultPath(); err == nil {
		return p
	}
	return "registry.json"
}

// newFlagSet declares every setting once. Defaults here are the defaults from
// features 001 and 002, unchanged: an operator who upgrades and sets nothing
// sees no difference.
func (c *Config) newFlagSet() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("haigosmartd", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the caller reports errors; flag's own output is not a record

	lamps := fs.String("lamps", "", "lamps this instance serves, as deviceID=name pairs separated by commas; required with -headless")

	fs.StringVar(&c.Listen, "listen", server.DefaultAddr, "address to accept bulb connections on")
	fs.BoolVar(&c.Headless, "headless", false, "run without the terminal interface, serving only the configured lamps")
	fs.BoolVar(&c.Verbose, "v", false, "debug logging, including per-frame protocol traces")
	fs.StringVar(&c.LogPath, "log", "", "write structured logs here (default: stdout in -headless, a temp file otherwise)")
	fs.StringVar(&c.RegistryPath, "registry", DefaultRegistryPath(), "path to the registry file")
	fs.DurationVar(&c.CommandTimeout, "command-timeout", control.CommandTimeout,
		"how long to wait for a bulb to confirm a command; bulbs that fade report only once the fade finishes")

	fs.StringVar(&c.MQTTBroker, "mqtt-broker", "", "host:port of your MQTT broker; empty disables the Home Assistant integration")
	fs.StringVar(&c.MQTTUsername, "mqtt-username", "", "broker username, if the broker needs one")
	fs.StringVar(&c.MQTTPassword, "mqtt-password", "", "broker password, if the broker needs one")
	fs.StringVar(&c.MQTTClientID, "mqtt-client-id", "haigosmart", "client id presented to the broker")
	fs.StringVar(&c.MQTTPrefix, "mqtt-prefix", "haigosmart", "base topic for state, availability and commands")
	fs.StringVar(&c.DiscoveryPrefix, "mqtt-discovery-prefix", "homeassistant", "Home Assistant discovery prefix")

	fs.IntVar(&c.MinKelvin, "ct-min-kelvin", 2700, "Kelvin at the lamp's warmest setting")
	fs.IntVar(&c.MaxKelvin, "ct-max-kelvin", 6500, "Kelvin at the lamp's coolest setting")

	return fs, lamps
}

// allFlags returns every declared setting, for documentation and for tests that
// must cover settings added after they were written.
func allFlags() []*flag.Flag {
	var c Config
	fs, _ := c.newFlagSet()
	var out []*flag.Flag
	fs.VisitAll(func(f *flag.Flag) { out = append(out, f) })
	return out
}

// Load assembles the configuration: defaults, then environment, then command
// line. Precedence is the ordering itself — the environment is applied to the
// flag set before parsing, so parsing overwrites it — rather than a precedence
// engine that could disagree with the documentation.
//
// env is the lookup function, normally os.Getenv. An empty variable counts as
// unset; the alternative would be distinguishing "" from absent for settings
// whose default is already "".
func Load(args []string, env func(string) string) (Config, error) {
	var c Config
	fs, lamps := c.newFlagSet()

	fromEnv := map[string]bool{}
	var envErr error
	fs.VisitAll(func(f *flag.Flag) {
		name := EnvName(f.Name)
		value := env(name)
		if value == "" || envErr != nil {
			return
		}
		if err := fs.Set(f.Name, value); err != nil {
			envErr = fmt.Errorf("%s=%q is not a valid %s: %w", name, value, kindOf(f), err)
			return
		}
		fromEnv[f.Name] = true
	})
	if envErr != nil {
		return Config{}, envErr
	}

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parsing command-line flags: %w", err)
	}

	// Anything given on the command line that the environment had also set was
	// overridden. Worth recording, so an instance behaving unlike its manifest
	// explains itself.
	//
	// fs.Visit cannot answer this on its own: applying the environment goes
	// through fs.Set, which marks a flag as set just as parsing does, so by now
	// the two are indistinguishable on this flag set. Parsing the arguments once
	// more against a throwaway set — declared by the same function, so there is
	// still only one declaration — separates them.
	for name := range flagsGivenOn(args) {
		if fromEnv[name] {
			c.Overrides = append(c.Overrides, name)
		}
	}
	slices.Sort(c.Overrides)

	parsed, err := ParseLamps(*lamps)
	if err != nil {
		return Config{}, err
	}
	c.Lamps = parsed
	return c, nil
}

// flagsGivenOn reports which settings appeared on the command line.
func flagsGivenOn(args []string) map[string]bool {
	var scratch Config
	fs, _ := scratch.newFlagSet()
	if err := fs.Parse(args); err != nil {
		return nil // the real parse reports this
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	return given
}

// kindOf names a flag's type for an error message, in the words an operator
// would use rather than Go's.
func kindOf(f *flag.Flag) string {
	switch f.Value.(flag.Getter).Get().(type) {
	case bool:
		return "boolean (true or false)"
	case int:
		return "whole number"
	case time.Duration:
		return "duration (for example 5s or 2m)"
	default:
		return "string"
	}
}

// Validate checks everything that can be checked before a socket is opened. It
// refuses rather than repairs: a silently corrected setting is a setting whose
// manifest lies about what is running.
func (c Config) Validate() error {
	if c.MinKelvin >= c.MaxKelvin {
		return fmt.Errorf("ct-min-kelvin (%d) must be below ct-max-kelvin (%d); an inverted range would reverse every warmth value shown in Home Assistant", c.MinKelvin, c.MaxKelvin)
	}
	if c.CommandTimeout <= 0 {
		return fmt.Errorf("command-timeout is %s; it must be positive, or every command would be reported unconfirmed before the bulb could answer", c.CommandTimeout)
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen is %q, which is not a host:port address (for example :1883 or 192.168.1.10:1883)", c.Listen)
	}
	if c.Headless && len(c.Lamps) == 0 {
		return errors.New("no lamps are configured: set HAIGOSMART_LAMPS (or -lamps) to deviceID=name pairs. " +
			"An unattended instance serves only the lamps it is told about, so one with an empty set would refuse every connection")
	}
	return nil
}

// LogValue renders the configuration for the startup record, with the password
// replaced by whether it is set.
//
// Redaction lives here rather than at each call site so that it holds for call
// sites that do not exist yet: there is no path through which the real value
// formats, which is what makes FR-014 structural rather than a promise.
func (c Config) LogValue() slog.Value {
	password := "(unset)"
	if c.MQTTPassword != "" {
		password = "(set)"
	}
	lamps := make([]string, 0, len(c.Lamps))
	for _, l := range c.Lamps {
		lamps = append(lamps, l.DeviceID+"="+l.Name)
	}
	return slog.GroupValue(
		slog.String("listen", c.Listen),
		slog.Bool("headless", c.Headless),
		slog.Bool("v", c.Verbose),
		slog.String("log", c.LogPath),
		slog.String("registry", c.RegistryPath),
		slog.String("command-timeout", c.CommandTimeout.String()),
		slog.String("lamps", strings.Join(lamps, ",")),
		slog.String("mqtt-broker", c.MQTTBroker),
		slog.String("mqtt-username", c.MQTTUsername),
		slog.String("mqtt-password", password),
		slog.String("mqtt-client-id", c.MQTTClientID),
		slog.String("mqtt-prefix", c.MQTTPrefix),
		slog.String("mqtt-discovery-prefix", c.DiscoveryPrefix),
		slog.Int("ct-min-kelvin", c.MinKelvin),
		slog.Int("ct-max-kelvin", c.MaxKelvin),
	)
}
