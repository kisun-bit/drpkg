package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

const waitLimit = 5 * time.Second

// startEngine creates and starts an engine, shutting it down during test
// cleanup.
func startEngine(t *testing.T, opts ...Option) *Engine {
	t.Helper()
	e, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = e.Shutdown(ctx)
	})
	return e
}

// waitFor polls cond until it returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", msg)
}

// waitCtx returns a context that fails the test when it expires.
func waitCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), waitLimit)
}

// drainUntilFinal consumes events for taskID until its EventFinal arrives.
func drainUntilFinal(t *testing.T, ch <-chan Event, taskID string, timeout time.Duration) []Event {
	t.Helper()
	var events []Event
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-ch:
			if ev.TaskID != taskID {
				continue
			}
			events = append(events, ev)
			if ev.Kind == EventFinal {
				return events
			}
		case <-deadline:
			t.Fatalf("timeout waiting for final event of %q; got %+v", taskID, events)
			return nil
		}
	}
}

func hasKindState(events []Event, kind EventKind, st TaskState) bool {
	for _, ev := range events {
		if ev.Kind == kind && ev.State == st {
			return true
		}
	}
	return false
}

func hasProgress(events []Event, pct int32) bool {
	for _, ev := range events {
		if ev.Kind == EventProgress && ev.Progress.Percent == pct {
			return true
		}
	}
	return false
}

func countKind(events []Event, kind EventKind) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// blockingWork returns a WorkFunc that blocks until ctx is done.
func blockingWork() WorkFunc {
	return func(ctx context.Context, rep *Reporter) error {
		<-ctx.Done()
		return ctx.Err()
	}
}

// errHas reports whether err matches the sentinel either through the
// error chain or by message text (the engine wraps some errors without
// preserving the chain).
func errHas(err error, sentinel error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, sentinel) || strings.Contains(err.Error(), sentinel.Error())
}

func TestLifecycleAndEvents(t *testing.T) {
	e := startEngine(t)
	ch, unsubscribe := e.Subscribe(64)
	defer unsubscribe()

	h, err := e.Submit(Task{
		ID: "t1",
		Work: func(ctx context.Context, rep *Reporter) error {
			return rep.Report(50, "half")
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	events := drainUntilFinal(t, ch, "t1", waitLimit)
	if events[0].Kind != EventSubmitted {
		t.Errorf("first event = %v, want submitted", events[0].Kind)
	}
	last := events[len(events)-1]
	if last.Kind != EventFinal || last.State != StateCompleted {
		t.Errorf("last event = %+v, want final/completed", last)
	}
	if !hasKindState(events, EventStateChange, StateRunning) {
		t.Errorf("missing state_change to running: %+v", events)
	}
	if !hasProgress(events, 50) {
		t.Errorf("missing progress 50: %+v", events)
	}
	if got := h.Progress().Percent; got != 50 {
		t.Errorf("Progress().Percent = %d, want 50", got)
	}
}

func TestPauseResume(t *testing.T) {
	e := startEngine(t)
	started := make(chan struct{})
	var ticks atomic.Int32

	h, err := e.Submit(Task{
		ID: "p",
		Work: func(ctx context.Context, rep *Reporter) error {
			select {
			case <-started:
			default:
				close(started)
			}
			for {
				if err := rep.Checkpoint(ctx); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Millisecond):
					ticks.Add(1)
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-started
	waitFor(t, waitLimit, "task running", func() bool { return h.State() == StateRunning })

	if err := h.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	waitFor(t, waitLimit, "task paused", func() bool { return h.State() == StatePaused })

	frozen := ticks.Load()
	time.Sleep(30 * time.Millisecond)
	if got := ticks.Load(); got != frozen {
		t.Errorf("ticks advanced while paused: %d -> %d", frozen, got)
	}
	if err := h.Pause(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second Pause = %v, want ErrInvalidState", err)
	}

	if err := h.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitFor(t, waitLimit, "task resumed", func() bool { return h.State() == StateRunning })
	if err := h.Resume(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second Resume = %v, want ErrInvalidState", err)
	}

	if err := h.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFor(t, waitLimit, "task cancelled", func() bool { return h.State() == StateCancelled })
}

func TestCancelRunningTask(t *testing.T) {
	e := startEngine(t)
	h, err := e.Submit(Task{ID: "c", Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitFor(t, waitLimit, "task running", func() bool { return h.State() == StateRunning })

	if err := h.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Wait = %v, want ErrCancelled", err)
	}
	if h.State() != StateCancelled {
		t.Errorf("state = %v, want cancelled", h.State())
	}
	if err := h.Cancel(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Cancel after finish = %v, want ErrInvalidState", err)
	}
}

func TestRetry(t *testing.T) {
	tests := []struct {
		name         string
		failFirst    int32
		retry        int
		wantState    TaskState
		wantAttempts int32
		wantRetries  int
	}{
		{"succeeds after failures", 2, 3, StateCompleted, 3, 2},
		{"retries exhausted", 100, 2, StateFailed, 3, 2},
		{"no retry configured", 100, 0, StateFailed, 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := startEngine(t)
			ch, unsubscribe := e.Subscribe(64)
			defer unsubscribe()

			var attempts atomic.Int32
			h, err := e.Submit(Task{
				ID:           "r-" + tc.name,
				Retry:        tc.retry,
				RetryBackoff: 5 * time.Millisecond,
				Work: func(ctx context.Context, rep *Reporter) error {
					if attempts.Add(1) <= tc.failFirst {
						return errBoom
					}
					return nil
				},
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			ctx, cancel := waitCtx(t)
			defer cancel()
			_ = h.Wait(ctx)

			if h.State() != tc.wantState {
				t.Errorf("state = %v, want %v (err=%v)", h.State(), tc.wantState, h.Err())
			}
			if got := attempts.Load(); got != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tc.wantAttempts)
			}
			events := drainUntilFinal(t, ch, h.ID(), waitLimit)
			if got := countKind(events, EventRetry); got != tc.wantRetries {
				t.Errorf("retry events = %d, want %d", got, tc.wantRetries)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	tests := []struct {
		name         string
		retry        int
		wantAttempts int32
	}{
		{"single attempt", 0, 1},
		{"with retry", 1, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := startEngine(t)
			var attempts atomic.Int32
			h, err := e.Submit(Task{
				ID:      "to-" + tc.name,
				Timeout: 20 * time.Millisecond,
				Retry:   tc.retry,
				Work: func(ctx context.Context, rep *Reporter) error {
					attempts.Add(1)
					<-ctx.Done()
					return ctx.Err()
				},
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			ctx, cancel := waitCtx(t)
			defer cancel()
			if err := h.Wait(ctx); err == nil {
				t.Fatalf("Wait = nil, want timeout error")
			}
			if h.State() != StateTimedOut {
				t.Errorf("state = %v, want timedout", h.State())
			}
			if got := attempts.Load(); got != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tc.wantAttempts)
			}
		})
	}
}

func TestDependencyChain(t *testing.T) {
	e := startEngine(t)
	var mu sync.Mutex
	var order []string
	record := func(id string) {
		mu.Lock()
		order = append(order, id)
		mu.Unlock()
	}

	makeTask := func(id string, deps ...string) Task {
		return Task{
			ID:        id,
			DependsOn: deps,
			Work: func(ctx context.Context, rep *Reporter) error {
				record(id)
				return nil
			},
		}
	}

	handles := make([]*Handle, 0, 3)
	for _, task := range []Task{
		makeTask("a"),
		makeTask("b", "a"),
		makeTask("c", "b"),
	} {
		h, err := e.Submit(task)
		if err != nil {
			t.Fatalf("Submit %s: %v", task.ID, err)
		}
		handles = append(handles, h)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	for _, h := range handles {
		if err := h.Wait(ctx); err != nil {
			t.Fatalf("Wait %s: %v", h.ID(), err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(order) != "[a b c]" {
		t.Errorf("order = %v, want [a b c]", order)
	}
}

func TestDependencyFailurePropagation(t *testing.T) {
	tests := []struct {
		name    string
		depWork WorkFunc
		cancel  bool
		wantErr error
	}{
		{
			name:    "dependency failed",
			depWork: func(ctx context.Context, rep *Reporter) error { return errBoom },
			wantErr: ErrDependencyFailed,
		},
		{
			name:    "dependency cancelled",
			depWork: blockingWork(),
			cancel:  true,
			wantErr: ErrDependencyCancelled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := startEngine(t)
			dep, err := e.Submit(Task{ID: "dep", Work: tc.depWork})
			if err != nil {
				t.Fatalf("Submit dep: %v", err)
			}
			h, err := e.Submit(Task{ID: "child", DependsOn: []string{"dep"}, Work: blockingWork()})
			if err != nil {
				t.Fatalf("Submit child: %v", err)
			}
			if tc.cancel {
				waitFor(t, waitLimit, "dep running", func() bool { return dep.State() == StateRunning })
				if err := dep.Cancel(); err != nil {
					t.Fatalf("Cancel dep: %v", err)
				}
			}
			ctx, cancel := waitCtx(t)
			defer cancel()
			err = h.Wait(ctx)
			if !errHas(err, tc.wantErr) {
				t.Fatalf("child Wait = %v, want %v", err, tc.wantErr)
			}
			if h.State() != StateFailed {
				t.Errorf("child state = %v, want failed", h.State())
			}
		})
	}
}

func TestSubmitAfterDependencyTerminal(t *testing.T) {
	e := startEngine(t)
	ctx, cancel := waitCtx(t)
	defer cancel()

	ok, err := e.Submit(Task{ID: "ok", Work: func(ctx context.Context, rep *Reporter) error { return nil }})
	if err != nil {
		t.Fatalf("Submit ok: %v", err)
	}
	if err := ok.Wait(ctx); err != nil {
		t.Fatalf("Wait ok: %v", err)
	}
	after, err := e.Submit(Task{ID: "after-ok", DependsOn: []string{"ok"}, Work: func(ctx context.Context, rep *Reporter) error { return nil }})
	if err != nil {
		t.Fatalf("Submit after-ok: %v", err)
	}
	if err := after.Wait(ctx); err != nil {
		t.Fatalf("after-ok should run despite finished dep: %v", err)
	}

	bad, err := e.Submit(Task{ID: "bad", Work: func(ctx context.Context, rep *Reporter) error { return errBoom }})
	if err != nil {
		t.Fatalf("Submit bad: %v", err)
	}
	if err := bad.Wait(ctx); err == nil {
		t.Fatalf("bad should fail")
	}
	afterBad, err := e.Submit(Task{ID: "after-bad", DependsOn: []string{"bad"}, Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit after-bad: %v", err)
	}
	if err := afterBad.Wait(ctx); !errHas(err, ErrDependencyFailed) {
		t.Fatalf("after-bad Wait = %v, want ErrDependencyFailed", err)
	}
}

func TestDependencyValidation(t *testing.T) {
	e := startEngine(t)
	_, err := e.Submit(Task{ID: "self", DependsOn: []string{"self"}, Work: blockingWork()})
	if !errHas(err, ErrCycleDetected) {
		t.Errorf("self dependency = %v, want ErrCycleDetected", err)
	}
	_, err = e.Submit(Task{ID: "ghost", DependsOn: []string{"nope"}, Work: blockingWork()})
	if !errHas(err, ErrTaskNotFound) {
		t.Errorf("unknown dependency = %v, want ErrTaskNotFound", err)
	}
}

func TestSubmitValidation(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Shutdown(context.Background()) })

	_, err = e.Submit(Task{ID: "x", Work: blockingWork()})
	if !errors.Is(err, ErrEngineNotStarted) {
		t.Fatalf("Submit before Start = %v, want ErrEngineNotStarted", err)
	}
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err = e.Submit(Task{})
	if !errHas(err, ErrInvalidTask) {
		t.Errorf("empty ID = %v, want ErrInvalidTask", err)
	}
	_, err = e.Submit(Task{ID: "nilwork"})
	if !errHas(err, ErrInvalidTask) {
		t.Errorf("nil Work = %v, want ErrInvalidTask", err)
	}
	_, err = e.Submit(Task{ID: "negtimeout", Work: blockingWork(), Timeout: -time.Second})
	if !errHas(err, ErrInvalidTask) {
		t.Errorf("negative Timeout = %v, want ErrInvalidTask", err)
	}

	if _, err := e.Submit(Task{ID: "dup", Work: blockingWork()}); err != nil {
		t.Fatalf("Submit dup: %v", err)
	}
	_, err = e.Submit(Task{ID: "dup", Work: blockingWork()})
	if !errHas(err, ErrTaskExists) {
		t.Errorf("duplicate ID = %v, want ErrTaskExists", err)
	}
}

func TestManualRetry(t *testing.T) {
	e := startEngine(t)
	var attempts atomic.Int32
	h, err := e.Submit(Task{
		ID: "manual",
		Work: func(ctx context.Context, rep *Reporter) error {
			if attempts.Add(1) == 1 {
				return errBoom
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); err == nil {
		t.Fatalf("first run should fail")
	}
	if h.State() != StateFailed {
		t.Fatalf("state = %v, want failed", h.State())
	}

	if err := h.Retry(); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if err := h.Wait(ctx); err != nil {
		t.Fatalf("Wait after Retry: %v", err)
	}
	if h.State() != StateCompleted || attempts.Load() != 2 {
		t.Errorf("state=%v attempts=%d, want completed/2", h.State(), attempts.Load())
	}
	if err := h.Retry(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Retry completed task = %v, want ErrInvalidState", err)
	}

	h2, err := e.Submit(Task{ID: "active", Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit active: %v", err)
	}
	waitFor(t, waitLimit, "active running", func() bool { return h2.State() == StateRunning })
	if err := h2.Retry(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("Retry active task = %v, want ErrInvalidState", err)
	}
	_ = h2.Cancel()
}

func TestReporterValidation(t *testing.T) {
	e := startEngine(t)
	var reportErrs []error
	var mu sync.Mutex
	h, err := e.Submit(Task{
		ID: "rep",
		Work: func(ctx context.Context, rep *Reporter) error {
			mu.Lock()
			reportErrs = append(reportErrs, rep.Report(-1, "low"))
			reportErrs = append(reportErrs, rep.Report(101, "high"))
			reportErrs = append(reportErrs, rep.Report(100, "done"))
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reportErrs) != 3 {
		t.Fatalf("reportErrs = %v", reportErrs)
	}
	if !errors.Is(reportErrs[0], ErrInvalidProgress) || !errors.Is(reportErrs[1], ErrInvalidProgress) {
		t.Errorf("out-of-range reports = %v, want ErrInvalidProgress", reportErrs[:2])
	}
	if reportErrs[2] != nil {
		t.Errorf("valid report = %v, want nil", reportErrs[2])
	}
	if got := h.Progress(); got.Percent != 100 || got.Message != "done" {
		t.Errorf("Progress = %+v, want 100/done", got)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	e := startEngine(t, WithConcurrency(2))
	var running, maxRunning atomic.Int32
	var wg sync.WaitGroup

	handles := make([]*Handle, 0, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		h, err := e.Submit(Task{
			ID: fmt.Sprintf("cc-%d", i),
			Work: func(ctx context.Context, rep *Reporter) error {
				defer wg.Done()
				cur := running.Add(1)
				for {
					old := maxRunning.Load()
					if cur <= old || maxRunning.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				running.Add(-1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
		handles = append(handles, h)
	}
	wg.Wait()
	ctx, cancel := waitCtx(t)
	defer cancel()
	for _, h := range handles {
		if err := h.Wait(ctx); err != nil {
			t.Fatalf("Wait %s: %v", h.ID(), err)
		}
	}
	if got := maxRunning.Load(); got > 2 {
		t.Errorf("max concurrent = %d, want <= 2", got)
	}
}

func TestShutdownCancelsAll(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h1, err := e.Submit(Task{ID: "s1", Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit s1: %v", err)
	}
	h2, err := e.Submit(Task{ID: "s2", Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit s2: %v", err)
	}
	waitFor(t, waitLimit, "both running", func() bool {
		return h1.State() == StateRunning && h2.State() == StateRunning
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if h1.State() != StateCancelled || h2.State() != StateCancelled {
		t.Errorf("states = %v/%v, want cancelled/cancelled", h1.State(), h2.State())
	}
	if _, err := e.Submit(Task{ID: "late", Work: blockingWork()}); !errors.Is(err, ErrEngineClosed) {
		t.Errorf("Submit after Shutdown = %v, want ErrEngineClosed", err)
	}
}

func TestWaitContextTimeout(t *testing.T) {
	e := startEngine(t)
	h, err := e.Submit(Task{ID: "slow", Work: blockingWork()})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait = %v, want DeadlineExceeded", err)
	}
	_ = h.Cancel()
}

func TestProcessMutexMode(t *testing.T) {
	e1, err := New(WithMode(ProcessMutex))
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	if err := e1.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, err = New(WithMode(ProcessMutex))
	if !errors.Is(err, ErrEngineAlreadyRunning) {
		t.Fatalf("New second = %v, want ErrEngineAlreadyRunning", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e1.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	e3, err := New(WithMode(ProcessMutex))
	if err != nil {
		t.Fatalf("New after shutdown: %v", err)
	}
	if err := e3.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown e3: %v", err)
	}
}

func TestMultiInstanceMode(t *testing.T) {
	e1 := startEngine(t)
	e2, err := New()
	if err != nil {
		t.Fatalf("New second multi-instance engine: %v", err)
	}
	if err := e2.Start(); err != nil {
		t.Fatalf("Start e2: %v", err)
	}
	defer func() { _ = e2.Shutdown(context.Background()) }()

	h, err := e2.Submit(Task{ID: "m", Work: func(ctx context.Context, rep *Reporter) error { return nil }})
	if err != nil {
		t.Fatalf("Submit on e2: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if e1 == nil {
		t.Fatal("e1 is nil")
	}
}

func TestHostSingletonMode(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "engine.lock")

	_, err := New(WithMode(HostSingleton))
	if !errors.Is(err, ErrLockPathRequired) {
		t.Fatalf("New without lock path = %v, want ErrLockPathRequired", err)
	}

	e1, err := New(WithMode(HostSingleton), WithLockPath(lockPath))
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	if err := e1.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err = New(WithMode(HostSingleton), WithLockPath(lockPath))
	if !errors.Is(err, ErrEngineAlreadyRunning) {
		t.Fatalf("New second = %v, want ErrEngineAlreadyRunning", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e1.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	e3, err := New(WithMode(HostSingleton), WithLockPath(lockPath))
	if err != nil {
		t.Fatalf("New after release: %v", err)
	}
	if err := e3.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown e3: %v", err)
	}
}

func TestSubmitSync(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		e := startEngine(t)
		var ran atomic.Bool
		h, err := e.SubmitSync(context.Background(), Task{
			ID: "sync-ok",
			Work: func(ctx context.Context, rep *Reporter) error {
				ran.Store(true)
				return rep.Report(100, "done")
			},
		})
		if err != nil {
			t.Fatalf("SubmitSync: %v", err)
		}
		if h.State() != StateCompleted {
			t.Errorf("state = %v, want completed", h.State())
		}
		if !ran.Load() {
			t.Errorf("work did not run")
		}
		if got := h.Progress().Percent; got != 100 {
			t.Errorf("progress = %d, want 100", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		e := startEngine(t)
		h, err := e.SubmitSync(context.Background(), Task{
			ID:   "sync-fail",
			Work: func(ctx context.Context, rep *Reporter) error { return errBoom },
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("SubmitSync = %v, want errBoom", err)
		}
		if h.State() != StateFailed {
			t.Errorf("state = %v, want failed", h.State())
		}
	})

	t.Run("context timeout cancels task", func(t *testing.T) {
		e := startEngine(t)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		h, err := e.SubmitSync(ctx, Task{ID: "sync-slow", Work: blockingWork()})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SubmitSync = %v, want DeadlineExceeded", err)
		}
		waitFor(t, waitLimit, "task cancelled", func() bool { return h.State() == StateCancelled })
	})

	t.Run("submit error", func(t *testing.T) {
		e := startEngine(t)
		if _, err := e.SubmitSync(context.Background(), Task{
			ID:   "sync-dup",
			Work: func(ctx context.Context, rep *Reporter) error { return nil },
		}); err != nil {
			t.Fatalf("first SubmitSync: %v", err)
		}
		h, err := e.SubmitSync(context.Background(), Task{ID: "sync-dup", Work: blockingWork()})
		if h != nil || !errHas(err, ErrTaskExists) {
			t.Fatalf("duplicate SubmitSync = (%v, %v), want (nil, ErrTaskExists)", h, err)
		}
	})
}

func TestWatcherValidation(t *testing.T) {
	e := startEngine(t)
	_, err := e.Submit(Task{
		ID:   "w-bad-interval",
		Work: blockingWork(),
		Watchers: []Watcher{{Interval: 0, Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
			return ActionContinue, nil
		}}},
	})
	if !errHas(err, ErrInvalidWatcher) {
		t.Errorf("zero interval = %v, want ErrInvalidWatcher", err)
	}
	_, err = e.Submit(Task{
		ID:       "w-bad-check",
		Work:     blockingWork(),
		Watchers: []Watcher{{Interval: time.Second}},
	})
	if !errHas(err, ErrInvalidWatcher) {
		t.Errorf("nil check = %v, want ErrInvalidWatcher", err)
	}
}

func TestWatcherFailAction(t *testing.T) {
	e := startEngine(t)
	watchErr := errors.New("target gone")
	var ticks atomic.Int32

	h, err := e.Submit(Task{
		ID:    "w-fail",
		Work:  blockingWork(),
		Retry: 0,
		Watchers: []Watcher{{
			Interval: 10 * time.Millisecond,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				if ticks.Add(1) >= 2 {
					return ActionFail, watchErr
				}
				return ActionContinue, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	err = h.Wait(ctx)
	if !errors.Is(err, watchErr) {
		t.Fatalf("Wait = %v, want watchErr", err)
	}
	if h.State() != StateFailed {
		t.Errorf("state = %v, want failed", h.State())
	}
	if got := ticks.Load(); got < 2 {
		t.Errorf("ticks = %d, want >= 2 (continue verdicts must be honored)", got)
	}
}

func TestWatcherCancelAction(t *testing.T) {
	e := startEngine(t)
	h, err := e.Submit(Task{
		ID:   "w-cancel",
		Work: blockingWork(),
		Watchers: []Watcher{{
			Interval: 10 * time.Millisecond,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				return ActionCancel, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	err = h.Wait(ctx)
	if !errHas(err, ErrCancelled) {
		t.Fatalf("Wait = %v, want ErrCancelled", err)
	}
	if h.State() != StateCancelled {
		t.Errorf("state = %v, want cancelled", h.State())
	}
}

func TestWatcherImmediate(t *testing.T) {
	e := startEngine(t)
	h, err := e.Submit(Task{
		ID:   "w-immediate",
		Work: blockingWork(),
		Watchers: []Watcher{{
			Interval:  10 * time.Second, // far beyond the test horizon
			Immediate: true,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				return ActionFail, errBoom
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("Wait = %v, want errBoom", err)
	}
}

func TestWatcherStopsOnTerminal(t *testing.T) {
	e := startEngine(t)
	var ticks atomic.Int32
	done := make(chan struct{})

	h, err := e.Submit(Task{
		ID: "w-stop",
		Work: func(ctx context.Context, rep *Reporter) error {
			_ = rep.Report(100, "done")
			close(done)
			return nil
		},
		Watchers: []Watcher{{
			Interval: 5 * time.Millisecond,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				ticks.Add(1)
				return ActionContinue, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	<-done
	time.Sleep(30 * time.Millisecond)
	after := ticks.Load()
	time.Sleep(30 * time.Millisecond)
	if got := ticks.Load(); got != after {
		t.Errorf("watcher kept ticking after terminal state: %d -> %d", after, got)
	}
}

func TestWatcherPanicIsContained(t *testing.T) {
	e := startEngine(t)
	h, err := e.Submit(Task{
		ID:   "w-panic",
		Work: blockingWork(),
		Watchers: []Watcher{{
			Interval: 5 * time.Millisecond,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				panic("watcher boom")
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitFor(t, waitLimit, "task still running despite watcher panics", func() bool {
		return h.State() == StateRunning
	})
	time.Sleep(20 * time.Millisecond)
	if h.State() != StateRunning {
		t.Fatalf("state = %v, want running (watcher panic must not kill the task)", h.State())
	}
	_ = h.Cancel()
}

func TestWatcherRearmsOnRetry(t *testing.T) {
	e := startEngine(t)
	var run atomic.Int32 // 0 = first run, 1 = after Retry
	var ticksRun2 atomic.Int32
	var attempts atomic.Int32

	h, err := e.Submit(Task{
		ID: "w-retry",
		Work: func(ctx context.Context, rep *Reporter) error {
			attempts.Add(1)
			<-ctx.Done()
			return ctx.Err()
		},
		Watchers: []Watcher{{
			Interval: 10 * time.Millisecond,
			Check: func(ctx context.Context, h *Handle) (WatcherAction, error) {
				if run.Load() == 0 {
					return ActionFail, errBoom
				}
				ticksRun2.Add(1)
				return ActionContinue, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := waitCtx(t)
	defer cancel()
	if err := h.Wait(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("first run Wait = %v, want errBoom", err)
	}

	run.Store(1)
	if err := h.Retry(); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// The re-armed watcher must start ticking again for the new run.
	waitFor(t, waitLimit, "watcher re-armed", func() bool { return ticksRun2.Load() >= 2 })
	// The second attempt must actually be executing.
	waitFor(t, waitLimit, "second attempt running", func() bool { return attempts.Load() == 2 })

	if err := h.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := h.Wait(ctx); !errHas(err, ErrCancelled) {
		t.Fatalf("second run Wait = %v, want ErrCancelled", err)
	}
}
