package backup

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/kisun-bit/drpkg/logger"
)

// RunMode controls how many engines may run concurrently.
type RunMode int

// Run modes.
const (
	// MultiInstance allows any number of engines in this process and on
	// this host. This is the default.
	MultiInstance RunMode = iota
	// ProcessMutex allows at most one engine in the current process.
	ProcessMutex
	// HostSingleton allows at most one engine on the whole host,
	// enforced by an OS file lock; see WithLockPath.
	HostSingleton
)

// String returns the mode name.
func (m RunMode) String() string {
	switch m {
	case MultiInstance:
		return "multi-instance"
	case ProcessMutex:
		return "process-mutex"
	case HostSingleton:
		return "host-singleton"
	default:
		return "unknown"
	}
}

// Option configures an Engine created by New.
type Option func(*options)

type options struct {
	mode        RunMode
	lockPath    string
	concurrency int
	eventBuffer int
}

// WithMode selects the engine run mode. Default: MultiInstance.
func WithMode(m RunMode) Option {
	return func(o *options) { o.mode = m }
}

// WithLockPath sets the lock file path used by HostSingleton mode.
func WithLockPath(path string) Option {
	return func(o *options) { o.lockPath = path }
}

// WithConcurrency sets the maximum number of tasks executed in parallel.
// Non-positive values keep the default (runtime.NumCPU).
func WithConcurrency(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.concurrency = n
		}
	}
}

// WithEventBuffer sets the default subscriber channel buffer used by
// Subscribe when its buffer argument is not positive.
func WithEventBuffer(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.eventBuffer = n
		}
	}
}

var (
	processModeMu      sync.Mutex
	processModeRunning bool
)

// Engine schedules and controls backup tasks.
type Engine struct {
	opts options
	hub  *eventHub

	mu           sync.Mutex
	items        map[string]*taskItem
	started      bool
	closed       bool
	engineCtx    context.Context
	engineCancel context.CancelFunc
	stopCh       chan struct{}
	wake         chan struct{}
	workerSem    chan struct{}
	wg           sync.WaitGroup

	lock     fileLock
	procHeld bool
	released bool
}

// New creates an engine. In HostSingleton mode the OS file lock is taken
// immediately; New returns ErrEngineAlreadyRunning when another process
// holds it. In ProcessMutex mode New fails when another engine in this
// process has not been shut down yet.
func New(opts ...Option) (*Engine, error) {
	o := options{
		mode:        MultiInstance,
		concurrency: runtime.NumCPU(),
		eventBuffer: 256,
	}
	for _, opt := range opts {
		opt(&o)
	}

	e := &Engine{
		opts:      o,
		hub:       newEventHub(),
		items:     make(map[string]*taskItem),
		workerSem: make(chan struct{}, o.concurrency),
	}

	switch o.mode {
	case ProcessMutex:
		processModeMu.Lock()
		if processModeRunning {
			processModeMu.Unlock()
			return nil, ErrEngineAlreadyRunning
		}
		processModeRunning = true
		processModeMu.Unlock()
		e.procHeld = true
	case HostSingleton:
		if o.lockPath == "" {
			return nil, ErrLockPathRequired
		}
		lk, err := acquireFileLock(o.lockPath)
		if err != nil {
			return nil, err
		}
		e.lock = lk
	}
	return e, nil
}

// Start launches the scheduler. It is idempotent and returns ErrEngineClosed
// after Shutdown.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrEngineClosed
	}
	if e.started {
		return nil
	}
	e.engineCtx, e.engineCancel = context.WithCancel(context.Background())
	e.stopCh = make(chan struct{})
	e.wake = make(chan struct{}, 1)
	e.wg.Add(1)
	go e.scheduler()
	e.started = true
	logger.Debugf("backup: engine started (mode=%s, concurrency=%d)", e.opts.mode, e.opts.concurrency)
	return nil
}

// Submit validates and registers a task, returning its control Handle.
// Dependencies must already be submitted; because of this ordering a
// dependency cycle other than a self-dependency cannot form.
func (e *Engine) Submit(t Task) (*Handle, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrEngineClosed
	}
	if !e.started {
		return nil, ErrEngineNotStarted
	}
	if _, ok := e.items[t.ID]; ok {
		return nil, fmt.Errorf("%v: %s", ErrTaskExists, t.ID)
	}
	for _, d := range t.DependsOn {
		if _, ok := e.items[d]; !ok {
			return nil, fmt.Errorf("%v: %s depends on unknown task %q", ErrTaskNotFound, t.ID, d)
		}
	}

	// Register as dependent and count dependencies that are not
	// finished yet. Dependencies already in a terminal state cannot
	// fire their completion path again, so resolve them right here.
	pending := 0
	var depErr error
	for _, d := range t.DependsOn {
		dep := e.items[d]
		dep.mu.Lock()
		depFinished, depSt := dep.finished, dep.state
		dep.mu.Unlock()
		dep.dependents = append(dep.dependents, t.ID)
		if !depFinished {
			pending++
			continue
		}
		if depSt == StateCompleted || depErr != nil {
			continue
		}
		if depSt == StateCancelled {
			depErr = fmt.Errorf("%v: dependency %q was cancelled", ErrDependencyCancelled, d)
		} else {
			depErr = fmt.Errorf("%v: dependency %q failed", ErrDependencyFailed, d)
		}
	}

	it := &taskItem{
		engine:      e,
		id:          t.ID,
		task:        t,
		state:       StatePending,
		doneCh:      make(chan struct{}),
		gate:        newPauseGate(),
		depsPending: pending,
	}
	if pending > 0 && depErr == nil {
		it.state = StateWaiting
	}
	it.taskCtx, it.taskCancel = context.WithCancel(e.engineCtx)
	e.items[t.ID] = it

	e.hub.publish(Event{Kind: EventSubmitted, TaskID: t.ID, State: it.state})
	if depErr != nil {
		e.finalizeLocked(it, StateFailed, depErr)
		return &Handle{item: it}, nil
	}
	e.wakeLocked()
	return &Handle{item: it}, nil
}

// SubmitSync submits the task and blocks until it reaches a terminal
// state, providing a synchronous execution mode on top of the scheduler.
// The task still runs through the engine with full timeout, retry,
// dependency, progress and event support; only the caller blocks.
//
// It returns the task handle and nil when the task completed
// successfully, and the task's final error when it finished
// unsuccessfully. When ctx is done before the task finishes, the task is
// cancelled and ctx.Err() is returned, so SubmitSync never leaves a task
// running after it returns. Submit failures (validation, duplicate ID,
// engine state) are returned directly with a nil handle.
func (e *Engine) SubmitSync(ctx context.Context, t Task) (*Handle, error) {
	h, err := e.Submit(t)
	if err != nil {
		return nil, err
	}
	err = h.Wait(ctx)
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		// The caller gave up waiting: stop the task.
		_ = h.Cancel()
	}
	return h, err
}

// Subscribe registers an event consumer. If buffer is not positive the
// engine default (WithEventBuffer) is used. It returns the event channel
// and an unsubscribe function that closes the channel.
func (e *Engine) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = e.opts.eventBuffer
	}
	return e.hub.subscribe(buffer)
}

// Shutdown stops the engine: no further Submit is accepted, the scheduler
// stops, non-running tasks are cancelled, running tasks get their context
// cancelled and are awaited until ctx is done. If ctx expires first, the
// remaining tasks are force-finalized as Cancelled and ctx.Err() is
// returned. Shutdown releases the run-mode reservation (process flag or
// host lock). Work functions that ignore ctx cancellation leak their
// goroutines past a timed-out Shutdown.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	wasStarted := e.started
	if wasStarted {
		close(e.stopCh)
	}
	e.mu.Unlock()

	if !wasStarted {
		e.releaseMode()
		return nil
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = ctx.Err()
		e.engineCancel() // unblock stragglers below
		e.mu.Lock()
		for _, it := range e.items {
			e.finalizeLocked(it, StateCancelled, ErrCancelled)
		}
		e.mu.Unlock()
	}

	if e.engineCancel != nil {
		e.engineCancel()
	}
	e.releaseMode()
	logger.Debugf("backup: engine shut down")
	return err
}

// releaseMode gives back the process-level flag or host file lock, once.
func (e *Engine) releaseMode() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.released {
		return
	}
	e.released = true
	if e.lock != nil {
		if err := e.lock.Unlock(); err != nil {
			logger.Warnf("backup: release host lock: %v", err)
		}
		e.lock = nil
	}
	if e.procHeld {
		processModeMu.Lock()
		processModeRunning = false
		processModeMu.Unlock()
		e.procHeld = false
	}
}

// scheduler picks ready tasks and launches workers until stopped.
func (e *Engine) scheduler() {
	defer e.wg.Done()
	defer e.onSchedulerExit()
	for {
		e.mu.Lock()
		ready := e.pickReadyLocked()
		e.launchLocked(ready)
		e.mu.Unlock()

		select {
		case <-e.wake:
		case <-e.stopCh:
			return
		}
	}
}

// onSchedulerExit cancels every task that is still alive: non-running
// tasks are finalized immediately, running ones get their context
// cancelled and are finalized by their workers.
func (e *Engine) onSchedulerExit() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.started = false
	for _, it := range e.items {
		it.mu.Lock()
		finished := it.finished
		st := it.state
		it.mu.Unlock()
		if finished {
			continue
		}
		switch st {
		case StatePending, StateWaiting:
			e.finalizeLocked(it, StateCancelled, ErrCancelled)
		case StateRunning, StatePaused:
			it.taskCancel()
		}
	}
}

// pickReadyLocked returns schedulable tasks: pending tasks without
// unfinished dependencies and waiting tasks whose dependencies are all
// satisfied. Tasks already owned by a worker (inFlight) are excluded.
func (e *Engine) pickReadyLocked() []*taskItem {
	var ready []*taskItem
	for _, it := range e.items {
		if it.inFlight {
			continue
		}
		it.mu.Lock()
		finished := it.finished
		st := it.state
		it.mu.Unlock()
		if finished {
			continue
		}
		if st == StatePending || (st == StateWaiting && it.depsPending == 0) {
			ready = append(ready, it)
		}
	}
	return ready
}

// launchLocked starts a worker for each ready task, plus one goroutine
// per configured watcher.
func (e *Engine) launchLocked(ready []*taskItem) {
	for _, it := range ready {
		if it.inFlight {
			continue
		}
		it.mu.Lock()
		if it.finished {
			it.mu.Unlock()
			continue
		}
		it.state = StateRunning
		it.mu.Unlock()
		it.inFlight = true
		it.generation++
		e.hub.publish(Event{Kind: EventStateChange, TaskID: it.id, State: StateRunning})
		e.wg.Add(1)
		go e.worker(it, it.generation)

		// Watchers share the task's context: they stop automatically
		// when the task reaches a terminal state (taskCtx is cancelled
		// on finalize) or when the run is replaced by a manual Retry.
		if len(it.task.Watchers) > 0 {
			taskCtx := it.taskCtx
			for i := range it.task.Watchers {
				e.wg.Add(1)
				go e.watcherLoop(it, it.task.Watchers[i], taskCtx, it.generation)
			}
		}
	}
}

// worker runs task attempts until the task reaches a terminal state. gen
// identifies this worker's ownership of the task: a stale worker (one
// that outlived a manual Retry) neither clears the scheduling flags nor
// finalizes the task.
func (e *Engine) worker(it *taskItem, gen int) {
	defer e.wg.Done()
	defer func() {
		e.mu.Lock()
		if it.generation == gen {
			it.inFlight = false
		}
		e.mu.Unlock()
	}()
	for {
		if it.isFinished() {
			return
		}
		e.mu.Lock()
		stale := it.generation != gen
		taskCtx := it.taskCtx
		e.mu.Unlock()
		if stale {
			return
		}

		// Honor the pause gate before (re)starting an attempt.
		if it.gate.isPaused() {
			if err := e.pauseWait(it, taskCtx); err != nil {
				e.mu.Lock()
				stale = it.generation != gen
				e.mu.Unlock()
				if !stale {
					e.finalize(it, StateCancelled, ErrCancelled)
				}
				return
			}
			continue // re-check ownership and state at the loop top
		}

		// Acquire a concurrency slot.
		select {
		case e.workerSem <- struct{}{}:
		case <-taskCtx.Done():
			e.mu.Lock()
			stale = it.generation != gen
			e.mu.Unlock()
			if !stale {
				e.finalize(it, StateCancelled, ErrCancelled)
			}
			return
		}

		attemptCtx := taskCtx
		cancel := context.CancelFunc(func() {})
		if it.task.Timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(taskCtx, it.task.Timeout)
		}

		it.mu.Lock()
		it.attempt++
		attempt := it.attempt
		it.mu.Unlock()

		err := e.runWork(it, attemptCtx)
		ctxErr := attemptCtx.Err()
		cancel()
		<-e.workerSem // release slot

		e.mu.Lock()
		stale = it.generation != gen
		e.mu.Unlock()
		if stale || it.isFinished() {
			return
		}

		timedOut := errors.Is(ctxErr, context.DeadlineExceeded)
		userCancelled := errors.Is(ctxErr, context.Canceled)

		switch {
		case err == nil:
			e.finalize(it, StateCompleted, nil)
			return
		case userCancelled:
			e.finalize(it, StateCancelled, ErrCancelled)
			return
		case timedOut && attempt > it.task.Retry:
			if err == nil {
				err = fmt.Errorf("backup: task %s timed out after %s", it.id, it.task.Timeout)
			}
			e.finalize(it, StateTimedOut, err)
			return
		case attempt > it.task.Retry:
			e.finalize(it, StateFailed, err)
			return
		}

		// Schedule a retry: wait out the backoff, then loop.
		e.transition(it, StateWaiting)
		e.hub.publish(Event{Kind: EventRetry, TaskID: it.id, Attempt: attempt, Err: err, State: StateWaiting})
		if it.task.RetryBackoff > 0 {
			timer := time.NewTimer(it.task.RetryBackoff)
			select {
			case <-timer.C:
			case <-taskCtx.Done():
				timer.Stop()
				e.mu.Lock()
				stale = it.generation != gen
				e.mu.Unlock()
				if !stale {
					e.finalize(it, StateCancelled, ErrCancelled)
				}
				return
			}
		}
	}
}

// runWork invokes the task's WorkFunc and converts panics into errors.
func (e *Engine) runWork(it *taskItem, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("backup: task %s panicked: %v", it.id, r)
			err = fmt.Errorf("backup: task %s panicked: %v", it.id, r)
		}
	}()
	return it.task.Work(ctx, &Reporter{item: it})
}

// pauseWait blocks while the task's pause gate is closed, keeping the
// task state in sync (StatePaused while waiting, StateRunning after
// resume). It returns ctx.Err() if ctx is cancelled while paused, and
// nil immediately when the gate is open.
func (e *Engine) pauseWait(it *taskItem, ctx context.Context) error {
	if !it.gate.isPaused() {
		return nil
	}
	e.transition(it, StatePaused)
	err := it.gate.waitCtx(ctx)
	if err == nil && !it.isFinished() {
		e.transition(it, StateRunning)
	}
	return err
}

// watcherLoop runs one Watcher periodically until ctx is done. ctx and
// gen are the task context and generation captured at launch, so the loop
// stops automatically when the task reaches a terminal state or the run is
// replaced by a manual Retry.
func (e *Engine) watcherLoop(it *taskItem, w Watcher, ctx context.Context, gen int) {
	defer e.wg.Done()

	if w.Immediate && e.runWatcherCheck(it, w, ctx, gen) {
		return
	}

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.runWatcherCheck(it, w, ctx, gen) {
				return
			}
		}
	}
}

// runWatcherCheck invokes one watcher check with panic protection and
// applies its verdict to the task, guarded by the launch-time generation
// so a check that outlives a manual Retry cannot affect the new run. It
// returns true when the loop should stop: the check terminated the task
// or its run is stale.
func (e *Engine) runWatcherCheck(it *taskItem, w Watcher, ctx context.Context, gen int) bool {
	e.mu.Lock()
	stale := it.generation != gen
	e.mu.Unlock()
	if stale {
		return true
	}

	h := &Handle{item: it}
	action, err := func() (a WatcherAction, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("backup: watcher of task %s panicked: %v", it.id, r)
				a, err = ActionContinue, nil
			}
		}()
		return w.Check(ctx, h)
	}()

	switch action {
	case ActionCancel:
		e.mu.Lock()
		if it.generation == gen {
			_ = e.cancelLocked(it)
		}
		e.mu.Unlock()
		return true
	case ActionFail:
		if err == nil {
			err = fmt.Errorf("backup: watcher of task %s failed the task", it.id)
		}
		e.mu.Lock()
		if it.generation == gen {
			e.finalizeLocked(it, StateFailed, err)
		}
		e.mu.Unlock()
		return true
	default: // ActionContinue
		return false
	}
}

// transition changes a task's state and publishes EventStateChange.
func (e *Engine) transition(it *taskItem, st TaskState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	it.mu.Lock()
	if it.finished || it.state == st {
		it.mu.Unlock()
		return
	}
	it.state = st
	it.mu.Unlock()
	e.hub.publish(Event{Kind: EventStateChange, TaskID: it.id, State: st})
}

// finalize moves a task to a terminal state exactly once, publishes
// EventFinal, notifies dependents and wakes the scheduler.
func (e *Engine) finalize(it *taskItem, st TaskState, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.finalizeLocked(it, st, err)
}

// cancelLocked requests cancellation while the caller holds e.mu.
// Non-running tasks are finalized immediately; running or paused tasks
// have their context cancelled and finalize once the WorkFunc returns.
func (e *Engine) cancelLocked(it *taskItem) error {
	it.mu.Lock()
	finished := it.finished
	st := it.state
	it.mu.Unlock()
	if finished {
		return ErrInvalidState
	}
	switch st {
	case StatePending, StateWaiting:
		e.finalizeLocked(it, StateCancelled, ErrCancelled)
	default: // StateRunning, StatePaused
		it.taskCancel()
	}
	e.wakeLocked()
	return nil
}

// publish forwards an event to all subscribers without blocking.
func (e *Engine) publish(ev Event) {
	e.hub.publish(ev)
}

// finalizeLocked is finalize for callers holding e.mu.
func (e *Engine) finalizeLocked(it *taskItem, st TaskState, err error) {
	it.mu.Lock()
	if it.finished {
		it.mu.Unlock()
		return
	}
	it.finished = true
	it.state = st
	it.err = err
	prog := it.progress
	close(it.doneCh)
	it.mu.Unlock()

	if it.taskCancel != nil {
		it.taskCancel()
	}
	e.hub.publish(Event{Kind: EventFinal, TaskID: it.id, State: st, Err: err, Progress: prog})
	logger.Debugf("backup: task %s finished: state=%s err=%v", it.id, st, err)
	e.onTerminalLocked(it)
	e.wakeLocked()
}

// onTerminalLocked updates dependents of a task that reached a terminal
// state: success decrements their pending counter; failure or cancellation
// finalizes them with the corresponding dependency error. Dependents that
// are already running are left untouched.
func (e *Engine) onTerminalLocked(it *taskItem) {
	for _, id := range it.dependents {
		dep := e.items[id]
		if dep == nil || dep.inFlight {
			continue
		}
		dep.mu.Lock()
		finished := dep.finished
		dep.mu.Unlock()
		if finished {
			continue
		}
		switch it.state {
		case StateCompleted:
			if dep.depsPending > 0 {
				dep.depsPending--
			}
		case StateCancelled:
			e.finalizeLocked(dep, StateFailed,
				fmt.Errorf("%v: dependency %q was cancelled", ErrDependencyCancelled, it.id))
		default: // Failed, TimedOut
			e.finalizeLocked(dep, StateFailed,
				fmt.Errorf("%v: dependency %q failed", ErrDependencyFailed, it.id))
		}
	}
}

// wakeLocked signals the scheduler that new work may be schedulable.
func (e *Engine) wakeLocked() {
	if e.wake == nil {
		return
	}
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// taskItem is the engine's internal record for one submitted task.
type taskItem struct {
	engine *Engine
	id     string
	task   Task

	// Fields below are guarded by mu.
	mu       sync.Mutex
	state    TaskState
	err      error
	progress Progress
	attempt  int
	finished bool
	doneCh   chan struct{}

	gate *pauseGate

	// Fields below are guarded by engine.mu.
	depsPending int
	dependents  []string
	inFlight    bool
	generation  int // incremented each time a worker takes ownership

	taskCtx    context.Context
	taskCancel context.CancelFunc
}

// isFinished reports whether the task reached a terminal state.
func (it *taskItem) isFinished() bool {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.finished
}
