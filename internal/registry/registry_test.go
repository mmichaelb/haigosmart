package registry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

func testCaps() bulb.Capabilities {
	return bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}
}

func TestUpsertDiscoversThenReuses(t *testing.T) {
	r := New(nil)
	now := time.Now()

	b, isNew := r.Upsert("703e975dc388", "192.168.1.5:1234", testCaps(), now)
	if !isNew {
		t.Fatal("first connect should be new")
	}
	if b.Status != bulb.Discovered {
		t.Errorf("status = %v, want discovered", b.Status)
	}
	if b.Name != b.DeviceID {
		t.Errorf("name should default to the device id, got %q", b.Name)
	}

	if _, err := r.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	// A power cycle must rejoin the same entry, not create a second one.
	again, isNew := r.Upsert("703e975dc388", "192.168.1.5:5678", testCaps(), now.Add(time.Minute))
	if isNew {
		t.Error("a returning bulb must not be reported as new")
	}
	if again.DeviceID != b.DeviceID {
		t.Error("a returning bulb must rejoin its existing entry")
	}
	if again.Name != "kitchen" {
		t.Errorf("name lost across reconnect: %q", again.Name)
	}
	if got := len(r.List()); got != 1 {
		t.Errorf("registry has %d bulbs, want 1", got)
	}
}

func TestUpsertKeepsKnownCapabilitiesOverUnknown(t *testing.T) {
	r := New(nil)
	now := time.Now()
	b, _ := r.Upsert("dev", "addr", testCaps(), now)
	// A later connect that could not classify the bulb must not erase what we
	// already knew.
	r.Upsert("dev", "addr", bulb.Capabilities{MinBrightness: 1}, now)
	got, _ := r.View(b.DeviceID)
	if !got.Capabilities.Known || !got.Capabilities.ColorTemp {
		t.Errorf("capabilities regressed to %+v", got.Capabilities)
	}
}

func TestResolve(t *testing.T) {
	r := New(nil)
	now := time.Now()
	kitchen, _ := r.Upsert("aaa111", "a", testCaps(), now)
	if _, err := r.Rename(kitchen.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	kids, _ := r.Upsert("bbb222", "b", testCaps(), now)
	if _, err := r.Rename(kids.DeviceID, "kids-room"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		target  string
		want    bulb.Bulb
		wantErr any
	}{
		{name: "exact name", target: "kitchen", want: kitchen},
		{name: "exact device id", target: "bbb222", want: kids},
		{name: "unique name prefix", target: "kit", want: kitchen},
		{name: "unique id prefix", target: "bbb", want: kids},
		{name: "case insensitive prefix", target: "KIDS", want: kids},
		{name: "ambiguous prefix", target: "k", wantErr: AmbiguousTargetError{}},
		{name: "unknown", target: "garage", wantErr: UnknownTargetError{}},
		{name: "empty", target: "", wantErr: UnknownTargetError{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Resolve(tc.target)
			switch want := tc.wantErr.(type) {
			case AmbiguousTargetError:
				var e AmbiguousTargetError
				if !errors.As(err, &e) {
					t.Fatalf("err = %v, want ambiguous", err)
				}
				if len(e.Candidates) != 2 {
					t.Errorf("candidates = %v, want both bulbs listed", e.Candidates)
				}
			case UnknownTargetError:
				var e UnknownTargetError
				if !errors.As(err, &e) {
					t.Fatalf("err = %v, want unknown", err)
				}
			default:
				_ = want
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.DeviceID != tc.want.DeviceID {
					t.Errorf("resolved to %v, want %v", got.DeviceID, tc.want.DeviceID)
				}
			}
		})
	}
}

func TestRenameAdoptsOnceAndRejectsCollisions(t *testing.T) {
	r := New(nil)
	now := time.Now()
	a, _ := r.Upsert("aaa", "addr", testCaps(), now)
	b, _ := r.Upsert("bbb", "addr", testCaps(), now)

	adopted, err := r.Rename(a.DeviceID, "kitchen")
	if err != nil || !adopted {
		t.Fatalf("first rename should adopt: adopted=%v err=%v", adopted, err)
	}
	if got, _ := r.View(a.DeviceID); got.Status != bulb.Connected {
		t.Errorf("adoption should connect the bulb, got %v", got.Status)
	}
	adopted, err = r.Rename(a.DeviceID, "kitchen-2")
	if err != nil || adopted {
		t.Fatalf("second rename should not adopt: adopted=%v err=%v", adopted, err)
	}
	var inUse NameInUseError
	if _, err := r.Rename(b.DeviceID, "kitchen-2"); !errors.As(err, &inUse) {
		t.Fatalf("err = %v, want NameInUseError", err)
	}
	if _, err := r.Rename(b.DeviceID, "   "); err == nil {
		t.Error("an empty name should be refused")
	}
}

func TestSetStateReturnsOnlyRealChanges(t *testing.T) {
	r := New(nil)
	now := time.Now()
	b, _ := r.Upsert("dev", "addr", testCaps(), now)
	start := bulb.LightState{Power: true, Brightness: 30, ColorTemp: 2}
	r.SetState(b.DeviceID, start, now)

	same := start
	same.ReportedAt = now.Add(time.Second)
	if got := r.SetState(b.DeviceID, same, now); len(got) != 0 {
		t.Errorf("a report with no change produced %v", got)
	}
	next := bulb.LightState{Power: true, Brightness: 100, ColorTemp: 2}
	changes := r.SetState(b.DeviceID, next, now)
	if len(changes) != 1 || changes[0].Field != "brightness" {
		t.Errorf("changes = %v, want one brightness change", changes)
	}
}

func TestSetStateClearsDesiredSoReportedWins(t *testing.T) {
	r := New(nil)
	b, _ := r.Upsert("dev", "addr", testCaps(), time.Now())
	r.SetDesired(b.DeviceID, bulb.LightState{Power: true, Brightness: 80})
	// Someone hits the wall switch: the bulb reports off.
	r.SetState(b.DeviceID, bulb.LightState{Power: false}, time.Now())
	got, _ := r.View(b.DeviceID)
	if got.Desired != nil {
		t.Error("a report must clear the commanded state, not compete with it")
	}
	if got.State.Power {
		t.Error("the bulb's own report must win")
	}
}

func TestDisconnectIgnoresStaleDriver(t *testing.T) {
	r := New(nil)
	b, _ := r.Upsert("dev", "addr", testCaps(), time.Now())
	if _, err := r.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	current := &fakeDriver{id: "current"}
	stale := &fakeDriver{id: "stale"}
	r.SetDriver(b.DeviceID, current)

	// A late teardown from an older connection must not knock the live one out.
	r.Disconnect(b.DeviceID, stale)
	if got, _ := r.View(b.DeviceID); got.Driver == nil || got.Status != bulb.Connected {
		t.Error("a stale disconnect tore down the live connection")
	}
	r.Disconnect(b.DeviceID, current)
	if got, _ := r.View(b.DeviceID); got.Driver != nil || got.Status != bulb.Disconnected {
		t.Error("the owning disconnect should have taken effect")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := New(nil)
	now := time.Now()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(3)
		go func() { defer wg.Done(); r.Upsert("dev", "addr", testCaps(), now) }()
		go func() { defer wg.Done(); _ = r.List() }()
		go func() {
			defer wg.Done()
			r.SetState("dev", bulb.LightState{Brightness: uint8(i%100 + 1)}, now)
		}()
	}
	wg.Wait()
	if got := len(r.List()); got != 1 {
		t.Errorf("concurrent upserts produced %d bulbs, want 1", got)
	}
}

type fakeDriver struct{ id string }

func (f fakeDriver) DeviceID() string                                  { return f.id }
func (f *fakeDriver) Apply(_ context.Context, _ bulb.LightState) error { return nil }
func (f fakeDriver) Close() error                                      { return nil }
