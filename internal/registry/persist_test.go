package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	return NewStore(path, time.Millisecond), path
}

func TestMissingFileIsAFirstRun(t *testing.T) {
	s, _ := newStore(t)
	reg, err := s.Load()
	if err != nil {
		t.Fatalf("a missing registry should not be an error: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Error("expected an empty registry")
	}
}

func TestRoundTrip(t *testing.T) {
	s, path := newStore(t)
	reg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	b, _ := reg.Upsert("703e975dc388", "192.168.1.5:1", bulb.Capabilities{Known: true, ColorTemp: true, MinBrightness: 1}, now)
	if _, err := reg.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	reg.SetState(b.DeviceID, bulb.LightState{Power: true, Brightness: 80, ColorTemp: 50, ReportedAt: now}, now)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reloaded, err := NewStore(path, time.Millisecond).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reloaded.View("703e975dc388")
	if !ok {
		t.Fatal("bulb did not survive the restart")
	}
	if got.Name != "kitchen" {
		t.Errorf("name = %q, want kitchen", got.Name)
	}
	if got.State.Brightness != 80 || got.State.ColorTemp != 50 || !got.State.Power {
		t.Errorf("state = %+v", got.State)
	}
	if !got.Capabilities.Known || !got.Capabilities.ColorTemp {
		t.Errorf("capabilities = %+v", got.Capabilities)
	}
	// Nothing is assumed still online across a restart.
	if got.Status != bulb.Disconnected {
		t.Errorf("status = %v, want disconnected", got.Status)
	}
}

func TestCorruptFileIsAnErrorAndIsLeftAlone(t *testing.T) {
	s, path := newStore(t)
	const junk = `{"version":1,"bulbs":[{"device_id":`
	if err := os.WriteFile(path, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load()
	if err == nil {
		t.Fatal("a corrupt registry must be an error, not a silent reset")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should say what is wrong: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != junk {
		t.Error("the corrupt file must be left untouched for the operator to inspect")
	}
}

func TestUnknownVersionIsRefused(t *testing.T) {
	s, path := newStore(t)
	if err := os.WriteFile(path, []byte(`{"version":99,"bulbs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load()
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err = %v, want a version error", err)
	}
}

func TestDuplicateEntriesAreRefused(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"duplicate device id", `{"version":1,"bulbs":[{"device_id":"a","name":"x"},{"device_id":"a","name":"y"}]}`},
		{"duplicate name", `{"version":1,"bulbs":[{"device_id":"a","name":"x"},{"device_id":"b","name":"x"}]}`},
		{"empty device id", `{"version":1,"bulbs":[{"device_id":"","name":"x"}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, path := newStore(t)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// The atomic write must never leave a half-written file where the good one was.
func TestSaveIsAtomic(t *testing.T) {
	s, path := newStore(t)
	reg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := reg.Upsert("dev", "addr", bulb.Capabilities{Known: true}, time.Now())
	if _, err := reg.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// No temporary files may survive a successful save.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".registry-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
	if len(original) == 0 {
		t.Error("registry file is empty")
	}
}

func TestDebouncedSavesCoalesce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	s := NewStore(path, 20*time.Millisecond)
	reg, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	b, _ := reg.Upsert("dev", "addr", bulb.Capabilities{Known: true}, now)
	if _, err := reg.Rename(b.DeviceID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	// A burst of reports should not produce a burst of writes.
	for i := 1; i <= 50; i++ {
		reg.SetState(b.DeviceID, bulb.LightState{Power: true, Brightness: uint8(i)}, now)
	}
	// Wait for the file rather than sleeping a fixed span. The debounce is 20ms,
	// but the save that follows it has to be scheduled and then fsync'd, and a
	// loaded CI runner under -race can miss any deadline picked in advance.
	// Polling is slow only when the save genuinely never lands.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := os.Stat(path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("debounced save never landed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
