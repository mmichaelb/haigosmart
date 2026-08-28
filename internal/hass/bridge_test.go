package hass

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/bulb/fakebulb"
	"haigosmart/internal/events"
	"haigosmart/internal/lights"
	"haigosmart/internal/mqtt"
	"haigosmart/internal/mqtt/mqtttest"
	"haigosmart/internal/registry"
	"haigosmart/internal/server"
)

// rig is the whole stack: a lamp server with a fake bulb, a stub broker, and a
// bridge between them. Nothing about Home Assistant is simulated — the broker
// implements the wire protocol only, so payload assertions are against Home
// Assistant's published format rather than against a helper of ours.
type rig struct {
	broker *mqtttest.Broker
	reg    *registry.Registry
	svc    *lights.Service
	bridge *Bridge
	fb     *fakebulb.Bulb
}

func newRig(t *testing.T) *rig {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(quiet)
	reg := registry.New(nil)
	srv := server.New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	srvDone := make(chan struct{})
	go func() { defer close(srvDone); _ = srv.Serve(ctx, ln) }()

	broker, err := mqtttest.Start()
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	svc := lights.New(reg, bus)
	var bridge *Bridge
	client := mqtt.New(mqtt.Options{
		Broker: broker.Addr(), ClientID: "haigosmart", Logger: quiet,
		KeepAlive:   time.Second,
		WillTopic:   DefaultConfig().StatusTopic(),
		WillPayload: []byte(Offline),
		WillRetain:  true,
		OnConnect:   func() { bridge.OnConnect() },
	})
	bridge = New(DefaultConfig(), svc, client, quiet)

	clientDone := make(chan struct{})
	bridgeDone := make(chan struct{})
	go func() { defer close(clientDone); _ = client.Run(ctx) }()
	go func() { defer close(bridgeDone); _ = bridge.Run(ctx) }()

	if !broker.WaitFor(3*time.Second, client.Connected) {
		cancel()
		t.Fatal("the bridge never connected to the broker")
	}

	fb, err := fakebulb.Dial(ln.Addr().String(), fakebulb.Options{
		DeviceName: "703e975dc388", Version: "aigo_light_cct_v4.0.0",
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fb.Close()
		cancel()
		<-srvDone
		<-clientDone
		<-bridgeDone
		broker.Close()
	})

	r := &rig{broker: broker, reg: reg, svc: svc, bridge: bridge, fb: fb}
	if !broker.WaitFor(3*time.Second, func() bool {
		b, ok := reg.View("703e975dc388")
		return ok && b.Capabilities.Known && b.State.Brightness == 30
	}) {
		t.Fatal("the lamp never registered")
	}
	return r
}

func (r *rig) adopt(t *testing.T, name string) {
	t.Helper()
	if _, err := r.svc.Rename("703e975dc388", name); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) retained(t *testing.T, topic string) map[string]any {
	t.Helper()
	m, ok := r.broker.Retained(topic)
	if !ok {
		t.Fatalf("nothing retained on %s", topic)
	}
	var out map[string]any
	if err := json.Unmarshal(m.Payload, &out); err != nil {
		t.Fatalf("payload on %s is not json: %v", topic, err)
	}
	return out
}

const (
	configTopic = "homeassistant/light/haigosmart/703e975dc388/config"
	stateTopic  = "haigosmart/light/703e975dc388/state"
	availTopic  = "haigosmart/light/703e975dc388/availability"
	setTopic    = "haigosmart/light/703e975dc388/set"
)

func TestAdoptedLampIsAnnounced(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")

	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(configTopic)
		return ok
	}) {
		t.Fatal("no discovery config was published")
	}
	cfg := r.retained(t, configTopic)
	if cfg["name"] != "headlamp" {
		t.Errorf("name = %v", cfg["name"])
	}
	if cfg["unique_id"] != "haigosmart_703e975dc388" {
		t.Errorf("unique_id = %v", cfg["unique_id"])
	}
	modes := cfg["supported_color_modes"].([]any)
	if len(modes) != 1 || modes[0] != "color_temp" {
		t.Errorf("supported_color_modes = %v; a white-only lamp must not offer colour", modes)
	}

	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(stateTopic)
		return ok
	}) {
		t.Fatal("no state was published")
	}
	state := r.retained(t, stateTopic)
	if state["state"] != "ON" {
		t.Errorf("state = %v", state["state"])
	}

	if !r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained(availTopic)
		return ok && string(m.Payload) == Online
	}) {
		t.Error("the lamp was never marked available")
	}
}

// An unadopted lamp must stay out of Home Assistant entirely (FR-015).
func TestUnadoptedLampIsNotAnnounced(t *testing.T) {
	r := newRig(t)
	time.Sleep(200 * time.Millisecond)
	if _, ok := r.broker.Retained(configTopic); ok {
		t.Error("a discovered but unadopted lamp was published to Home Assistant")
	}
}

func TestCommandFromHomeAssistantReachesTheLamp(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(stateTopic); return ok })

	r.broker.Publish(setTopic, []byte(`{"state":"ON","brightness":204}`))

	if !r.broker.WaitFor(5*time.Second, func() bool {
		b, _ := r.reg.View("703e975dc388")
		return b.State.Brightness == 80 // 204/255
	}) {
		t.Fatal("the command never reached the lamp")
	}
	// And the resulting state must be republished from the lamp's own report.
	if !r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained(stateTopic)
		if !ok {
			return false
		}
		var s map[string]any
		_ = json.Unmarshal(m.Payload, &s)
		return s["brightness"] == float64(204)
	}) {
		t.Error("the new state was not republished")
	}
}

func TestWarmthCommandFromHomeAssistant(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(stateTopic); return ok })

	r.broker.Publish(setTopic, []byte(`{"color_temp_kelvin":4600}`))
	if !r.broker.WaitFor(5*time.Second, func() bool {
		b, _ := r.reg.View("703e975dc388")
		return b.State.ColorTemp == 50
	}) {
		t.Fatal("the warmth command never took effect")
	}
}

// A malformed command must not drop the broker connection or affect anything.
func TestMalformedCommandIsHarmless(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(stateTopic); return ok })
	before, _ := r.reg.View("703e975dc388")

	for _, junk := range []string{`not json`, `{}`, `{"state":`, ``} {
		r.broker.Publish(setTopic, []byte(junk))
	}
	time.Sleep(300 * time.Millisecond)

	after, _ := r.reg.View("703e975dc388")
	if after.State.Power != before.State.Power ||
		after.State.Brightness != before.State.Brightness ||
		after.State.ColorTemp != before.State.ColorTemp {
		t.Errorf("a malformed command changed the lamp: %+v -> %+v", before.State, after.State)
	}
	// The connection must still work.
	r.broker.Publish(setTopic, []byte(`{"state":"OFF"}`))
	if !r.broker.WaitFor(5*time.Second, func() bool {
		b, _ := r.reg.View("703e975dc388")
		return !b.State.Power
	}) {
		t.Error("the bridge stopped working after a malformed command")
	}
}

// A change made at the wall switch must reach Home Assistant unprompted.
func TestWallSwitchChangeIsPublished(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(stateTopic); return ok })

	if err := r.fb.SetPower(false); err != nil {
		t.Fatal(err)
	}
	if !r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained(stateTopic)
		if !ok {
			return false
		}
		var s map[string]any
		_ = json.Unmarshal(m.Payload, &s)
		return s["state"] == "OFF"
	}) {
		t.Error("a wall-switch change never reached Home Assistant")
	}
}

func TestDisconnectMarksUnavailable(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained(availTopic)
		return ok && string(m.Payload) == Online
	})

	r.fb.Close()
	if !r.broker.WaitFor(5*time.Second, func() bool {
		m, ok := r.broker.Retained(availTopic)
		return ok && string(m.Payload) == Offline
	}) {
		t.Error("an unplugged lamp was not marked unavailable")
	}
}

func TestRenameRepublishesWithoutDuplicating(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(configTopic); return ok })
	first := r.retained(t, configTopic)

	if _, err := r.svc.Rename("703e975dc388", "hallway"); err != nil {
		t.Fatal(err)
	}
	if !r.broker.WaitFor(3*time.Second, func() bool {
		return r.retained(t, configTopic)["name"] == "hallway"
	}) {
		t.Fatal("the rename was not republished")
	}
	second := r.retained(t, configTopic)
	if second["unique_id"] != first["unique_id"] {
		t.Error("unique_id changed on rename; Home Assistant would create a second device")
	}
	// Only one config topic exists, so there is only one device.
	if got := r.retained(t, configTopic); got["object_id"] != "703e975dc388" {
		t.Errorf("object_id = %v", got["object_id"])
	}
}

func TestRemovePublishesAnEmptyConfig(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(configTopic); return ok })

	r.bridge.Remove("703e975dc388")
	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(configTopic)
		return !ok
	}) {
		t.Error("removing a lamp did not clear its retained config")
	}
}

func TestBridgePublishesItsOwnAvailability(t *testing.T) {
	r := newRig(t)
	if !r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained("haigosmart/status")
		return ok && string(m.Payload) == Online
	}) {
		t.Error("the bridge never announced itself as online")
	}
}

// Everything must be republished after a broker outage, so a Home Assistant
// restart or a broker restart recovers with nobody doing anything.
func TestRepublishesAfterBrokerReconnect(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.WaitFor(3*time.Second, func() bool { _, ok := r.broker.Retained(configTopic); return ok })

	r.broker.DropConnections()
	if !r.broker.WaitFor(15*time.Second, func() bool { return r.broker.Connects() >= 2 }) {
		t.Fatal("the bridge did not reconnect")
	}
	if !r.broker.WaitFor(5*time.Second, func() bool {
		m, ok := r.broker.Retained("haigosmart/status")
		return ok && string(m.Payload) == Online
	}) {
		t.Error("bridge availability was not republished after reconnecting")
	}
	if !r.broker.WaitFor(5*time.Second, func() bool {
		_, ok := r.broker.Retained(configTopic)
		return ok
	}) {
		t.Error("discovery was not republished after reconnecting")
	}
}

// A broker outage must leave the lamps entirely usable from the terminal.
func TestBrokerOutageDoesNotAffectTheLamp(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")
	r.broker.Close()
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.svc.SetBrightness(ctx, "703e975dc388", 42); err != nil {
		t.Fatalf("the lamp should still be controllable with the broker gone: %v", err)
	}
	if !waitUntil(5*time.Second, func() bool {
		b, _ := r.reg.View("703e975dc388")
		return b.State.Brightness == 42
	}) {
		t.Error("the lamp did not respond with the broker down")
	}
}

func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

var _ = bulb.Bulb{}

// The server must announce itself, or Home Assistant shows the lamps hanging
// off an "Unnamed device".
func TestServerAnnouncesItselfAsADevice(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")

	const bridgeConfig = "homeassistant/binary_sensor/haigosmart/status/config"
	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(bridgeConfig)
		return ok
	}) {
		t.Fatal("the server never announced itself")
	}
	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(configTopic)
		return ok
	}) {
		t.Fatal("the lamp was never announced")
	}
	bridge := r.retained(t, bridgeConfig)
	lamp := r.retained(t, configTopic)

	via := lamp["device"].(map[string]any)["via_device"]
	ids := bridge["device"].(map[string]any)["identifiers"].([]any)
	if ids[0] != via {
		t.Errorf("lamp points at via_device %v but the server declares %v", via, ids[0])
	}
	if name := bridge["device"].(map[string]any)["name"]; name != "haigosmart" {
		t.Errorf("the server's device name = %v", name)
	}
}
