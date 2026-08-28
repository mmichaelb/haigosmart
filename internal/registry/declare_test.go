package registry

import (
	"testing"
	"time"

	"haigosmart/internal/bulb"
)

func TestDeclareCreatesADisconnectedEntry(t *testing.T) {
	r := New(nil)
	created, renamed, err := r.Declare("a1b2c3d4", "headlamp")
	if err != nil {
		t.Fatal(err)
	}
	if !created || renamed {
		t.Errorf("created=%v renamed=%v, want created", created, renamed)
	}

	b, ok := r.View("a1b2c3d4")
	if !ok {
		t.Fatal("the declared lamp is not in the registry")
	}
	if b.Name != "headlamp" {
		t.Errorf("name = %q, want %q", b.Name, "headlamp")
	}
	if b.Status != bulb.Disconnected {
		t.Errorf("status = %v, want Disconnected: a configured lamp has not proved it is online", b.Status)
	}
	if !b.Adopted() {
		t.Error("a configured lamp must count as adopted; it was named deliberately")
	}
}

func TestDeclareIsIdempotent(t *testing.T) {
	r := New(nil)
	if _, _, err := r.Declare("a1", "headlamp"); err != nil {
		t.Fatal(err)
	}
	created, renamed, err := r.Declare("a1", "headlamp")
	if err != nil {
		t.Fatal(err)
	}
	if created || renamed {
		t.Errorf("created=%v renamed=%v, want neither on an unchanged declaration", created, renamed)
	}
}

// TestDeclareRenamesToTheConfiguredName is FR-022: where the stored name and the
// configured name disagree, the configuration wins.
func TestDeclareRenamesToTheConfiguredName(t *testing.T) {
	r := New(nil)
	now := time.Now()
	r.Upsert("a1", "10.0.0.5:1234", bulb.Capabilities{}, now)
	if _, err := r.Rename("a1", "oldname"); err != nil {
		t.Fatal(err)
	}

	created, renamed, err := r.Declare("a1", "newname")
	if err != nil {
		t.Fatal(err)
	}
	if created || !renamed {
		t.Errorf("created=%v renamed=%v, want renamed", created, renamed)
	}
	if b, _ := r.View("a1"); b.Name != "newname" {
		t.Errorf("name = %q, want the configured %q", b.Name, "newname")
	}
}

// TestDeclarePreservesWhatItAlreadyKnew: declaring is not a reset. State and
// capabilities learned from the lamp survive, because the configuration says
// which lamps to serve, not what they are.
func TestDeclarePreservesWhatItAlreadyKnew(t *testing.T) {
	r := New(nil)
	now := time.Now()
	caps := bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}
	r.Upsert("a1", "10.0.0.5:1234", caps, now)
	r.SetState("a1", bulb.LightState{Power: true, Brightness: 40, ColorTemp: 60}, now)
	r.SetFirmware("a1", "1.0.7_cct")

	if _, _, err := r.Declare("a1", "headlamp"); err != nil {
		t.Fatal(err)
	}

	b, _ := r.View("a1")
	if b.State.Brightness != 40 || !b.State.Power {
		t.Errorf("state = %+v, want the reported state preserved", b.State)
	}
	if !b.Capabilities.Known || !b.Capabilities.ColorTemp {
		t.Errorf("capabilities = %+v, want them preserved", b.Capabilities)
	}
	if b.FirmwareVersion != "1.0.7_cct" {
		t.Errorf("firmware = %q, want it preserved", b.FirmwareVersion)
	}
}

// TestDeclareRejectsANameAlreadyInUse: two lamps under one name would be
// ambiguous in the terminal and in Home Assistant alike.
func TestDeclareRejectsANameAlreadyInUse(t *testing.T) {
	r := New(nil)
	if _, _, err := r.Declare("a1", "headlamp"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Declare("b2", "headlamp"); err == nil {
		t.Fatal("two lamps were allowed to share a name")
	}
}

// TestDeclareDoesNotDisconnectAConnectedLamp: a lamp already online when its
// declaration is applied stays online.
func TestDeclareDoesNotDisconnectAConnectedLamp(t *testing.T) {
	r := New(nil)
	r.Upsert("a1", "10.0.0.5:1234", bulb.Capabilities{}, time.Now())
	if _, err := r.Rename("a1", "headlamp"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Declare("a1", "headlamp"); err != nil {
		t.Fatal(err)
	}
	if b, _ := r.View("a1"); b.Status != bulb.Connected {
		t.Errorf("status = %v, want Connected", b.Status)
	}
}
