package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
	"github.com/mmichaelb/haigosmart/internal/bulb/fakebulb"
	"github.com/mmichaelb/haigosmart/internal/events"
	"github.com/mmichaelb/haigosmart/internal/registry"
)

// TestSoak runs the 30 concurrent bulbs the spec requires (FR-020, SC-005).
// The full seven-day run is an operator exercise; this is the same shape,
// compressed, so a regression is caught in CI rather than on day six.
func TestSoak(t *testing.T) {
	if testing.Short() {
		t.Skip("soak test skipped in -short mode")
	}
	const bulbs = 30

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg := registry.New(nil)
	srv := New(reg, bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	defer func() {
		cancel()
		<-done
	}()

	// A subscriber that never drains, to prove a stalled display cannot stall
	// the network path.
	stalled := bus.Subscribe(4)
	defer stalled.Close()

	fakes := make([]*fakebulb.Bulb, 0, bulbs)
	for i := range bulbs {
		fb, err := fakebulb.Dial(ln.Addr().String(), fakebulb.Options{
			DeviceName: fmt.Sprintf("bulb%02d", i),
			Version:    "aigo_light_cct_v4.0.0",
		})
		if err != nil {
			t.Fatalf("bulb %d could not connect: %v", i, err)
		}
		defer fb.Close()
		fakes = append(fakes, fb)
	}

	waitFor(t, "all bulbs to register", func() bool { return len(reg.List()) == bulbs })
	for i, fb := range fakes {
		if _, err := reg.Rename(fb.DeviceID(), fmt.Sprintf("bulb-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Drive traffic: keep-alives and state changes from every bulb at once.
	const rounds = 20
	for round := range rounds {
		for _, fb := range fakes {
			if err := fb.Ping(); err != nil {
				t.Fatalf("keep-alive failed mid-soak: %v", err)
			}
			if err := fb.PostChange(map[string]any{"Brightness": round%100 + 1}, ""); err != nil {
				t.Fatalf("state report failed mid-soak: %v", err)
			}
		}
	}

	waitFor(t, "the last round of reports to land", func() bool {
		want := uint8(rounds-1)%100 + 1
		for _, fb := range fakes {
			b, ok := reg.View(fb.DeviceID())
			if !ok || b.State.Brightness != want {
				return false
			}
		}
		return true
	})

	// Nothing may have dropped off.
	for _, fb := range fakes {
		b, _ := reg.View(fb.DeviceID())
		if b.Status != bulb.Connected {
			t.Errorf("%s is %v after the soak; no bulb should disconnect unexplained", b.Name, b.Status)
		}
		if reg.Driver(fb.DeviceID()) == nil {
			t.Errorf("%s lost its connection", b.Name)
		}
	}
	if got := len(reg.List()); got != bulbs {
		t.Errorf("registry holds %d bulbs, want %d: reconnects must not duplicate entries", got, bulbs)
	}

	// Every bulb must still take a command after the load.
	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cmdCancel()
	for _, fb := range fakes {
		driver := reg.Driver(fb.DeviceID())
		if driver == nil {
			t.Fatalf("%s has no driver", fb.DeviceID())
		}
		if err := driver.Apply(cmdCtx, bulb.LightState{Power: true, Brightness: 42}); err != nil {
			t.Errorf("%s stopped accepting commands: %v", fb.DeviceID(), err)
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	// The stalled subscriber must have shed events rather than growing without
	// bound; that is the whole reason the queue drops.
	if stalled.Dropped() == 0 {
		t.Error("a subscriber that never drains should have dropped events")
	}
	t.Logf("heap after soak: %d KiB (%d bulbs, %d rounds, %d display drops)",
		after.HeapAlloc/1024, bulbs, rounds, stalled.Dropped())
}
