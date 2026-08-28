package events

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"haigosmart/internal/bulb"
)

func quietBus() *Bus { return NewBus(slog.New(slog.NewTextHandler(io.Discard, nil))) }

func TestFanOutToEverySubscriber(t *testing.T) {
	bus := quietBus()
	subs := make([]*Subscription, 3)
	for i := range subs {
		subs[i] = bus.Subscribe(16)
		defer subs[i].Close()
	}
	bus.Publish(Event{At: time.Now(), Kind: Connected, Name: "kitchen"})
	for i, s := range subs {
		select {
		case e := <-s.Events():
			if e.Name != "kitchen" {
				t.Errorf("subscriber %d got %+v", i, e)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d received nothing", i)
		}
	}
}

func TestCloseRemovesSubscriber(t *testing.T) {
	bus := quietBus()
	kept := bus.Subscribe(4)
	defer kept.Close()
	dropped := bus.Subscribe(4)
	dropped.Close()

	bus.Publish(Event{Kind: Connected})
	if len(dropped.Events()) != 0 {
		t.Error("a closed subscription should stop receiving")
	}
	if len(kept.Events()) != 1 {
		t.Error("the remaining subscriber should still receive")
	}
}

// The whole point of the bus: a stalled consumer must never stall the producer,
// because the producer is a bulb's read loop.
func TestPublishNeverBlocksOnAStalledSubscriber(t *testing.T) {
	bus := quietBus()
	stalled := bus.Subscribe(2)
	defer stalled.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 10_000 {
			bus.Publish(Event{Kind: StateChanged, DeviceID: "dev", Detail: string(rune(i))})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a subscriber that never drains")
	}
	if stalled.Dropped() == 0 {
		t.Error("expected drops to be counted rather than hidden")
	}
}

func TestDropOldestKeepsTheNewest(t *testing.T) {
	bus := quietBus()
	sub := bus.Subscribe(2)
	defer sub.Close()
	for i := range 10 {
		bus.Publish(Event{Kind: StateChanged, Detail: string(rune('a' + i))})
	}
	// Whatever survived must be from the end of the run, not the start.
	var got []string
	for len(sub.Events()) > 0 {
		got = append(got, (<-sub.Events()).Detail)
	}
	if len(got) == 0 {
		t.Fatal("nothing survived")
	}
	if got[len(got)-1] != "j" {
		t.Errorf("newest surviving event = %q, want the last published", got[len(got)-1])
	}
}

// SC-008: every event reaches the log, even when the display drops some.
func TestEveryEventReachesTheLogEvenWhenTheDisplayDrops(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	bus := NewBus(slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil)))
	sub := bus.Subscribe(1) // one slot: guaranteed to drop
	defer sub.Close()

	const total = 500
	for i := range total {
		bus.Publish(Event{
			At: time.Now(), Kind: StateChanged, DeviceID: "703e975dc388", Name: "kitchen",
			Changed: []bulb.FieldChange{{Field: "brightness", From: "0", To: string(rune('0' + i%10))}},
		})
	}
	if sub.Dropped() == 0 {
		t.Fatal("this test is meaningless unless the display actually dropped events")
	}

	mu.Lock()
	defer mu.Unlock()
	var logged int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid json: %v", err)
		}
		if record["device"] != "703e975dc388" {
			t.Errorf("event attributed to %v, want the right bulb", record["device"])
		}
		logged++
	}
	if logged != total {
		t.Errorf("%d of %d events reached the log; the record must be complete", logged, total)
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	bus := quietBus()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 200 {
				bus.Publish(Event{Kind: StateChanged})
			}
		}()
		go func() {
			defer wg.Done()
			s := bus.Subscribe(4)
			for range 50 {
				select {
				case <-s.Events():
				default:
				}
			}
			s.Close()
		}()
	}
	wg.Wait()
}

func TestEventText(t *testing.T) {
	tests := []struct {
		name string
		e    Event
		want string
	}{
		{"connected", Event{Kind: Connected}, "connected"},
		{"disconnected with reason", Event{Kind: Disconnected, Detail: "power cut"}, "disconnected (power cut)"},
		{"disconnected bare", Event{Kind: Disconnected}, "disconnected"},
		{"discovered", Event{Kind: Discovered}, "discovered — name it to control it"},
		{"command failure", Event{Kind: CommandResult, Detail: "timeout"}, "command failed: timeout"},
		{"protocol error", Event{Kind: ProtocolError, Detail: "bad frame"}, "protocol error: bad frame"},
		{"duplicate id", Event{Kind: DuplicateID, Detail: "10.0.0.9:1"}, "WARNING duplicate device id, also seen from 10.0.0.9:1"},
		{
			"multi-field change",
			Event{Kind: StateChanged, Changed: []bulb.FieldChange{
				{Field: "power", From: "off", To: "on"},
				{Field: "brightness", From: "40", To: "80"},
			}},
			"power off→on  brightness 40→80",
		},
		{"empty change", Event{Kind: StateChanged}, "state reported (no change)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.Text(); got != tc.want {
				t.Errorf("Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventLabelFallsBackToDeviceID(t *testing.T) {
	tests := []struct {
		e    Event
		want string
	}{
		{Event{Name: "kitchen", DeviceID: "aaa"}, "kitchen"},
		{Event{DeviceID: "aaa"}, "aaa"},
		{Event{}, "server"},
	}
	for _, tc := range tests {
		if got := tc.e.Label(); got != tc.want {
			t.Errorf("Label() = %q, want %q", got, tc.want)
		}
	}
}

func TestEventLine(t *testing.T) {
	at := time.Date(2026, 8, 27, 14, 2, 11, 0, time.UTC)
	e := Event{At: at, Kind: Connected, Name: "kitchen"}
	if got := e.Line(); !strings.HasPrefix(got, "14:02:11  kitchen") {
		t.Errorf("Line() = %q", got)
	}
}
