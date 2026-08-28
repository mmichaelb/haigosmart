package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"haigosmart/internal/bulb/fakebulb"
	"haigosmart/internal/events"
	"haigosmart/internal/registry"
)

// admitHarness is newHarness with an admission predicate and a clock the test
// controls. The assertions below go through a real CONNECT and a real CONNACK
// rather than calling the predicate: a double built from assumptions agrees
// with those assumptions.
type admitHarness struct {
	harness
	srv     *Server
	advance func(time.Duration)

	seen []events.Event
}

func newAdmitHarness(t *testing.T, allowed ...string) *admitHarness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	set := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		set[id] = true
	}

	var mu sync.Mutex
	clock := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)

	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, "")
	srv.Admit = func(deviceID string) bool { return set[deviceID] }
	srv.nowFn = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &admitHarness{
		harness: harness{addr: ln.Addr().String(), reg: reg, bus: bus, sub: bus.Subscribe(256)},
		srv:     srv,
		advance: func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			clock = clock.Add(d)
		},
	}
}

// rejections drains whatever has arrived into seen and returns every rejection
// so far. Draining is destructive, so the accumulation has to live here rather
// than in each assertion.
func (h *admitHarness) rejections() []events.Event {
	for len(h.sub.Events()) > 0 {
		h.seen = append(h.seen, <-h.sub.Events())
	}
	var out []events.Event
	for _, e := range h.seen {
		if e.Kind == events.Rejected {
			out = append(out, e)
		}
	}
	return out
}

// TestAdmittedBulbIsServed: the configured lamp behaves exactly as before.
func TestAdmittedBulbIsServed(t *testing.T) {
	h := newAdmitHarness(t, "known")
	b, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "known"})
	if err != nil {
		t.Fatalf("a configured lamp was refused: %v", err)
	}
	defer b.Close()

	waitFor(t, "the lamp to appear in the registry", func() bool {
		_, ok := h.reg.View("known")
		return ok
	})
}

// TestUnknownBulbIsRefused: CONNACK 0x05 and a closed connection, so the lamp
// is told why rather than left guessing at a dropped socket.
func TestUnknownBulbIsRefused(t *testing.T) {
	h := newAdmitHarness(t, "known")

	_, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "stranger"})
	if err == nil {
		t.Fatal("an unconfigured lamp was accepted")
	}
	if !strings.Contains(err.Error(), "not authorised") && !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("refusal reported as %q, want the CONNACK reason", err)
	}
}

// TestRefusedBulbLeavesNothingBehind is FR-017: nothing about a rejected lamp
// may survive, in memory or on disk.
func TestRefusedBulbLeavesNothingBehind(t *testing.T) {
	h := newAdmitHarness(t, "known")
	_, _ = fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "stranger"})

	waitFor(t, "the rejection to be published", func() bool {
		return len(h.rejections()) > 0
	})
	if _, ok := h.reg.View("stranger"); ok {
		t.Error("the rejected lamp is in the registry")
	}
	for _, b := range h.reg.List() {
		if b.DeviceID == "stranger" {
			t.Error("the rejected lamp is in the registry listing")
		}
	}
}

// TestRejectionIsRateLimited is FR-017a: a refused lamp reconnects forever, so
// an unrate-limited record is an unbounded log.
func TestRejectionIsRateLimited(t *testing.T) {
	h := newAdmitHarness(t, "known")

	for range 20 {
		_, _ = fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "stranger"})
	}
	waitFor(t, "the first rejection to be published", func() bool {
		return len(h.rejections()) >= 1
	})
	// Give any further records a chance to arrive before counting.
	time.Sleep(50 * time.Millisecond)
	if got := len(h.rejections()); got != 1 {
		t.Errorf("%d rejection records inside the window, want 1", got)
	}

	h.advance(rejectionWindow + time.Second)
	_, _ = fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "stranger"})
	waitFor(t, "the suppressed count to be reported", func() bool {
		return len(h.rejections()) >= 2
	})

	all := h.rejections()
	last := all[len(all)-1]
	if !strings.Contains(last.Detail, "attempts") {
		t.Errorf("repeat record %q does not report the suppressed attempts", last.Detail)
	}
}

// TestReconnectReportsStateEvenWhenNothingChanged is the regression for the
// defect found on hardware on 2026-08-28: a lamp whose state survived a restart
// unchanged never announced itself to Home Assistant, which kept showing it
// unavailable forever.
//
// The bridge treats a state report as the proof that a lamp is really there.
// The server used to publish that event only when a value differed, so a lamp
// reconnecting and reporting exactly what the registry already held — the normal
// case once lamps are declared from a persisted registry — was silent.
func TestReconnectReportsStateEvenWhenNothingChanged(t *testing.T) {
	h := newAdmitHarness(t, "known")

	b, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "known"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitFor(t, "the first state report", func() bool { return h.stateReports() >= 1 })
	first := h.stateReports()
	b.Close()

	waitFor(t, "the disconnect to land", func() bool {
		bulb, ok := h.reg.View("known")
		return ok && bulb.Status != 2 // not Connected
	})

	// Reconnect and post exactly the same state again.
	b2, err := fakebulb.Dial(h.addr, fakebulb.Options{DeviceName: "known"})
	if err != nil {
		t.Fatalf("redial: %v", err)
	}
	defer b2.Close()

	waitFor(t, "a state report on the second connection", func() bool {
		return h.stateReports() > first
	})
}

func (h *admitHarness) stateReports() int {
	for len(h.sub.Events()) > 0 {
		h.seen = append(h.seen, <-h.sub.Events())
	}
	n := 0
	for _, e := range h.seen {
		if e.Kind == events.StateChanged {
			n++
		}
	}
	return n
}
