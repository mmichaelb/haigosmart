package lights

import (
	"context"
	"errors"
	"fmt"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/events"
	"haigosmart/internal/registry"
)

// DefaultTimeout is how long to wait for a bulb to confirm a change before
// reporting it as outstanding.
//
// It is a reporting threshold, not a deadline: the command has already been
// delivered, and passing this point changes only what the caller is told. The
// hardware's confirmation latency is uneven — the same brightness change has
// been observed confirming after four seconds and after nineteen — so no fixed
// number separates "slow" from "failed".
const DefaultTimeout = 5 * time.Second

// Service reads and changes lamps. Both front-ends consume it and neither
// reaches past it to the registry or a driver.
type Service struct {
	reg     *registry.Registry
	bus     *events.Bus
	timeout time.Duration
}

// New returns a service over the given registry and event bus.
func New(reg *registry.Registry, bus *events.Bus) *Service {
	return &Service{reg: reg, bus: bus, timeout: DefaultTimeout}
}

// SetTimeout overrides the confirmation threshold. A non-positive duration
// restores the default.
func (s *Service) SetTimeout(d time.Duration) {
	if d <= 0 {
		d = DefaultTimeout
	}
	s.timeout = d
}

// Timeout reports the current confirmation threshold.
func (s *Service) Timeout() time.Duration { return s.timeout }

// Snapshot returns every known lamp, ordered by name.
func (s *Service) Snapshot() []bulb.Bulb { return s.reg.List() }

// Get returns one lamp by its stable device id. There is no prefix matching:
// an integration that guessed which lamp was meant would be a bug with the
// lights on.
func (s *Service) Get(deviceID string) (bulb.Bulb, error) {
	b, ok := s.reg.View(deviceID)
	if !ok {
		return bulb.Bulb{}, fmt.Errorf("%q: %w", deviceID, ErrUnknownBulb)
	}
	return b, nil
}

// Subscribe returns a new subscription to the event stream.
func (s *Service) Subscribe(depth int) *events.Subscription { return s.bus.Subscribe(depth) }

// Rename assigns a display name. Naming a lamp that has not been adopted adopts
// it: it becomes controllable and is persisted.
//
// This lives in the service rather than in the terminal because adopting a lamp
// that is already connected produces no other event, and the Home Assistant
// bridge would never learn the lamp had become publishable. The terminal is
// still the only thing that calls it — that is a policy, not a layering
// accident.
func (s *Service) Rename(deviceID, name string) (adopted bool, err error) {
	adopted, err = s.reg.Rename(deviceID, name)
	if err != nil {
		return false, err
	}
	detail := "renamed to " + name
	if adopted {
		detail = "adopted as " + name
	}
	s.bus.Publish(events.Event{
		At: time.Now(), Kind: events.Renamed,
		DeviceID: deviceID, Name: name, Detail: detail,
	})
	return adopted, nil
}

// Change is a partial update. A nil field means "leave this alone", which is
// what distinguishes a request that only sets brightness from one that also
// turns the lamp on.
type Change struct {
	Power      *bool
	Brightness *uint8
	ColorTemp  *uint8
}

// IsEmpty reports whether the change asks for nothing.
func (c Change) IsEmpty() bool { return c.Power == nil && c.Brightness == nil && c.ColorTemp == nil }

// SetPower turns a lamp on or off.
func (s *Service) SetPower(ctx context.Context, deviceID string, on bool) error {
	return s.Apply(ctx, deviceID, Change{Power: &on})
}

// SetBrightness sets a lamp's brightness as a percentage.
func (s *Service) SetBrightness(ctx context.Context, deviceID string, pct uint8) error {
	return s.Apply(ctx, deviceID, Change{Brightness: &pct})
}

// SetColorTemp sets a lamp's white warmth as a percentage, 0 being warmest.
func (s *Service) SetColorTemp(ctx context.Context, deviceID string, pct uint8) error {
	return s.Apply(ctx, deviceID, Change{ColorTemp: &pct})
}

// Apply validates a change, sends it, and waits for the lamp to confirm.
//
// The lamp's own report remains authoritative: this never writes state into the
// registry, it asks the lamp to move and lets the lamp say what happened.
func (s *Service) Apply(ctx context.Context, deviceID string, c Change) error {
	target, err := s.Get(deviceID)
	if err != nil {
		return err
	}
	if !target.Adopted() {
		return fmt.Errorf("%s: %w", target.DeviceID, ErrNotAdopted)
	}
	if target.Status != bulb.Connected {
		return fmt.Errorf("%s (last seen %s): %w",
			target.Name, target.LastSeen.Format("15:04:05"), ErrNotConnected)
	}

	want, err := s.resolve(target, c)
	if err != nil {
		return err
	}

	driver := s.reg.Driver(target.DeviceID)
	if driver == nil {
		return fmt.Errorf("%s: %w", target.Name, ErrNotConnected)
	}
	s.reg.SetDesired(target.DeviceID, want)

	applyCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	switch err := driver.Apply(applyCtx, want); {
	case err == nil, errors.Is(err, bulb.ErrUnconfirmed):
		// Unconfirmed is not a failure. It means delivered-but-not-yet-reported,
		// which this hardware produces routinely. Callers distinguish the two
		// with errors.Is; neither is an error condition to recover from.
		return err
	default:
		s.bus.Publish(events.Event{
			At: time.Now(), Kind: events.CommandResult,
			DeviceID: target.DeviceID, Name: target.Name, Detail: err.Error(),
		})
		return err
	}
}

// resolve turns a partial change into the full state to ask the lamp for,
// validating every field against what the lamp accepts.
func (s *Service) resolve(target bulb.Bulb, c Change) (bulb.LightState, error) {
	want := target.State

	if c.Brightness != nil {
		pct := int(*c.Brightness)
		if pct > 100 {
			return want, RangeError{What: "brightness", Got: pct, Min: 0, Max: 100}
		}
		floor := int(target.Capabilities.MinBrightness)
		if pct > 0 && pct < floor {
			return want, fmt.Errorf("%s: brightness below %d switches this bulb off: %w",
				target.Name, floor, ErrOutOfRange)
		}
		want.Brightness = *c.Brightness
		// Brightness implies power: zero means off, anything else means on.
		want.Power = pct > 0
	}

	if c.ColorTemp != nil {
		pct := int(*c.ColorTemp)
		if pct > 100 {
			return want, RangeError{What: "colour temperature", Got: pct, Min: 0, Max: 100}
		}
		if !target.Capabilities.SupportsColorTemp() {
			return want, fmt.Errorf("%s does not support colour temperature: %w",
				target.Name, bulb.ErrUnsupported)
		}
		want.ColorTemp = *c.ColorTemp
	}

	// Power is applied last so an explicit power field wins over the implication
	// drawn from brightness above.
	if c.Power != nil {
		want.Power = *c.Power
		if want.Power && want.Brightness == 0 {
			// A lamp that has never reported brightness would otherwise be told
			// to turn on at zero, which reads as still off.
			want.Brightness = 100
		}
	}
	return want, nil
}
