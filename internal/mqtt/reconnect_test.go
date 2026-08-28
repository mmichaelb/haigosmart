package mqtt

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"haigosmart/internal/mqtt/mqtttest"
)

// A broker outage must be a temporary condition, not a permanent one. The lamps
// keep working from the terminal throughout; Home Assistant recovers on its own.
func TestReconnectsAfterTheBrokerDropsUs(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	var connects atomic.Int64
	c := New(Options{
		Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second,
		OnConnect: func() { connects.Add(1) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}
	if !b.WaitFor(time.Second, func() bool { return connects.Load() == 1 }) {
		t.Fatal("OnConnect did not fire on the first connect")
	}

	b.DropConnections()
	if !b.WaitFor(3*time.Second, func() bool { return !c.Connected() }) {
		t.Fatal("the client did not notice the drop")
	}
	if !b.WaitFor(10*time.Second, c.Connected) {
		t.Fatal("the client did not come back on its own")
	}
	if !b.WaitFor(3*time.Second, func() bool { return connects.Load() >= 2 }) {
		t.Error("OnConnect must fire on a reconnect too, or nothing republishes")
	}
	if b.Connects() < 2 {
		t.Errorf("broker saw %d connects, want at least 2", b.Connects())
	}
}

// Subscriptions must survive a reconnect, or commands from Home Assistant would
// silently stop arriving after any broker hiccup.
func TestSubscriptionsSurviveAReconnect(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c := New(Options{Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second})
	received := make(chan string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}
	if err := c.Subscribe("haigosmart/light/+/set", func(topic string, payload []byte) {
		received <- string(payload)
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	b.DropConnections()
	if !b.WaitFor(10*time.Second, func() bool { return c.Connected() && b.Connects() >= 2 }) {
		t.Fatal("did not reconnect")
	}
	time.Sleep(100 * time.Millisecond) // let the re-SUBSCRIBE land

	b.Publish("haigosmart/light/abc/set", []byte("after-reconnect"))
	select {
	case got := <-received:
		if got != "after-reconnect" {
			t.Errorf("received %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the subscription was not re-established after reconnecting")
	}
}

// A broker that is never reachable must be retried patiently, not spun on.
func TestUnreachableBrokerBacksOff(t *testing.T) {
	c := New(Options{Broker: "127.0.0.1:1", Logger: quiet(), KeepAlive: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	start := time.Now()
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Run(ctx) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}
	// With a one-second initial backoff, a 1.5s window admits very few attempts.
	// The point is that it returns on cancellation rather than spinning.
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Run took %s to stop after cancellation", elapsed)
	}
	if c.Connected() {
		t.Error("reported connected against an unreachable broker")
	}
}

// Cancelling the context must be a clean departure, so the broker discards the
// will rather than announcing us dead.
func TestCleanShutdownSuppressesTheWill(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c := New(Options{
		Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second,
		WillTopic: "haigosmart/status", WillPayload: []byte("offline"), WillRetain: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Run(ctx) }()
	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}

	cancel()
	<-done
	time.Sleep(100 * time.Millisecond)

	for _, m := range b.Messages() {
		if m.Topic == "haigosmart/status" && string(m.Payload) == "offline" {
			t.Error("a deliberate shutdown published the will; the broker should have discarded it")
		}
	}
}
