package backup

import (
	"sync"
	"time"
)

// Event is a notification emitted by an Engine about a task.
type Event struct {
	// Kind is the event type.
	Kind EventKind
	// TaskID identifies the task the event belongs to.
	TaskID string
	// State is the new (EventStateChange/EventFinal) or current state.
	State TaskState
	// Progress carries the latest progress for EventProgress and
	// EventFinal events.
	Progress Progress
	// Attempt is the 1-based attempt number for EventRetry events.
	Attempt int
	// Err is the cause for EventRetry and failed EventFinal events.
	Err error
	// Time is when the event was emitted.
	Time time.Time
}

// subscription is one registered event consumer.
type subscription struct {
	ch     chan Event
	closed bool
	drops  uint64
}

// eventHub is a fan-out event broadcaster with non-blocking publish:
// events for subscribers whose buffer is full are dropped and counted.
// All methods are safe for concurrent use.
type eventHub struct {
	mu     sync.Mutex
	subs   map[uint64]*subscription
	nextID uint64
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[uint64]*subscription)}
}

// subscribe registers a consumer with the given channel buffer size and
// returns the event channel plus an unsubscribe function. The channel is
// closed by unsubscribe.
func (h *eventHub) subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	sub := &subscription{ch: make(chan Event, buffer)}
	h.subs[id] = sub
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		s, ok := h.subs[id]
		if ok {
			s.closed = true
			close(s.ch)
			delete(h.subs, id)
		}
		h.mu.Unlock()
	}
	return sub.ch, unsubscribe
}

// publish delivers ev to every subscriber without blocking. Events for
// full channels are dropped.
func (h *eventHub) publish(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			sub.drops++
		}
	}
}
