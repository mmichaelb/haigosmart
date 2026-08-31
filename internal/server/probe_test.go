package server

import (
	"net"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb/fakebulb"
	"github.com/mmichaelb/haigosmart/internal/events"
)

// TestProbeConnectionIsNotAProtocolError is the regression for the noise a
// Kubernetes `tcpSocket` probe produced: the kubelet opens a connection, closes
// it without sending anything, and repeats every few seconds forever. Each one
// was reported as a protocol error, so an idle unattended instance filled its
// record stream with them.
//
// A connection that sends no bytes has violated no protocol — nothing was said,
// so nothing can be malformed. Port scanners and load-balancer checks look
// exactly the same and deserve the same silence.
func TestProbeConnectionIsNotAProtocolError(t *testing.T) {
	h := newHarness(t)

	for range 5 {
		conn, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conn.Close()
	}

	// Give the server time to have published anything it was going to.
	time.Sleep(100 * time.Millisecond)

	for len(h.sub.Events()) > 0 {
		e := <-h.sub.Events()
		if e.Kind == events.ProtocolError {
			t.Errorf("a probe that sent nothing was reported as a protocol error: %q", e.Detail)
		}
	}
}

// TestRealBulbStillWorksAfterProbes: silencing probes must not silence bulbs.
// The probe and the bulb arrive on the same port and are told apart only by
// whether anything was sent.
func TestRealBulbStillWorksAfterProbes(t *testing.T) {
	h := newHarness(t)

	for range 3 {
		conn, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conn.Close()
	}

	b, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "a1b2c3d4"})
	if err != nil {
		t.Fatalf("a bulb was refused after probe traffic: %v", err)
	}
	defer b.Close()

	waitFor(t, "the bulb to register", func() bool {
		_, ok := h.reg.View("a1b2c3d4")
		return ok
	})
}
