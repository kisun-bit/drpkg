package util

import (
	"context"
	"io"
	"sync"
	"time"
)

type ProgressReporter interface {
	Report(progress string)
}

type DurationProgressWriter struct {
	cb      func(byteCount int, d time.Duration)
	mu      sync.Mutex
	count   int
	started time.Time
	writer  io.Writer
}

func (pw *DurationProgressWriter) Write(buf []byte) (int, error) {
	n, err := pw.writer.Write(buf)
	pw.mu.Lock()
	pw.count += n
	pw.mu.Unlock()
	return n, err
}

func NewDurationProgressWriter(
	ctx context.Context, cb func(byteCount int, d time.Duration),
	writer io.Writer, period time.Duration) (*DurationProgressWriter, func()) {
	self := &DurationProgressWriter{
		cb:      cb,
		started: GetTime().Now(),
		writer:  writer,
	}

	subCtx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()

		for {
			select {
			case <-subCtx.Done():
				return

			case <-time.After(period):
				self.mu.Lock()
				duration := GetTime().Now().Sub(self.started)
				cb(self.count, duration)
				self.mu.Unlock()
			}
		}
	}()

	return self, cancel
}
