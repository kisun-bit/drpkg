package transfer

import (
	"fmt"
	"unsafe"

	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"
)

type TransferStatus uint8

type IoHeader struct {
	DiskId      ioctl.DiskGuid // 磁盘ID
	Type        uint8          // IO类型  0 普通IO   1 分区表修改
	Consistency uint8          // 一致性标记  0 非一致性，即修复io   1 一致性io
	Offset      uint64         // IO相对磁盘的偏移
	Length      uint64         // 该条IO下，Buffer的长度
	Timestamp   uint64         // IO产生的时间戳    // FIXME: 新增字段，待Linux驱动确认
}

// 由于驱动采用1字节对齐，所以这里不能直接计算结构体大小
func GetIoHeaderRealSize() (size int64) {
	return int64(unsafe.Sizeof(uint32(0))*3 +
		uintptr(ioctl.
			MaxnameLen) +
		unsafe.Sizeof(uint8(0)) +
		unsafe.Sizeof(uint8(0)) +
		unsafe.Sizeof(uint64(0)) +
		unsafe.Sizeof(uint64(0)) +
		unsafe.Sizeof(uint64(0)))
}

// 通过 TransferRead 接口读取的IO
type Io struct {
	Header IoHeader // IO头
	Buffer []byte   // IO下的真实数据
}

func (i *Io) String() string {
	if i == nil {
		return "<ShmIo(nil)>"
	}
	return fmt.Sprintf("<ShmIo(consistent=%v,ts=%v,disk=%v,type=%v,off=%v,size=%v)>",
		i.Header.Consistency,
		i.Header.Timestamp,
		i.Header.DiskId,
		i.Header.Type,
		i.Header.Offset,
		i.Header.Length)
}

// 该结构为内部结构，为Transfer使用的共享内存的数据头部信息，保存了共享内存相关数据
type RingHead struct {
	RingSize uint64 // 整个共享内存大小
	Overflow int64  // 环形缓冲区是否已经溢出
	ReadPos  int64  // 当前读指针的位置 相对环的起点（0）
	WritePos int64  // 当前写指针的位置 相对环的起点（0）
}
