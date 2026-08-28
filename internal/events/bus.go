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
	msg, level := e.Kind.record()
	b.logger.Log(context.Background(), level, msg, attrs...)
}

// record is the log line's message and level for a kind, per
// specs/003-headless-deployment/contracts/log-records.md.
//
// The message is fixed per kind and carries no value from the event: everything
// variable is already an attribute above. A record reading {"msg":"bulb
// disconnected","detail":"no keep-alive for 180s"} can be grouped by message and
// filtered by field; {"msg":"disconnected (no keep-alive for 180s)"} can be
// neither. Event.Text() is unaffected — the terminal renders from the event.
func (k Kind) record() (msg string, level slog.Level) {
	switch k {
	case Connected:
		return "bulb connected", slog.LevelInfo
	case Disconnected:
		return "bulb disconnected", slog.LevelInfo
	case Discovered:
		return "bulb discovered", slog.LevelInfo
	case CommandResult:
		return "command failed", slog.LevelWarn
	case ProtocolError:
		return "protocol error", slog.LevelWarn
	case DuplicateID:
		return "duplicate device id", slog.LevelWarn
	case Renamed:
		return "bulb renamed", slog.LevelInfo
	case Rejected:
		return "bulb rejected", slog.LevelWarn
	default:
		return "bulb reported state", slog.LevelInfo
	}
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
	case Renamed:
		return "renamed"
	case Rejected:
		return "rejected"
	default:
		return "state_changed"
	}
}
