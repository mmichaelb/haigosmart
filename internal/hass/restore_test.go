package hass

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"haigosmart/internal/bulb"
	"haigosmart/internal/events"
	"haigosmart/internal/lights"
	"haigosmart/internal/mqtt"
	"haigosmart/internal/mqtt/mqtttest"
	"haigosmart/internal/registry"
)

// Starting the bridge against a registry full of persisted state must publish
// no state at all, and must not mark anything available.
//
// This is FR-010 and it is the requirement most integrations get wrong. The
// registry remembers what a lamp was doing last week, and publishing that would
// assert something unverified — while the whole scenario the owner described is
// "the lamp changed while we were not looking". An automation reading a restored
// value acts on a fiction.
func TestPersistedStateIsNeverPublished(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()

	bus := events.NewBus(quiet)
	reg := registry.New(nil)

	// A registry as it looks after a restart: the lamp is known, named, and its
	// last state is remembered — but it has not connected.
	reg.Upsert("703e975dc388", "192.168.1.5:1", bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}, time.Now())
	if _, err := reg.Rename("703e975dc388", "headlamp"); err != nil {
		t.Fatal(err)
	}
	reg.SetState("703e975dc388", bulb.LightState{Power: true, Brightness: 90, ColorTemp: 70}, time.Now())
	reg.Disconnect("703e975dc388", nil)

	svc := lights.New(reg, bus)
	var bridge *Bridge
	client := mqtt.New(mqtt.Options{
		Broker: broker.Addr(), Logger: quiet, KeepAlive: time.Second,
		OnConnect: func() { bridge.OnConnect() },
	})
	bridge = New(DefaultConfig(), svc, client, quiet)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()
	go func() { _ = bridge.Run(ctx) }()

	if !broker.WaitFor(3*time.Second, client.Connected) {
		t.Fatal("never connected")
	}
	// Discovery is expected — the lamp exists and Home Assistant should know
	// about it. State and availability are not.
	if !broker.WaitFor(3*time.Second, func() bool {
		_, ok := broker.Retained(configTopic)
		return ok
	}) {
		t.Fatal("a known lamp should still be announced")
	}
	time.Sleep(300 * time.Millisecond)

	if m, ok := broker.Retained(stateTopic); ok {
		t.Errorf("remembered state was published: %s", m.Payload)
	}
	if m, ok := broker.Retained(availTopic); ok && string(m.Payload) == Online {
		t.Error("a lamp that has not connected was marked available")
	}
}

// The counterpart: once the lamp does report, state and availability appear.
func TestStateAppearsOnlyAfterTheLampReports(t *testing.T) {
	r := newRig(t)
	r.adopt(t, "headlamp")

	if !r.broker.WaitFor(3*time.Second, func() bool {
		_, ok := r.broker.Retained(stateTopic)
		return ok
	}) {
		t.Fatal("state was never published for a lamp that has reported")
	}
	if !r.broker.WaitFor(3*time.Second, func() bool {
		m, ok := r.broker.Retained(availTopic)
		return ok && string(m.Payload) == Online
	}) {
		t.Fatal("a reporting lamp was never marked available")
	}
}

// After a disconnect the lamp must go unavailable and must not be re-marked
// available until it speaks again.
func TestAvailabilityRequiresAFreshReport(t *testing.T) {
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
		t.Fatal("the lamp was not marked unavailable")
	}

	// Nothing may flip it back to online without the lamp reporting.
	time.Sleep(300 * time.Millisecond)
	m, _ := r.broker.Retained(availTopic)
	if string(m.Payload) != Offline {
		t.Error("availability returned without the lamp reporting anything")
	}
}
