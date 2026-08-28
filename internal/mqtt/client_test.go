package mqtt

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"haigosmart/internal/mqtt/mqtttest"
	"haigosmart/internal/protocol"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// start brings up a stub broker and a connected client.
func start(t *testing.T, opts Options) (*Broker, *Client) {
	t.Helper()
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)

	opts.Broker = b.Addr()
	opts.Logger = quiet()
	if opts.KeepAlive == 0 {
		opts.KeepAlive = time.Second
	}
	c := New(opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("client never connected")
	}
	return b, c
}

// Broker is an alias so the helper reads cleanly.
type Broker = mqtttest.Broker

func TestPublish(t *testing.T) {
	b, c := start(t, Options{ClientID: "haigosmart"})

	if err := c.Publish("haigosmart/light/x/state", []byte(`{"state":"ON"}`), false); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !b.WaitFor(3*time.Second, func() bool { return len(b.Messages()) > 0 }) {
		t.Fatal("the broker never saw the message")
	}
	got := b.Messages()[0]
	if got.Topic != "haigosmart/light/x/state" || string(got.Payload) != `{"state":"ON"}` {
		t.Errorf("got %+v", got)
	}
	if got.Retained {
		t.Error("this message was not published retained")
	}
}

func TestPublishRetained(t *testing.T) {
	b, c := start(t, Options{})
	if err := c.Publish("haigosmart/status", []byte("online"), true); err != nil {
		t.Fatal(err)
	}
	if !b.WaitFor(3*time.Second, func() bool { _, ok := b.Retained("haigosmart/status"); return ok }) {
		t.Fatal("the message was not retained")
	}
	m, _ := b.Retained("haigosmart/status")
	if string(m.Payload) != "online" {
		t.Errorf("retained payload = %q", m.Payload)
	}
}

// An empty retained payload clears a topic, which is how a device is removed.
func TestEmptyRetainedPayloadClears(t *testing.T) {
	b, c := start(t, Options{})
	if err := c.Publish("homeassistant/light/x/config", []byte(`{"a":1}`), true); err != nil {
		t.Fatal(err)
	}
	b.WaitFor(3*time.Second, func() bool { _, ok := b.Retained("homeassistant/light/x/config"); return ok })

	if err := c.Publish("homeassistant/light/x/config", nil, true); err != nil {
		t.Fatal(err)
	}
	if !b.WaitFor(3*time.Second, func() bool { _, ok := b.Retained("homeassistant/light/x/config"); return !ok }) {
		t.Error("an empty retained payload should clear the topic")
	}
}

func TestPublishQoS1IsAcknowledged(t *testing.T) {
	b, c := start(t, Options{})
	if err := c.PublishQoS1("haigosmart/light/x/state", []byte("x"), false); err != nil {
		t.Fatalf("PublishQoS1: %v", err)
	}
	if !b.WaitFor(3*time.Second, func() bool { return len(b.Messages()) > 0 }) {
		t.Fatal("the broker never saw the message")
	}
	// The client must still be usable, which it would not be if the PUBACK had
	// been mishandled and left bytes in the stream.
	if err := c.Publish("haigosmart/light/x/state", []byte("y"), false); err != nil {
		t.Fatalf("the connection did not survive a QoS 1 exchange: %v", err)
	}
}

func TestSubscribeReceives(t *testing.T) {
	b, c := start(t, Options{})
	received := make(chan string, 4)
	if err := c.Subscribe("haigosmart/light/+/set", func(topic string, payload []byte) {
		received <- topic + " " + string(payload)
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let the SUBSCRIBE land
	b.Publish("haigosmart/light/abc/set", []byte(`{"state":"OFF"}`))

	select {
	case got := <-received:
		if got != `haigosmart/light/abc/set {"state":"OFF"}` {
			t.Errorf("received %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the handler never fired")
	}
}

func TestSubscribeBeforeConnectIsEstablishedOnConnect(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c := New(Options{Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second})
	received := make(chan struct{}, 1)
	// Subscribing before Run must not be an error; it is established when the
	// connection comes up.
	if err := c.Subscribe("a/b", func(string, []byte) { received <- struct{}{} }); err != nil {
		t.Fatalf("Subscribe before connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	if !b.WaitFor(3*time.Second, c.Connected) {
		t.Fatal("never connected")
	}
	time.Sleep(50 * time.Millisecond)
	b.Publish("a/b", []byte("x"))
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("the pre-registered subscription was not established")
	}
}

func TestCredentialsArePassedThrough(t *testing.T) {
	b, _ := start(t, Options{Username: "ha", Password: "s3cret"})
	creds := b.Credentials()
	if len(creds) != 1 || creds[0][0] != "ha" || creds[0][1] != "s3cret" {
		t.Errorf("broker saw %v", creds)
	}
}

// A broker that rejects us must not look like a network failure.
func TestRejectedConnectionReportsWhy(t *testing.T) {
	b, err := mqtttest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.RefuseWith(protocol.ConnackBadCredentials)

	c := New(Options{Broker: b.Addr(), Logger: quiet(), KeepAlive: time.Second})
	err = c.session(context.Background())
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if want := "broker rejected the username or password"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to explain %q", err, want)
	}
}

func TestPublishWhileDisconnected(t *testing.T) {
	c := New(Options{Broker: "127.0.0.1:1", Logger: quiet()})
	if err := c.Publish("a/b", []byte("x"), false); err != ErrNotConnected {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestTopicMatches(t *testing.T) {
	tests := []struct {
		filter, topic string
		want          bool
	}{
		{"a/b", "a/b", true},
		{"a/b", "a/c", false},
		{"a/+/c", "a/b/c", true},
		{"a/+/c", "a/b/d", false},
		{"a/+", "a/b/c", false},
		{"a/#", "a/b/c", true},
		// MQTT 3.1.1 section 4.7.1.2: "sport/#" also matches the singular
		// "sport", because # includes the parent level.
		{"a/#", "a", true},
		{"#", "anything/at/all", true},
		{"haigosmart/light/+/set", "haigosmart/light/703e975dc388/set", true},
		{"haigosmart/light/+/set", "haigosmart/light/703e975dc388/state", false},
	}
	for _, tc := range tests {
		if got := topicMatches(tc.filter, tc.topic); got != tc.want {
			t.Errorf("topicMatches(%q, %q) = %v, want %v", tc.filter, tc.topic, got, tc.want)
		}
	}
}
