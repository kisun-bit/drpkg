package ioctl

import "github.com/pkg/errors"

var (
	ErrorOverflow             = errors.New("shared-memory overflowed")
	ErrorInsufficientMemSpace = errors.New("insufficient memory space")
)

//////////////////////////////////////
// 驱动错误码

const (
	// ringbuffer未正常工作 在设置一致性标记时，但共享缓存已经满了就会返回该错误码
	ERROR_RINGBUFFER_NOT_WORKING_FORBID_CLEAR_BIT = 140

	// 内存分配失败 任何接口都有可能返回该错误码
	ERR_NO_MEMORY = 102
)
