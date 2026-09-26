package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb/fakebulb"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/protocol"
	"github.com/mmichaelb/haigosmart/internal/registry"
)

// TestSessionCloseOnShutdownIsHardReset proves the fix at the mechanism level
// (spec 006-bulb-reconnect-restart, FR-001): a bulb connection closed by
// server shutdown must be a hard reset (RST), not a graceful close, since a
// graceful close is what today leaves real firmware needing a manual
// power-cycle to reconnect. See contracts/shutdown-close-behavior.md.
func TestSessionCloseOnShutdownIsHardReset(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Complete the handshake so the server has a registered session to close,
	// rather than a half-open connection it would drop for an unrelated reason.
	connect := protocol.EncodeConnect(protocol.ConnectOptions{
		ClientID:  "a1GGnyln558.703e975dc388|securemode=2,signmethod=hmacsha256|",
		Username:  "703e975dc388&a1GGnyln558",
		Password:  "unused",
		KeepAlive: 120,
	})
	if _, err := conn.Write(connect); err != nil {
		t.Fatalf("writing CONNECT: %v", err)
	}
	ack := make([]byte, 4)
	if _, err := io.ReadFull(conn, ack); err != nil {
		t.Fatalf("reading CONNACK: %v", err)
	}

	// Trigger the shutdown path: cancel the server's context, the same as a
	// SIGTERM/SIGINT would, which drives session.serve's context.AfterFunc to
	// call session.Close() (internal/server/session.go:157-159).
	cancel()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 16)
	_, readErr := conn.Read(buf)

	if readErr == nil {
		t.Fatal("expected the connection to be closed after shutdown, got a successful read")
	}
	if errors.Is(readErr, io.EOF) {
		t.Fatalf("shutdown closed the connection gracefully (EOF); want a hard reset (ECONNRESET): %v", readErr)
	}
	if !errors.Is(readErr, syscall.ECONNRESET) {
		t.Fatalf("want a reset-style error (syscall.ECONNRESET), got: %v", readErr)
	}
}

// TestShutdownDisconnectStillReportsServerShuttingDown guards data-model.md's
// claim (spec 006-bulb-reconnect-restart, FR-003/FR-004): switching the
// shutdown close to a hard reset must not change the operator-visible event
// text. reportDisconnect classifies the local close via errors.Is(err,
// net.ErrClosed) / errors.Is(err, context.Canceled)
// (internal/server/session.go:305-323), which observes the *local* close and
// is unaffected by SO_LINGER, which only changes what the *peer* sees.
func TestShutdownDisconnectStillReportsServerShuttingDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	t.Cleanup(func() { <-done })

	sub := bus.Subscribe(64)
	defer sub.Close()

	fb, err := fakebulb.Dial(ln.Addr().String(), fakebulb.Options{Version: "aigo_light_cct_v4.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()

	// Wait for the bulb's initial state report, not just registration: the
	// server still has a reply in flight for it (handlePropertyPost writes a
	// PropertyPostReply after publishing StateChanged). Canceling before that
	// write lands races the shutdown close against it and produces a
	// ProtocolError instead of the Disconnected this test checks — a
	// pre-existing timing window, not something this feature introduces.
	// Draining up to the StateChanged event, then a short pause for the
	// in-flight reply write to finish, avoids it deterministically enough for
	// a test.
	deadline := time.After(3 * time.Second)
drain:
	for {
		select {
		case e := <-sub.Events():
			if e.Kind == events.StateChanged && e.DeviceID == fb.DeviceID() {
				break drain
			}
		case <-deadline:
			t.Fatal("timed out waiting for the initial state report")
		}
	}
	time.Sleep(50 * time.Millisecond)

	cancel() // the shutdown path, same as SIGTERM/SIGINT

	deadline = time.After(3 * time.Second)
	for {
		select {
		case e := <-sub.Events():
			if e.Kind == events.Disconnected && e.DeviceID == fb.DeviceID() {
				if e.Detail != "server shutting down" {
					t.Fatalf("shutdown disconnect detail = %q, want %q", e.Detail, "server shutting down")
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the shutdown Disconnected event")
		}
	}
}
