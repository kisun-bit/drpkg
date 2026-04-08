package extend

import (
	"context"
)

type CtxMutex struct {
	ch chan struct{}
}

// 初始化（类似 Mutex）
func New() *CtxMutex {
	m := &CtxMutex{
		ch: make(chan struct{}, 1),
	}
	m.ch <- struct{}{} // 初始是 unlocked
	return m
}

// Lock（支持 context）
func (m *CtxMutex) Lock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ch:
		return nil
	}
}

// Unlock
func (m *CtxMutex) Unlock() {
	select {
	case m.ch <- struct{}{}:
	default:
		panic("unlock of unlocked ctx-mutex")
	}
}
