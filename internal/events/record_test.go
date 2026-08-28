package events

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mmichaelb/haigosmart/internal/bulb"
)

// distinctDetail is deliberately unmistakable: the assertions below check it
// lands in the detail field and nowhere else.
const distinctDetail = "a-very-distinctive-detail"

// TestKindRecords pins the log message and level of every event kind to
// contracts/log-records.md, and fails when a kind is added without one.
func TestKindRecords(t *testing.T) {
	want := map[Kind]struct {
		name  string
		msg   string
		level slog.Level
	}{
		StateChanged:  {"state_changed", "bulb reported state", slog.LevelInfo},
		Connected:     {"connected", "bulb connected", slog.LevelInfo},
		Disconnected:  {"disconnected", "bulb disconnected", slog.LevelInfo},
		Discovered:    {"discovered", "bulb discovered", slog.LevelInfo},
		CommandResult: {"command_result", "command failed", slog.LevelWarn},
		ProtocolError: {"protocol_error", "protocol error", slog.LevelWarn},
		DuplicateID:   {"duplicate_id", "duplicate device id", slog.LevelWarn},
		Renamed:       {"renamed", "bulb renamed", slog.LevelInfo},
		Rejected:      {"rejected", "bulb rejected", slog.LevelWarn},
	}

	for k := Kind(0); k < numKinds; k++ {
		w, ok := want[k]
		if !ok {
			t.Fatalf("kind %d has no expected record: a new event kind needs its own message and level in Kind.record(), not the default", k)
		}
		if got := k.String(); got != w.name {
			t.Errorf("kind %d String() = %q, want %q", k, got, w.name)
		}
		msg, level := k.record()
		if msg != w.msg {
			t.Errorf("kind %s message = %q, want %q", w.name, msg, w.msg)
		}
		if level != w.level {
			t.Errorf("kind %s level = %v, want %v", w.name, level, w.level)
		}
	}
}

// TestRecordMessagesCarryNoValues is the reason the messages are fixed. A
// message with the detail interpolated into it cannot be grouped by message,
// and a collector cannot filter on what it cannot see as a field.
func TestRecordMessagesCarryNoValues(t *testing.T) {
	for k := Kind(0); k < numKinds; k++ {
		var buf bytes.Buffer
		bus := NewBus(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		bus.Publish(Event{
			At: time.Now(), Kind: k, DeviceID: "a1b2c3d4", Name: "headlamp", Detail: distinctDetail,
			Changed: []bulb.FieldChange{{Field: "brightness", From: "40", To: "100"}},
		})

		var rec map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
			t.Fatalf("kind %s: record is not JSON: %v", k, err)
		}
		msg, _ := rec["msg"].(string)
		if strings.Contains(msg, distinctDetail) {
			t.Errorf("kind %s: msg %q carries the detail; variable text belongs in a field", k, msg)
		}
		if strings.Contains(msg, "a1b2c3d4") || strings.Contains(msg, "headlamp") {
			t.Errorf("kind %s: msg %q carries the device identity; that belongs in device/name", k, msg)
		}
		if rec["detail"] != distinctDetail {
			t.Errorf("kind %s: detail field = %v, want %q", k, rec["detail"], distinctDetail)
		}
		if rec["kind"] != k.String() {
			t.Errorf("kind %s: kind field = %v, want %q", k, rec["kind"], k.String())
		}
		if rec["brightness"] != "40→100" {
			t.Errorf("kind %s: changed field = %v, want %q", k, rec["brightness"], "40→100")
		}
	}
}
