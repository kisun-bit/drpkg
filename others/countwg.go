package others

import (
	"sync"
	"sync/atomic"
)

type WaitGroup struct {
	wg      sync.WaitGroup
	counter atomic.Int32
}

func (w *WaitGroup) Add(delta int) {
	w.counter.Add(int32(delta))
	w.wg.Add(delta)
}

func (w *WaitGroup) Done() {
	w.counter.Add(-1)
	w.wg.Done()
}

func (w *WaitGroup) Wait() {
	w.wg.Wait()
}

func (w *WaitGroup) WillBlock() bool {
	return w.counter.Load() > 0
}
