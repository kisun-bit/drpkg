package others

import "sync"

type GlobalErrorMgr struct {
	mutex sync.RWMutex
	err   error
}

func NewGlobalError() *GlobalErrorMgr {
	return &GlobalErrorMgr{}
}

func (ge *GlobalErrorMgr) Get() error {
	ge.mutex.RLock()
	defer ge.mutex.RUnlock()
	return ge.err
}

func (ge *GlobalErrorMgr) Set(err error) {
	if err == nil {
		return
	}
	if ge.ErrorOccurred() {
		return
	}
	ge.ForceSet(err)
}

func (ge *GlobalErrorMgr) ForceSet(err error) {
	ge.mutex.Lock()
	defer ge.mutex.Unlock()
	ge.err = err
}

func (ge *GlobalErrorMgr) ErrorOccurred() bool {
	return ge.Get() != nil
}
