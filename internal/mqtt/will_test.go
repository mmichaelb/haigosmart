package mqtt

import (
	"context"
	"testing"
	"time"

	"haigosmart/internal/mqtt/mqtttest"
)

// The last will is the only mechanism that reports a crash or a pulled cable.
// A shutdown handler cannot cover those, which is exactly when Home Assistant
// most needs to know the lamps are unreachable.
func TestWillIsPublishedWhenTheConnectionDies(t *testing.T) {
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
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}

	// Kill the connection without a DISCONNECT — a crash, not a shutdown.
	b.DropConnections()

	if !b.WaitFor(3*time.Second, func() bool {
		m, ok := b.Retained("haigosmart/status")
		return ok && string(m.Payload) == "offline"
	}) {
		t.Fatal("the will was not published when the connection died")
	}
	m, _ := b.Retained("haigosmart/status")
	if !m.Retained {
		t.Error("the will must be retained, or a later subscriber never learns we are gone")
	}
}

// A will registered alongside credentials must not corrupt either.
func TestWillWithCredentials(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c := New(Options{
		Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second,
		Username: "ha", Password: "s3cret",
		WillTopic: "haigosmart/status", WillPayload: []byte("offline"), WillRetain: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()
	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}
	creds := b.Credentials()
	if len(creds) != 1 || creds[0][0] != "ha" || creds[0][1] != "s3cret" {
		t.Fatalf("credentials mangled by the will: %v", creds)
	}
	b.DropConnections()
	if !b.WaitFor(3*time.Second, func() bool {
		m, ok := b.Retained("haigosmart/status")
		return ok && string(m.Payload) == "offline"
	}) {
		t.Error("the will did not survive being sent alongside credentials")
	}
}
