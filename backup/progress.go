package backup

import (
	"context"
	"sync"
	"time"
)

// Progress is a snapshot of a task's progress.
type Progress struct {
	// Percent is the completion percentage in [0, 100].
	Percent int32
	// Message is the latest progress message.
	Message string
	// UpdatedAt is the time of the last progress update.
	UpdatedAt time.Time
}

// Reporter lets a task's WorkFunc report progress back to the engine.
// It is safe for concurrent use.
type Reporter struct {
	item *taskItem
}

// Report records the current completion percentage and a message, and
// publishes an EventProgress event. percent outside [0, 100] returns
// ErrInvalidProgress and the update is ignored.
func (r *Reporter) Report(percent int32, msg string) error {
	if percent < 0 || percent > 100 {
		return ErrInvalidProgress
	}
	now := time.Now()

	item := r.item
	item.mu.Lock()
	item.progress.Percent = percent
	item.progress.Message = msg
	item.progress.UpdatedAt = now
	item.mu.Unlock()

	item.engine.publish(Event{
		Kind:     EventProgress,
		TaskID:   item.id,
		Progress: Progress{Percent: percent, Message: msg, UpdatedAt: now},
	})
	return nil
}

// Checkpoint blocks while the owning task is paused and returns when it
// is resumed. It returns ctx.Err() if ctx is cancelled first (e.g. the
// task was cancelled while paused).
//
// Pause is cooperative: work functions that want to support pause should
// call Checkpoint periodically, the same way they honor ctx cancellation.
func (r *Reporter) Checkpoint(ctx context.Context) error {
	item := r.item
	return item.engine.pauseWait(item, ctx)
}

// pauseGate implements pause/resume as a channel gate: running while the
// channel is closed, blocked while it is open.
type pauseGate struct {
	mu     sync.Mutex
	ch     chan struct{} // closed => running
	paused bool
}

func newPauseGate() *pauseGate {
	ch := make(chan struct{})
	close(ch)
	return &pauseGate{ch: ch}
}

// waitCtx blocks until the gate is opened (task running) or ctx is done.
func (g *pauseGate) waitCtx(ctx context.Context) error {
	g.mu.Lock()
	ch := g.ch
	g.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pause closes the gate; returns false if already paused.
func (g *pauseGate) pause() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return false
	}
	g.ch = make(chan struct{})
	g.paused = true
	return true
}

// resume opens the gate; returns false if not paused.
func (g *pauseGate) resume() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.paused {
		return false
	}
	close(g.ch) // waiters unblock; the closed channel stays valid
	g.paused = false
	return true
}

// isPaused reports whether the gate is currently closed.
func (g *pauseGate) isPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}
