package hass

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/lights"
	"github.com/mmichaelb/haigosmart/internal/mqtt"
)

// Bridge publishes the lamps to Home Assistant and applies commands coming back.
type Bridge struct {
	cfg    Config
	svc    *lights.Service
	client *mqtt.Client
	log    *slog.Logger

	// reported tracks which lamps have actually spoken since this process
	// started. Nothing is published, and no lamp is marked available, until it
	// appears here — persisted state is a memory, not a report (FR-010).
	mu       sync.Mutex
	reported map[string]bool
	// announced tracks the name each lamp was last published under, so a rename
	// republishes and an unchanged name does not.
	announced map[string]string

	// version is shown on the server's device card in Home Assistant.
	version string
}

// New returns a bridge. Nothing is published until Run is called.
func New(cfg Config, svc *lights.Service, client *mqtt.Client, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{
		cfg: cfg.withDefaults(), svc: svc, client: client, log: log,
		reported:  make(map[string]bool),
		announced: make(map[string]string),
		version:   Version,
	}
}

// Version is reported on the server's device card. It is a plain constant
// rather than build metadata because there is nothing yet that stamps one in.
const Version = "0.2.0"

// Run publishes discovery, follows the event stream, and applies inbound
// commands until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	sub := b.svc.Subscribe(256)
	defer sub.Close()

	if err := b.client.Subscribe(b.cfg.CommandFilter(), b.onCommand); err != nil {
		b.log.Warn("could not subscribe to the command topic", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			b.publishOffline()
			return nil
		case e, ok := <-sub.Events():
			if !ok {
				return nil
			}
			b.handle(e)
		}
	}
}

// OnConnect republishes everything after a (re)connect. Retained messages then
// carry a restarted Home Assistant back to a correct picture with no action from
// anyone.
func (b *Bridge) OnConnect() {
	// The server's own device first. Every lamp names it as their via_device,
	// and Home Assistant shows an "Unnamed device" placeholder until something
	// actually declares it.
	if payload, err := b.cfg.BridgeDiscovery(b.version); err == nil {
		if err := b.client.Publish(b.cfg.BridgeConfigTopic(), payload, true); err != nil {
			b.log.Warn("could not publish the server's own discovery", "error", err)
		}
	} else {
		b.log.Error("could not build the server's discovery", "error", err)
	}
	if err := b.client.Publish(b.cfg.StatusTopic(), []byte(Online), true); err != nil {
		b.log.Warn("could not publish bridge availability", "error", err)
	}
	if err := b.client.Subscribe(b.cfg.CommandFilter(), b.onCommand); err != nil {
		b.log.Warn("could not subscribe to the command topic", "error", err)
	}
	for _, lamp := range b.svc.Snapshot() {
		b.announce(lamp)
	}
}

// handle turns one event into the publications it implies.
func (b *Bridge) handle(e events.Event) {
	if e.DeviceID == "" {
		return
	}
	lamp, err := b.svc.Get(e.DeviceID)
	if err != nil {
		return
	}

	switch e.Kind {
	case events.Discovered:
		// Not adopted, so not published. An unnamed device would clutter the
		// house with entries nobody can identify (FR-015).
		return

	case events.Connected:
		b.announce(lamp)

	case events.Renamed:
		// Adoption of an already-connected lamp arrives here and nowhere else.
		b.announce(lamp)
		b.mu.Lock()
		reported := b.reported[e.DeviceID]
		b.mu.Unlock()
		if reported || lamp.Status == bulb.Connected {
			b.publishState(lamp)
			b.publishAvailability(lamp.DeviceID, Availability(lamp))
		}

	case events.Disconnected:
		b.mu.Lock()
		delete(b.reported, e.DeviceID)
		b.mu.Unlock()
		b.publishAvailability(lamp.DeviceID, Offline)

	case events.StateChanged:
		b.mu.Lock()
		first := !b.reported[e.DeviceID]
		b.reported[e.DeviceID] = true
		b.mu.Unlock()
		b.announce(lamp)
		b.publishState(lamp)
		if first {
			// Available only once the lamp has actually spoken.
			b.publishAvailability(lamp.DeviceID, Online)
		}
	}
}

// announce publishes a lamp's discovery config, if it is adopted and its name
// has changed since the last announcement.
func (b *Bridge) announce(lamp bulb.Bulb) {
	if !lamp.Adopted() {
		return
	}
	b.mu.Lock()
	unchanged := b.announced[lamp.DeviceID] == lamp.Name
	b.mu.Unlock()
	if unchanged {
		return
	}

	payload, err := b.cfg.Discovery(lamp)
	if err != nil {
		b.log.Error("could not build discovery payload", "device", lamp.DeviceID, "error", err)
		return
	}
	if err := b.client.Publish(b.cfg.ConfigTopic(lamp.DeviceID), payload, true); err != nil {
		b.log.Warn("could not publish discovery", "device", lamp.DeviceID, "error", err)
		return
	}
	b.mu.Lock()
	b.announced[lamp.DeviceID] = lamp.Name
	b.mu.Unlock()
	b.log.Info("announced a lamp to home assistant", "device", lamp.DeviceID, "name", lamp.Name)
}

// Remove takes a lamp out of Home Assistant with an empty retained payload,
// rather than leaving it forever as an unavailable entry (FR-016).
func (b *Bridge) Remove(deviceID string) {
	if err := b.client.Publish(b.cfg.ConfigTopic(deviceID), nil, true); err != nil {
		b.log.Warn("could not remove a lamp from home assistant", "device", deviceID, "error", err)
	}
	b.mu.Lock()
	delete(b.announced, deviceID)
	delete(b.reported, deviceID)
	b.mu.Unlock()
}

func (b *Bridge) publishState(lamp bulb.Bulb) {
	payload, err := b.cfg.State(lamp)
	if err != nil {
		b.log.Error("could not build state payload", "device", lamp.DeviceID, "error", err)
		return
	}
	if err := b.client.Publish(b.cfg.StateTopic(lamp.DeviceID), payload, true); err != nil {
		b.log.Warn("could not publish state", "device", lamp.DeviceID, "error", err)
	}
}

func (b *Bridge) publishAvailability(deviceID, value string) {
	if err := b.client.Publish(b.cfg.AvailabilityTopic(deviceID), []byte(value), true); err != nil {
		b.log.Warn("could not publish availability", "device", deviceID, "error", err)
	}
}

// publishOffline marks the bridge down on a deliberate shutdown. The last will
// covers the case this cannot: a crash or a pulled cable.
func (b *Bridge) publishOffline() {
	if err := b.client.Publish(b.cfg.StatusTopic(), []byte(Offline), true); err != nil {
		b.log.Debug("could not publish shutdown availability", "error", err)
	}
}

// onCommand applies a command from Home Assistant.
func (b *Bridge) onCommand(topic string, payload []byte) {
	deviceID, ok := b.cfg.DeviceIDFromCommandTopic(topic)
	if !ok {
		b.log.Warn("command on an unrecognised topic", "topic", topic)
		return
	}
	lamp, err := b.svc.Get(deviceID)
	if err != nil {
		b.log.Warn("command for an unknown lamp", "device", deviceID)
		return
	}
	change, err := b.cfg.ParseCommand(payload, lamp.Capabilities)
	if err != nil {
		// A malformed command is logged and dropped. It must not drop the broker
		// connection, and it must not affect another lamp (FR-017).
		b.log.Warn("could not parse a command", "device", deviceID, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.svc.Timeout()+2*time.Second)
	defer cancel()

	switch err := b.svc.Apply(ctx, deviceID, change); {
	case err == nil:
	case errors.Is(err, bulb.ErrUnconfirmed):
		// Not a failure. The lamp has the command and will report when it is
		// done; the entity stays as it was until then rather than being told a
		// value nobody has confirmed.
		b.log.Debug("command delivered, awaiting the lamp's report", "device", deviceID)
	default:
		b.log.Warn("command failed", "device", deviceID, "error", err)
	}
	// Nothing is published here on purpose. The lamp changes, the lamp reports,
	// and the report is what reaches Home Assistant.
}
