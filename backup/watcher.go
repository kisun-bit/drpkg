package backup

import (
	"context"
	"time"
)

// WatcherAction is the verdict a Watcher check returns, telling the engine
// what to do with the owning task.
type WatcherAction int

// Watcher verdicts.
const (
	// ActionContinue lets the task keep running unchanged. This is the
	// zero value, so a check that returns (ActionContinue, nil) is a
	// no-op.
	ActionContinue WatcherAction = iota

	// ActionCancel requests cancellation of the task. The task ends in
	// StateCancelled, exactly as if Handle.Cancel had been called.
	ActionCancel

	// ActionFail fails the task. The error returned alongside the action
	// becomes the task's final error; a nil error is replaced with a
	// generic message. The task ends in StateFailed.
	ActionFail
)

// String returns the action name.
func (a WatcherAction) String() string {
	switch a {
	case ActionContinue:
		return "continue"
	case ActionCancel:
		return "cancel"
	case ActionFail:
		return "fail"
	default:
		return "unknown"
	}
}

// WatcherCheckFunc is invoked periodically for a Watcher. It receives the
// task's context and a Handle to the owning task, so it can inspect state
// and progress before deciding.
//
// Returning ActionContinue lets the task run on. Returning ActionCancel or
// ActionFail terminates the task; for ActionFail the returned error is
// recorded as the task's final error. The function must not block
// indefinitely: it runs on a dedicated goroutine but a slow check delays
// subsequent ticks for this watcher.
type WatcherCheckFunc func(ctx context.Context, h *Handle) (WatcherAction, error)

// Watcher is a periodic check that runs alongside a task while the task is
// active (from the moment it starts executing until it reaches a terminal
// state). Watchers are started and stopped automatically by the engine; the
// caller never manages their lifecycle.
//
// Watchers re-arm on manual Retry: each run of the task gets a fresh set of
// watchers.
type Watcher struct {
	// Interval is how often Check is invoked. Must be positive.
	Interval time.Duration

	// Check is the function invoked on each tick. Required.
	Check WatcherCheckFunc

	// Immediate, when true, invokes Check once as soon as the watcher
	// starts, before the first Interval elapses. Defaults to false.
	Immediate bool
}

// validate checks the watcher fields.
func (w *Watcher) validate() error {
	if w.Interval <= 0 {
		return ErrInvalidWatcher
	}
	if w.Check == nil {
		return ErrInvalidWatcher
	}
	return nil
}
