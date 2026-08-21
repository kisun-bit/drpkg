package backup

// TaskState is the lifecycle state of a task managed by an Engine.
type TaskState int

// Task states.
const (
	// StatePending: submitted, waiting to be scheduled.
	StatePending TaskState = iota
	// StateWaiting: dependencies are not finished yet.
	StateWaiting
	// StateRunning: the work function is executing.
	StateRunning
	// StatePaused: the work function is blocked until resumed.
	StatePaused
	// StateCompleted: finished successfully.
	StateCompleted
	// StateFailed: finished with an error (including dependency
	// failures).
	StateFailed
	// StateCancelled: cancelled by the user or by engine shutdown.
	StateCancelled
	// StateTimedOut: the per-task deadline was exceeded.
	StateTimedOut
)

// String returns the state name.
func (s TaskState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateWaiting:
		return "waiting"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateCancelled:
		return "cancelled"
	case StateTimedOut:
		return "timedout"
	default:
		return "unknown"
	}
}

// Terminal reports whether the state is final (the task will not change
// state anymore).
func (s TaskState) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateTimedOut:
		return true
	default:
		return false
	}
}

// Success reports whether the task finished successfully.
func (s TaskState) Success() bool { return s == StateCompleted }

// EventKind identifies the type of an Event emitted by an Engine.
type EventKind int

// Event kinds.
const (
	// EventSubmitted: a task was accepted by Submit.
	EventSubmitted EventKind = iota
	// EventStateChange: the task changed state; Event.State carries the
	// new state.
	EventStateChange
	// EventProgress: the task reported progress; Event.Progress carries
	// the latest values.
	EventProgress
	// EventRetry: a failed attempt will be retried; Event.Attempt and
	// Event.Err carry the attempt number and the failure cause.
	EventRetry
	// EventFinal: the task reached a terminal state. It is always the
	// last event for a task; Event.State, Event.Err and Event.Progress
	// carry the final values.
	EventFinal
)

// String returns the event kind name.
func (k EventKind) String() string {
	switch k {
	case EventSubmitted:
		return "submitted"
	case EventStateChange:
		return "state_change"
	case EventProgress:
		return "progress"
	case EventRetry:
		return "retry"
	case EventFinal:
		return "final"
	default:
		return "unknown"
	}
}
