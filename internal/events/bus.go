package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Bus fans events out to subscribers. Publish never blocks: a subscriber that
// cannot keep up loses its oldest queued events, and the drop is counted rather
// than hidden. A stalled terminal must never stall a bulb's read loop.
type Bus struct {
	mu     sync.RWMutex
	subs   []*Subscription
	logger *slog.Logger
}

// Subscription is one consumer's view of the bus.
type Subscription struct {
	ch      chan Event
	dropped atomic.Uint64
	bus     *Bus
}

// NewBus returns a bus that logs every published event through logger.
func NewBus(logger *slog.Logger) *Bus {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bus{logger: logger}
}

// Subscribe returns a new subscription with the given queue depth.
func (b *Bus) Subscribe(depth int) *Subscription {
	if depth < 1 {
		depth = 1
	}
	s := &Subscription{ch: make(chan Event, depth), bus: b}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	return s
}

// Events is the channel to receive on.
func (s *Subscription) Events() <-chan Event { return s.ch }

// Dropped reports how many events never reached this subscriber's display.
// They are still in the log; nothing is lost from the record.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Close removes the subscription from its bus.
func (s *Subscription) Close() {
	b := s.bus
	b.mu.Lock()
	for i, other := range b.subs {
		if other == s {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
}

// Publish logs the event unconditionally, then offers it to every subscriber.
// The log is the complete record required by SC-008; subscriber queues are a
// display buffer and may drop under load (SC-009).
func (b *Bus) Publish(e Event) {
	b.log(e)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		s.offer(e)
	}
}

// offer enqueues e, evicting the oldest event if the queue is full. It never
// blocks: at worst it drops one event to make room and drops e itself if the
// consumer refilled the queue in between.
func (s *Subscription) offer(e Event) {
	select {
	case s.ch <- e:
		return
	default:
	}
	select {
	case <-s.ch:
		s.dropped.Add(1)
	default:
	}
	select {
	case s.ch <- e:
	default:
		s.dropped.Add(1)
	}
}

func (b *Bus) log(e Event) {
	attrs := []any{"kind", e.Kind.String(), "device", e.DeviceID, "name", e.Name}
	if e.Detail != "" {
		attrs = append(attrs, "detail", e.Detail)
	}
	for _, c := range e.Changed {
		attrs = append(attrs, c.Field, c.From+"→"+c.To)
	}
	level := slog.LevelInfo
	if e.Kind == ProtocolError || e.Kind == DuplicateID {
		level = slog.LevelWarn
	}
	b.logger.Log(context.Background(), level, e.Text(), attrs...)
}

func (k Kind) String() string {
	switch k {
	case Connected:
		return "connected"
	case Disconnected:
		return "disconnected"
	case Discovered:
		return "discovered"
	case CommandResult:
		return "command_result"
	case ProtocolError:
		return "protocol_error"
	case DuplicateID:
		return "duplicate_id"
	default:
		return "state_changed"
	}
}
