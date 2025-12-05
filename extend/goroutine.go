package extend

import "sync"

// TryOnceManager 是一个“并发重试管理器”。
// 行为特性：
//  1. 多个 goroutine 并发调用 Do() 时，只有一个 goroutine 会实际执行 job()。
//  2. 其他 goroutine 会等待执行结果。
//  3. 如果 job() 执行成功：
//     - 当前执行者返回成功
//     - 所有等待的 goroutine 醒来并直接返回成功，不会调用 job()
//     - 之后再次调用 Do() 也会直接成功返回（除非外部重新设置为需要重试）
//  4. 如果 job() 执行失败：
//     - 当前执行者返回错误
//     - 所有等待的 goroutine 被唤醒后会继续争抢执行 job()
//     - 直到第一次成功为止
//
// 使用场景（例如）：
//
//	多个协程同时进行网络上传，若网络断开，只需其中一个协程负责重试连接，
//	当连接恢复后，其他协程无需再次执行重试流程，也无需阻塞过久。
type TryOnceManager struct {
	mutex sync.Mutex
	cond  *sync.Cond

	statRunning bool // 是否有线程正在执行 job
	statNeedRun bool // 是否仍需执行（失败 -> true，成功 -> false）
}

func NewTryOnceManager() *TryOnceManager {
	oi := &TryOnceManager{}
	oi.cond = sync.NewCond(&oi.mutex)
	return oi
}

func (tom *TryOnceManager) Do(params any, job func(params any) error) error {
	tom.mutex.Lock()

	// 每次 Do() 表示需要一次执行
	tom.statNeedRun = true

	// 如果已成功过，直接返回
	if !tom.statNeedRun {
		tom.mutex.Unlock()
		return nil
	}

	// 有协程正在执行，则等待执行结果
	for tom.statRunning {
		tom.cond.Wait()

		// 被唤醒后检查是否仍需要执行
		if !tom.statNeedRun {
			tom.mutex.Unlock()
			return nil
		}
	}

	// 我成为执行者
	tom.statRunning = true
	tom.mutex.Unlock()

	// ===== 实际执行 job() =====
	err := job(params)
	// ===========================

	tom.mutex.Lock()

	tom.statRunning = false
	tom.statNeedRun = err != nil

	tom.cond.Broadcast()
	tom.mutex.Unlock()

	return err
}
