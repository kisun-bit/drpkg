package backup

import (
	"context"
	"fmt"
	"time"
)

// WorkFunc is the body of a task. It receives a context that is cancelled
// when the task is cancelled or times out, and a Reporter for progress
// updates. Returning nil marks the task completed; returning an error
// marks it Failed (subject to retry configuration).
//
// WorkFunc must honor ctx.Done: the engine can only stop a task once the
// function returns.
type WorkFunc func(ctx context.Context, rep *Reporter) error

// Task describes a unit of backup work to be scheduled by an Engine.
type Task struct {
	// ID uniquely identifies the task within an engine. Required.
	ID string

	// Work is the function to execute. Required.
	Work WorkFunc

	// DependsOn lists IDs of tasks that must complete successfully
	// before this task starts. Dependencies may be submitted in any
	// order; a dependency on an unknown ID is an error at Submit time.
	DependsOn []string

	// Timeout bounds a single attempt of the task. Zero means no
	// timeout. The work function observes the timeout through its ctx.
	Timeout time.Duration

	// Retry is the number of additional attempts after a failed one
	// (Retry=3 means up to 4 attempts in total). Zero means no retry.
	Retry int

	// RetryBackoff is the delay before each retry attempt. Zero means
	// retries start immediately.
	RetryBackoff time.Duration

	// Watchers are periodic checks that run while the task is active.
	// They start when the task begins executing and stop when it reaches
	// a terminal state. A check can terminate the task by returning
	// ActionCancel or ActionFail. Optional.
	Watchers []Watcher
}

// validate checks the task fields.
func (t *Task) validate() error {
	if t.ID == "" {
		return fmt.Errorf("%v: empty ID", ErrInvalidTask)
	}
	if t.Work == nil {
		return fmt.Errorf("%v: %s: nil Work", ErrInvalidTask, t.ID)
	}
	if t.Timeout < 0 {
		return fmt.Errorf("%v: %s: negative Timeout", ErrInvalidTask, t.ID)
	}
	if t.Retry < 0 {
		return fmt.Errorf("%v: %s: negative Retry", ErrInvalidTask, t.ID)
	}
	if t.RetryBackoff < 0 {
		return fmt.Errorf("%v: %s: negative RetryBackoff", ErrInvalidTask, t.ID)
	}
	for _, d := range t.DependsOn {
		if d == "" {
			return fmt.Errorf("%v: %s: empty dependency ID", ErrInvalidTask, t.ID)
		}
		if d == t.ID {
			return fmt.Errorf("%v: %s depends on itself", ErrCycleDetected, t.ID)
		}
	}
	for i := range t.Watchers {
		if err := t.Watchers[i].validate(); err != nil {
			return fmt.Errorf("%v: %s: watcher %d", err, t.ID, i)
		}
	}
	return nil
}
