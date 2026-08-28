package bulb

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Status is where a bulb sits in its connection lifecycle.
type Status uint8

// Connection statuses. Discovered means the bulb has connected but has not been
// adopted by the operator yet; it is visible but not controllable.
const (
	Disconnected Status = iota
	Discovered
	Connected
)

func (s Status) String() string {
	switch s {
	case Connected:
		return "connected"
	case Discovered:
		return "discovered"
	default:
		return "disconnected"
	}
}

// Bulb is one physical bulb known to the server.
type Bulb struct {
	DeviceID     string
	Name         string
	Status       Status
	State        LightState
	Desired      *LightState
	Capabilities Capabilities
	// FirmwareVersion is what the bulb reported, e.g. "aigo_light_cct_v4.0.0".
	// Empty until it has said.
	FirmwareVersion string
	FirstSeen       time.Time
	LastSeen        time.Time
	RemoteAddr      string

	// Driver is the live connection, nil when the bulb is not connected.
	Driver Driver
}

// Model is the hardware family, derived from the firmware version by trimming
// the version suffix: "aigo_light_cct_v4.0.0" becomes "aigo_light_cct".
func (b *Bulb) Model() string {
	v := b.FirmwareVersion
	if i := strings.LastIndex(v, "_v"); i > 0 {
		return v[:i]
	}
	return v
}

// Adopted reports whether the operator has taken ownership of this bulb.
func (b *Bulb) Adopted() bool { return b.Status != Discovered }

// ErrUnsupported is returned when a bulb cannot do what was asked of it.
var ErrUnsupported = errors.New("unsupported by this bulb")

// ErrUnconfirmed means a command was delivered but the bulb has not confirmed
// it yet. It is deliberately not a failure: these bulbs confirm anywhere from a
// fraction of a second to tens of seconds after the fact, so "no confirmation
// yet" and "did not work" are different claims, and reporting the second when
// only the first is known tells the operator something untrue.
var ErrUnconfirmed = errors.New("delivered but not confirmed yet")

// Driver is the seam between the command layer and a bulb's transport. The real
// implementation speaks MQTT; fakebulb implements the same contract in-process
// so every layer above can be tested without hardware.
type Driver interface {
	// DeviceID returns the stable identifier the bulb presented at connect.
	DeviceID() string
	// Apply asks the bulb to move to the given state. It returns when the bulb
	// has acknowledged, or when ctx expires.
	Apply(ctx context.Context, want LightState) error
	// Close tears down the connection.
	Close() error
}
