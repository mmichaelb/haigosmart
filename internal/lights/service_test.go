package lights

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/bulb/fakebulb"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/registry"
	"github.com/mmichaelb/haigosmart/internal/server"
)

type harness struct {
	svc *Service
	reg *registry.Registry
	fb  *fakebulb.Bulb
}

func newHarness(t *testing.T, opts fakebulb.Options) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := server.New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()

	if opts.Version == "" {
		opts.Version = "aigo_light_cct_v4.0.0"
	}
	if opts.DeviceName == "" {
		opts.DeviceName = "703e975dc388"
	}
	fb, err := fakebulb.Dial(ln.Addr().String(), opts)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fb.Close()
		cancel()
		<-done
	})
	h := &harness{svc: New(reg, bus), reg: reg, fb: fb}
	waitFor(t, "the bulb to register with capabilities", func() bool {
		b, ok := reg.View(fb.DeviceID())
		return ok && b.Capabilities.Known && b.State.Brightness == 30
	})
	return h
}

func (h *harness) adopt(t *testing.T, name string) {
	t.Helper()
	if _, err := h.reg.Rename(h.fb.DeviceID(), name); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) state(t *testing.T) bulb.LightState {
	t.Helper()
	b, ok := h.reg.View(h.fb.DeviceID())
	if !ok {
		t.Fatal("bulb vanished")
	}
	return b.State
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestOperationsHappyPath(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	id := h.fb.DeviceID()

	tests := []struct {
		name    string
		op      func() error
		settled func(bulb.LightState) bool
	}{
		{"power off", func() error { return h.svc.SetPower(ctx(t), id, false) },
			func(s bulb.LightState) bool { return !s.Power }},
		{"power on", func() error { return h.svc.SetPower(ctx(t), id, true) },
			func(s bulb.LightState) bool { return s.Power }},
		{"brightness", func() error { return h.svc.SetBrightness(ctx(t), id, 80) },
			func(s bulb.LightState) bool { return s.Brightness == 80 }},
		{"colour temperature", func() error { return h.svc.SetColorTemp(ctx(t), id, 55) },
			func(s bulb.LightState) bool { return s.ColorTemp == 55 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.op(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			waitFor(t, "the lamp to report the change", func() bool { return tc.settled(h.state(t)) })
		})
	}
}

func TestApplyCombinesFields(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	on := true
	bright := uint8(65)
	temp := uint8(20)
	if err := h.svc.Apply(ctx(t), h.fb.DeviceID(), Change{Power: &on, Brightness: &bright, ColorTemp: &temp}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	waitFor(t, "all three attributes", func() bool {
		s := h.state(t)
		return s.Power && s.Brightness == 65 && s.ColorTemp == 20
	})
}

// A nil field means "leave alone". This is what separates a Home Assistant
// command that only sets brightness from one that also changes power.
func TestNilFieldsAreLeftAlone(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	id := h.fb.DeviceID()

	if err := h.svc.SetColorTemp(ctx(t), id, 42); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the warmth change", func() bool { return h.state(t).ColorTemp == 42 })
	before := h.state(t)

	if err := h.svc.SetBrightness(ctx(t), id, 70); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the brightness change", func() bool { return h.state(t).Brightness == 70 })
	if got := h.state(t).ColorTemp; got != before.ColorTemp {
		t.Errorf("warmth changed to %d when only brightness was set", got)
	}
}

func TestValidation(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	id := h.fb.DeviceID()

	tests := []struct {
		name    string
		op      func() error
		wantIs  error
		wantMsg string
	}{
		{
			name:   "brightness above the maximum",
			op:     func() error { return h.svc.SetBrightness(ctx(t), id, 150) },
			wantIs: ErrOutOfRange, wantMsg: "brightness must be 0-100, got 150",
		},
		{
			name:   "warmth above the maximum",
			op:     func() error { return h.svc.SetColorTemp(ctx(t), id, 101) },
			wantIs: ErrOutOfRange,
		},
		{
			name:   "unknown bulb",
			op:     func() error { return h.svc.SetPower(ctx(t), "nosuchbulb", true) },
			wantIs: ErrUnknownBulb,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op()
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("err = %v, want one matching %v", err, tc.wantIs)
			}
			if tc.wantMsg != "" && err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// Brightness 0 and 100 are the boundaries and both are legal.
func TestBrightnessBounds(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	id := h.fb.DeviceID()

	if err := h.svc.SetBrightness(ctx(t), id, 100); err != nil {
		t.Fatalf("100 should be accepted: %v", err)
	}
	waitFor(t, "full brightness", func() bool { return h.state(t).Brightness == 100 })

	// Zero is accepted and means off, rather than being rejected as below the floor.
	if err := h.svc.SetBrightness(ctx(t), id, 0); err != nil {
		t.Fatalf("0 should be accepted as off: %v", err)
	}
	waitFor(t, "the lamp to go off", func() bool { return !h.state(t).Power })
}

func TestBelowBrightnessFloorIsRefused(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	// The captured hardware reports a floor of 1, so nothing is below it; force
	// a higher floor to exercise the rule.
	h.reg.SetCapabilities(h.fb.DeviceID(), bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 10})
	err := h.svc.SetBrightness(ctx(t), h.fb.DeviceID(), 5)
	if !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("err = %v, want ErrOutOfRange", err)
	}
}

func TestUnsupportedCapabilityIsRefused(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	h.reg.SetCapabilities(h.fb.DeviceID(), bulb.Capabilities{Known: true, ColorTemp: false, MinBrightness: 1})
	err := h.svc.SetColorTemp(ctx(t), h.fb.DeviceID(), 50)
	if !errors.Is(err, bulb.ErrUnsupported) {
		t.Fatalf("err = %v, want bulb.ErrUnsupported", err)
	}
}

// A lamp whose capabilities were never determined gets the benefit of the
// doubt: the command is attempted rather than pre-refused.
func TestUndeterminedCapabilitiesAreAttempted(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	h.reg.SetCapabilities(h.fb.DeviceID(), bulb.Capabilities{Known: false, MinBrightness: 1})
	if err := h.svc.SetColorTemp(ctx(t), h.fb.DeviceID(), 50); err != nil {
		t.Fatalf("an undetermined lamp should be attempted, got %v", err)
	}
}

func TestNotAdoptedIsRefused(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	// Deliberately not adopted.
	err := h.svc.SetPower(ctx(t), h.fb.DeviceID(), true)
	if !errors.Is(err, ErrNotAdopted) {
		t.Fatalf("err = %v, want ErrNotAdopted", err)
	}
}

func TestDisconnectedIsRefused(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	id := h.fb.DeviceID()
	h.fb.Close()
	waitFor(t, "the disconnect", func() bool {
		b, _ := h.reg.View(id)
		return b.Status == bulb.Disconnected
	})
	if err := h.svc.SetPower(ctx(t), id, true); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// Unconfirmed is not a failure anywhere. Callers distinguish it with errors.Is.
func TestSilentBulbYieldsUnconfirmed(t *testing.T) {
	h := newHarness(t, fakebulb.Options{DeviceName: "silent", Silent: true})
	h.adopt(t, "kitchen")
	h.svc.SetTimeout(150 * time.Millisecond)
	err := h.svc.SetBrightness(ctx(t), "silent", 77)
	if !errors.Is(err, bulb.ErrUnconfirmed) {
		t.Fatalf("err = %v, want bulb.ErrUnconfirmed", err)
	}
}

func TestNoOpSendsNothing(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	for len(h.fb.Commands()) > 0 {
		<-h.fb.Commands()
	}
	current := h.state(t)
	if err := h.svc.SetBrightness(ctx(t), h.fb.DeviceID(), current.Brightness); err != nil {
		t.Fatalf("a no-op should succeed immediately: %v", err)
	}
	select {
	case props := <-h.fb.Commands():
		t.Errorf("a no-op reached the lamp: %v", props)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSnapshotAndGet(t *testing.T) {
	h := newHarness(t, fakebulb.Options{})
	h.adopt(t, "kitchen")
	if got := h.svc.Snapshot(); len(got) != 1 || got[0].Name != "kitchen" {
		t.Errorf("snapshot = %v", got)
	}
	if _, err := h.svc.Get(h.fb.DeviceID()); err != nil {
		t.Errorf("Get by device id: %v", err)
	}
	// No prefix matching here: integrations address lamps exactly.
	if _, err := h.svc.Get("703e"); !errors.Is(err, ErrUnknownBulb) {
		t.Errorf("a prefix should not resolve in the service layer, got %v", err)
	}
	if _, err := h.svc.Get("kitchen"); !errors.Is(err, ErrUnknownBulb) {
		t.Errorf("a name should not resolve in the service layer, got %v", err)
	}
}

func TestChangeIsEmpty(t *testing.T) {
	on := true
	if !(Change{}).IsEmpty() {
		t.Error("a zero Change is empty")
	}
	if (Change{Power: &on}).IsEmpty() {
		t.Error("a Change with a field set is not empty")
	}
}
