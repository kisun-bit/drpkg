package journal

import "sync"

//var GlobalSequencer = NewSequenceGenerator(1)

type SequenceGenerator interface {
	// Get 获取时序号
	Get() uint64
	// Rollback 回滚时序号
	Rollback()
	// Freeze 冻结
	Freeze()
	// Unfreeze 解冻
	Unfreeze()
	// Reset 重置序号起始为0
	Reset()
}

type sequenceGenerator struct {
	mutex  sync.Mutex
	start  uint64
	seq    uint64
	freeze bool
}

func NewSequenceGenerator(start uint64) SequenceGenerator {
	s := &sequenceGenerator{start: start, seq: start}
	s.Reset()
	return s
}

// Get 获取时序号
func (g *sequenceGenerator) Get() uint64 {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if g.freeze {
		return g.seq
	}

	defer func() {
		g.seq += 1
	}()

	return g.seq
}

// Rollback 回滚时序号
func (g *sequenceGenerator) Rollback() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.seq -= 1
}

// Freeze 冻结时序号
func (g *sequenceGenerator) Freeze() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.freeze = true
}

// Unfreeze 解冻时序号
func (g *sequenceGenerator) Unfreeze() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.freeze = false
}

// Reset 重置时序号
func (g *sequenceGenerator) Reset() {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	g.seq = g.start
	g.freeze = false
}
