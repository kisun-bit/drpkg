package transfer

import "unsafe"

const (
	// EventWaitTimeout 等待事件的超时时间
	EventWaitTimeout = 2000 // Milliseconds
)

// 保存原始的内存映射指针
var mapAddr unsafe.Pointer = nil

// 驱动发送的共享内存的起始地址
var sharedMemAddr *byte = nil

// 保存当前读取指针应该设置的值
var nextReadPos int64 = -1

// 保存最大读取多少字节
var MaxReadLen uint64 = 4 * 1024

// 保存代理设置的共享缓存大小，只有在linux的mmap时用得到
var ShareMemSize uintptr = 0

const (
	// TransferSuccess 读取数据时，transfer回复的成功
	TransferSuccess TransferStatus = iota

	// TransferError 读取数据时，transfer回复的错误，这时就需要检查error
	TransferError

	// TransferNoData 读取数据时，transfer回复的没有数据，就需要调用 TransferReadWait 进入等待
	TransferNoData

	// TransferOverflow 读取数据时，transfer回复的数据满了，这时驱动就会开启位图，进入暂停实时状态，就不需要读取数据了
	TransferOverflow
)
