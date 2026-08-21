package backup

import (
	"context"
	"time"
)

// Handle is the user-facing control and observation interface for one
// submitted task. All methods are safe for concurrent use.
type Handle struct {
	item *taskItem
}

// ID returns the task ID.
func (h *Handle) ID() string { return h.item.id }

// State returns the task's current state.
func (h *Handle) State() TaskState {
	h.item.mu.Lock()
	defer h.item.mu.Unlock()
	return h.item.state
}

// Progress returns a snapshot of the latest reported progress.
func (h *Handle) Progress() Progress {
	h.item.mu.Lock()
	defer h.item.mu.Unlock()
	return h.item.progress
}

// Err returns the terminal error once the task has finished, or nil
// while it is still active (and for successful tasks).
func (h *Handle) Err() error {
	h.item.mu.Lock()
	defer h.item.mu.Unlock()
	return h.item.err
}

// Done returns a channel that is closed when the task reaches a terminal
// state. After a manual Retry, Done reflects the new attempt: call Done
// again to obtain the fresh channel.
func (h *Handle) Done() <-chan struct{} {
	h.item.mu.Lock()
	defer h.item.mu.Unlock()
	return h.item.doneCh
}

// Wait blocks until the task reaches a terminal state and returns its
// error (nil on success), or ctx.Err() if ctx is done first.
func (h *Handle) Wait(ctx context.Context) error {
	select {
	case <-h.Done():
		return h.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pause requests pausing the task. Pause is cooperative: the task blocks
// at the next Reporter.Checkpoint call (or before its next attempt if it
// has not started yet). It returns ErrInvalidState when the task has
// already finished or is already paused.
func (h *Handle) Pause() error {
	it := h.item
	it.mu.Lock()
	finished := it.finished
	it.mu.Unlock()
	if finished {
		return ErrInvalidState
	}
	if !it.gate.pause() {
		return ErrInvalidState
	}
	return nil
}

// Resume resumes a paused task. It returns ErrInvalidState when the task
// is not paused.
func (h *Handle) Resume() error {
	if !h.item.gate.resume() {
		return ErrInvalidState
	}
	return nil
}

// Cancel requests cancellation of the task. The task's context is
// cancelled; tasks that are not currently running are finalized
// immediately, running tasks finalize once their WorkFunc returns. It
// returns ErrInvalidState when the task has already finished.
func (h *Handle) Cancel() error {
	e := h.item.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancelLocked(h.item)
}

// Retry re-runs a task that reached a terminal state other than
// StateCompleted: its state returns to StatePending, the attempt counter
// and progress reset, and it is scheduled again unconditionally (without
// re-evaluating dependencies). It returns ErrInvalidState when the task
// is still active or completed successfully, and ErrEngineClosed when the
// engine has been shut down.
func (h *Handle) Retry() error {
	e := h.item.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrEngineClosed
	}

	it := h.item
	it.mu.Lock()
	if !it.finished {
		it.mu.Unlock()
		return ErrInvalidState
	}
	if it.state == StateCompleted {
		it.mu.Unlock()
		return ErrInvalidState
	}
	it.finished = false
	it.state = StatePending
	it.err = nil
	it.attempt = 0
	it.progress = Progress{UpdatedAt: time.Now()}
	it.doneCh = make(chan struct{})
	it.mu.Unlock()

	// The previous worker (if any) has already left runWork before the
	// task was finalized, so re-arming the scheduling flags is safe.
	// Bump the generation anyway: any worker or watcher goroutine still
	// in flight from the previous run becomes stale immediately and can
	// no longer finalize or mutate the new run.
	it.inFlight = false
	it.generation++
	it.gate.resume() // clear a lingering pause from the previous run
	if it.taskCancel != nil {
		it.taskCancel()
	}
	it.taskCtx, it.taskCancel = context.WithCancel(e.engineCtx)

	e.hub.publish(Event{Kind: EventStateChange, TaskID: it.id, State: StatePending})
	e.wakeLocked()
	return nil
}
