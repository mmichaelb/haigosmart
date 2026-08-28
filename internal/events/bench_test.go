package events

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"haigosmart/internal/bulb"
)

func benchEvent() Event {
	return Event{
		At: time.Now(), Kind: StateChanged, DeviceID: "703e975dc388", Name: "kitchen",
		Changed: []bulb.FieldChange{{Field: "brightness", From: "40", To: "80"}},
	}
}

// BenchmarkPublish measures the cost on a bulb's read loop of reporting a state
// change. This is the hot path: it runs once per bulb per change.
func BenchmarkPublish(b *testing.B) {
	bus := NewBus(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	sub := bus.Subscribe(1024)
	defer sub.Close()
	go func() {
		for range sub.Events() {
		}
	}()
	e := benchEvent()
	b.ReportAllocs()
	for b.Loop() {
		bus.Publish(e)
	}
}

// BenchmarkFanOut30 is the shape the spec actually asks for: 30 bulbs, so up to
// 30 subscribers in a pathological configuration.
func BenchmarkFanOut30(b *testing.B) {
	bus := NewBus(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for range 30 {
		s := bus.Subscribe(64)
		defer s.Close()
		go func() {
			for range s.Events() {
			}
		}()
	}
	e := benchEvent()
	b.ReportAllocs()
	for b.Loop() {
		bus.Publish(e)
	}
}

// BenchmarkPublishStalledSubscriber measures the drop path, which is what runs
// when the terminal cannot keep up. It must stay cheap: it happens on the
// network goroutine.
func BenchmarkPublishStalledSubscriber(b *testing.B) {
	bus := NewBus(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	sub := bus.Subscribe(2) // never drained
	defer sub.Close()
	e := benchEvent()
	b.ReportAllocs()
	for b.Loop() {
		bus.Publish(e)
	}
}

func BenchmarkEventLine(b *testing.B) {
	e := benchEvent()
	b.ReportAllocs()
	for b.Loop() {
		_ = e.Line()
	}
}
