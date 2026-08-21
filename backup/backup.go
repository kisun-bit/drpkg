// Package backup provides a backup task engine: a general-purpose task
// scheduler with pause/resume/cancel/retry control, per-task timeout,
// inter-task dependencies, progress reporting and event notification.
//
// # Run modes
//
// An Engine is created with one of three run modes:
//
//   - MultiInstance (default): any number of engines may run, in this
//     process or across processes on the same host.
//   - ProcessMutex: at most one engine may run in the current process.
//   - HostSingleton: at most one engine may run on the whole host,
//     enforced by an OS file lock on the path given by WithLockPath.
//
// # Typical usage
//
//	engine, err := backup.New(backup.WithConcurrency(4))
//	if err != nil { ... }
//	engine.Start()
//	defer engine.Shutdown(context.Background())
//
//	ch, unsubscribe := engine.Subscribe(64)
//	go drainEvents(ch)
//
//	h, err := engine.Submit(backup.Task{
//		ID: "backup-vm-1",
//		Work: func(ctx context.Context, rep *backup.Reporter) error {
//			rep.Report(50, "halfway")
//			...
//			return nil
//		},
//	})
//	if err != nil { ... }
//
//	h.Pause()  // later: h.Resume()
//	if err := h.Wait(context.Background()); err != nil { ... }
//
// # Periodic checks (watchers)
//
// A task may carry Watchers: periodic checks that run while the task is
// active and can terminate it. Each check returns ActionContinue,
// ActionCancel or ActionFail. A common use is polling an external
// condition (e.g. whether the backup source is still mounted) and
// failing the task when it breaks:
//
//	backup.Task{
//		ID:   "backup-vm-1",
//		Work: work,
//		Watchers: []backup.Watcher{{
//			Interval: 5 * time.Second,
//			Check: func(ctx context.Context, h *backup.Handle) (backup.WatcherAction, error) {
//				if !sourceStillMounted() {
//					return backup.ActionFail, errors.New("source unmounted")
//				}
//				return backup.ActionContinue, nil
//			},
//		}},
//	}
//
// Task work functions must honor ctx cancellation: the engine cancels the
// context when a task is cancelled or its deadline is exceeded, but the
// actual stop only happens when the work function returns.
package backup

import "errors"

// Engine-level errors.
var (
	// ErrEngineAlreadyRunning is returned by New when the selected run
	// mode forbids a second engine and one is already running.
	ErrEngineAlreadyRunning = errors.New("backup: engine already running")

	// ErrEngineClosed is returned by Submit on an engine whose Shutdown
	// has been called.
	ErrEngineClosed = errors.New("backup: engine is closed")

	// ErrEngineNotStarted is returned by Submit on an engine that has
	// not been started yet.
	ErrEngineNotStarted = errors.New("backup: engine is not started")

	// ErrLockPathRequired is returned by New when HostSingleton mode is
	// selected without a WithLockPath option.
	ErrLockPathRequired = errors.New("backup: HostSingleton mode requires a lock path")
)

// Task-level errors.
var (
	// ErrTaskExists is returned by Submit when the task ID is already
	// known to the engine (in any state).
	ErrTaskExists = errors.New("backup: task already exists")

	// ErrTaskNotFound is returned when an operation references an
	// unknown task ID (e.g. a dependency on a task that was never
	// submitted).
	ErrTaskNotFound = errors.New("backup: task not found")

	// ErrCycleDetected is returned by Submit when the submitted task's
	// DependsOn list, together with already submitted tasks, would form
	// a dependency cycle.
	ErrCycleDetected = errors.New("backup: dependency cycle detected")

	// ErrDependencyFailed is set on a task when one of its dependencies
	// finished unsuccessfully (failed or timed out).
	ErrDependencyFailed = errors.New("backup: dependency failed")

	// ErrDependencyCancelled is set on a task when one of its
	// dependencies was cancelled before it could complete.
	ErrDependencyCancelled = errors.New("backup: dependency cancelled")

	// ErrCancelled is set on a task whose work was cancelled by the
	// user or by engine shutdown.
	ErrCancelled = errors.New("backup: task cancelled")

	// ErrInvalidState is returned by handle operations that are not
	// applicable in the task's current state, e.g. Pause on a task that
	// has already finished.
	ErrInvalidState = errors.New("backup: invalid task state for operation")
)

// Validation errors.
var (
	// ErrInvalidTask is returned when a submitted Task fails basic
	// validation (empty ID, nil Work, negative timeout/retry fields).
	ErrInvalidTask = errors.New("backup: invalid task")

	// ErrInvalidProgress is returned by Reporter.Report when the
	// percentage is outside [0, 100].
	ErrInvalidProgress = errors.New("backup: progress percentage out of range [0,100]")

	// ErrInvalidWatcher is returned when a Task.Watchers entry fails
	// validation (non-positive Interval or nil Check).
	ErrInvalidWatcher = errors.New("backup: invalid watcher")
)
