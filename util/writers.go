package util

import (
	"io"
	"sync"
)

type TeeWriter struct {
	mu      sync.Mutex
	writers []io.Writer
	count   int
}

func (self *TeeWriter) Count() int {
	self.mu.Lock()
	defer self.mu.Unlock()

	return self.count
}

func (self *TeeWriter) Write(p []byte) (n int, err error) {
	self.mu.Lock()
	defer self.mu.Unlock()

	self.count += len(p)

	for _, writer := range self.writers {
		n, err = writer.Write(p)
		if err != nil && err != io.EOF {
			return n, err
		}
	}

	return len(p), nil
}

// NewTee creates a writer that duplicates its writes to all the
// provided writers, similar to the Unix tee(1) command.
func NewTee(writers ...io.Writer) *TeeWriter {
	return &TeeWriter{
		writers: writers,
	}
}

type NopWriteCloser struct {
	io.Writer
}

func (self NopWriteCloser) Close() error {
	return nil
}
