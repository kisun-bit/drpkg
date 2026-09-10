package ioctl

import (
	"bytes"
	"encoding/binary"
	"sync/atomic"
	"unsafe"

	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

// ===========================================================================
// 共享内存类型
// ===========================================================================

// ShmConfig 表示创建共享缓存的请求与响应。
type ShmConfig struct {
	// Size 为请求创建的共享缓存大小（字节）（请求参数）。
	Size uint32

	// EventHandle 为共享缓存的读写事件对象句柄（响应参数）。
	EventHandle uint64

	// Address 为共享缓存的映射内存地址（响应参数）。
	Address uint64
}

// TransferStatus 表示共享内存读取操作的返回状态。
type TransferStatus uint8

const (
	// TransferSuccess 表示成功读取到 IO 记录。
	TransferSuccess TransferStatus = iota

	// TransferError 表示读取过程中发生错误。
	TransferError

	// TransferNoData 表示当前没有可用的 IO 记录。
	TransferNoData

	// TransferOverflow 表示共享内存已溢出，驱动已切换到位图模式。
	TransferOverflow
)

// IoHeader 表示共享内存中每条 IO 记录的头部信息。
//
// 二进制布局（packed, LittleEndian）：
//
//	DiskID      biotrkmeta.DiskID  (520 bytes)
//	Type        uint8              (  1 byte)   0=普通IO, 1=分区表修改
//	Consistency uint8              (  1 byte)   0=修复IO, 1=一致性IO
//	Offset      uint64             (  8 bytes)
//	Length      uint64             (  8 bytes)
//	Timestamp   uint64             (  8 bytes)
//
// 总大小：546 字节。
type IoHeader struct {
	DiskID      biotrkmeta.DiskID
	Type        uint8
	Consistency uint8
	Offset      uint64
	Length      uint64
	Timestamp   uint64
}

// IoHeaderWireSize 是 IoHeader 在共享内存中的二进制大小。
const IoHeaderWireSize = biotrkmeta.DiskIDBinSize + 1 + 1 + 8 + 8 + 8 // 546

// RingHeader 是环形缓冲区头部，保存共享内存的元数据。
//
// 位于共享内存起始位置，由驱动维护。
type RingHeader struct {
	RingSize uint64 // 整个环形缓冲区大小（字节）
	Overflow int64  // 溢出标记：1 表示已溢出
	ReadPos  int64  // 当前读指针位置（相对环数据区起点）
	WritePos int64  // 当前写指针位置（相对环数据区起点）
}

// RingHeaderWireSize 是 RingHeader 的二进制大小。
const RingHeaderWireSize = 8 + 8 + 8 + 8 // 32

// IoRecord 表示从共享内存中读取的一条 IO 记录。
type IoRecord struct {
	Header IoHeader // IO 记录头部
	Data   []byte   // IO 数据（Length 字节）
}

// ShmRing 封装共享内存环形缓冲区的访问。
//
// 使用 NewShmRing 创建实例，通过 Read 读取 IO 记录，
// ConfirmRead 确认读取并清理位图引用，Close 销毁。
//
// 所有字段不直接暴露，通过方法访问。
type ShmRing struct {
	ringHeader  *RingHeader // 指向共享内存中的环形缓冲区头部
	ringData    *byte       // 指向环形缓冲区数据区起点
	ringSize    uint64      // 环形缓冲区总大小
	dataSize    uint64      // 数据区大小（ringSize - sizeof(RingHeader)）
	nextReadPos int64       // 下次读取的起始位置（-1 表示从头开始）
	maxReadLen  uint64      // 单次读取最大字节数
	platform    platformState
}

// MaxReadLen 返回单次读取的最大字节数设置。
func (r *ShmRing) MaxReadLen() uint64 {
	return r.maxReadLen
}

// ===========================================================================
// 环形缓冲区读取
// ===========================================================================

// Read 从共享内存环形缓冲区中读取 IO 记录。
//
// 返回值：
//   - records: 读取到的 IO 记录切片（TransferSuccess 时非空）。
//   - status: 读取状态。
//   - err: 仅在 status == TransferError 时非 nil。
func (r *ShmRing) Read() ([]*IoRecord, TransferStatus, error) {
	if r.ringHeader == nil {
		return nil, TransferError, errors.New("ShmRing not initialized")
	}

	header := r.ringHeader
	writePos := atomic.LoadInt64(&header.WritePos)
	readPos := atomic.LoadInt64(&header.ReadPos)

	if atomic.LoadInt64(&header.Overflow) == 1 {
		return nil, TransferOverflow, nil
	}

	currentReadPos := readPos
	if r.nextReadPos > 0 {
		currentReadPos = r.nextReadPos
	}

	var records []*IoRecord
	var totalDataSize uint64

	for {
		if atomic.LoadInt64(&header.Overflow) == 1 {
			return nil, TransferOverflow, nil
		}

		if currentReadPos == writePos {
			break
		}

		nextLen, err := r.peekNextLength(currentReadPos, writePos)
		if err != nil {
			return nil, TransferError, err
		}

		if totalDataSize+nextLen > r.maxReadLen && totalDataSize > 0 {
			break
		}

		record, newPos, err := r.readOneIo(currentReadPos, writePos)
		if err != nil {
			return nil, TransferError, err
		}
		currentReadPos = newPos

		records = append(records, record)
		totalDataSize += record.Header.Length
	}

	r.nextReadPos = currentReadPos

	if len(records) == 0 {
		return nil, TransferNoData, nil
	}

	return records, TransferSuccess, nil
}

// ConfirmRead 确认 IO 记录已处理完毕。
//
// 更新环形缓冲区读指针，并按设备分组清理位图引用。
// 调用 Read 返回非空记录后，处理完毕必须调用此方法。
func (r *ShmRing) ConfirmRead(records []*IoRecord) error {
	if r.ringHeader == nil {
		return errors.New("ShmRing not initialized")
	}

	if r.nextReadPos == -1 {
		return errors.New("no unconfirmed read data")
	}

	// 更新读指针
	atomic.StoreInt64(&r.ringHeader.ReadPos, r.nextReadPos)

	if len(records) == 0 {
		return nil
	}

	// 按设备分组，批量清理位图引用
	return r.clearBitmapByRecords(records)
}

// clearBitmapByRecords 按 DiskID 分组，批量发送清理位图引用请求。
func (r *ShmRing) clearBitmapByRecords(records []*IoRecord) error {
	var req *ClearBitmapReferenceRequest

	flush := func() error {
		if req == nil || len(req.Segments) == 0 {
			return nil
		}
		req.SegmentsLen = uint32(len(req.Segments))
		return ClearBitmapReference(req)
	}

	for _, rec := range records {
		seg := biotrkmeta.Segment{
			Start: rec.Header.Offset,
			Size:  rec.Header.Length,
		}

		if req == nil {
			req = &ClearBitmapReferenceRequest{
				RecordType: biotrkmeta.RecordFromSharedMemory,
				DiskID:     rec.Header.DiskID,
				Segments:   []biotrkmeta.Segment{seg},
			}
			continue
		}

		// DiskID 变化时，先发送上一组
		if !diskIDEqual(req.DiskID, rec.Header.DiskID) {
			if err := flush(); err != nil {
				return errors.Wrapf(err, "ClearBitmapReference")
			}
			req = &ClearBitmapReferenceRequest{
				RecordType: biotrkmeta.RecordFromSharedMemory,
				DiskID:     rec.Header.DiskID,
				Segments:   []biotrkmeta.Segment{seg},
			}
		} else {
			req.Segments = append(req.Segments, seg)
		}
	}

	return flush()
}

// diskIDEqual 比较两个 DiskID 是否相等。
func diskIDEqual(a, b biotrkmeta.DiskID) bool {
	return bytes.Equal(a.ID[:], b.ID[:])
}

// ===========================================================================
// 环形缓冲区内部读取逻辑
// ===========================================================================

// peekNextLength 查看下一条 IO 记录的数据长度，不推进读指针。
func (r *ShmRing) peekNextLength(readPos, writePos int64) (uint64, error) {
	buf := make([]byte, IoHeaderWireSize)
	if _, err := r.readCache(buf, int64(IoHeaderWireSize), readPos, writePos); err != nil {
		return 0, err
	}

	var header IoHeader
	if err := struc.UnpackWithOptions(bytes.NewReader(buf), &header, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return 0, errors.Wrapf(err, "unpack peek header")
	}

	return header.Length, nil
}

// readOneIo 从环形缓冲区读取一条完整的 IO 记录。
//
// 返回读取到的记录和新的读指针位置。
func (r *ShmRing) readOneIo(readPos, writePos int64) (*IoRecord, int64, error) {
	// 读取 IO 头部
	headerBuf := make([]byte, IoHeaderWireSize)
	currentPos, err := r.readCache(headerBuf, int64(IoHeaderWireSize), readPos, writePos)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "read io header")
	}

	var header IoHeader
	if err := struc.UnpackWithOptions(bytes.NewReader(headerBuf), &header, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, 0, errors.Wrapf(err, "unpack io header")
	}

	normalizeIoTimestamp(&header)

	// 普通 IO（Type == 0）需要读取数据
	var data []byte
	if header.Type == 0 && header.Length > 0 {
		data = make([]byte, header.Length)
		currentPos, err = r.readCache(data, int64(header.Length), currentPos, writePos)
		if err != nil {
			return nil, 0, errors.Wrapf(err, "read io data")
		}
	}

	return &IoRecord{Header: header, Data: data}, currentPos, nil
}

// readCache 从环形缓冲区读取指定长度的数据。
//
// dataLen 为要读取的字节数。返回新的读指针位置。
// 该函数处理环形缓冲区的回绕逻辑。
func (r *ShmRing) readCache(buf []byte, dataLen, readPos, writePos int64) (int64, error) {
	if dataLen == 0 {
		return readPos, nil
	}

	currentReadPos := readPos
	dataSizeI64 := int64(r.dataSize)

	if readPos >= writePos {
		// 读指针在写指针之后或相等：需要检查数据是否回绕

		if dataSizeI64-readPos+writePos < dataLen && currentReadPos > writePos {
			return 0, errors.Errorf("readable area smaller than %d bytes", dataLen)
		}

		if dataSizeI64-readPos <= dataLen {
			// 需要回绕读取
			hadRead := int64(0)
			firstPart := dataSizeI64 - readPos
			copy(buf, unsafe.Slice(r.ringOffset(currentReadPos), int(firstPart)))
			hadRead += firstPart
			currentReadPos = 0

			if hadRead < dataLen {
				remaining := dataLen - hadRead
				copy(buf[hadRead:], unsafe.Slice(r.ringOffset(currentReadPos), int(remaining)))
				currentReadPos += remaining
			}
		} else {
			copy(buf, unsafe.Slice(r.ringOffset(currentReadPos), int(dataLen)))
			currentReadPos += dataLen
		}
	} else {
		// 读指针在写指针之前：正常读取
		available := writePos - readPos
		if available < dataLen {
			return 0, errors.Errorf("readable area (%d) smaller than %d bytes", available, dataLen)
		}
		copy(buf, unsafe.Slice(r.ringOffset(currentReadPos), int(dataLen)))
		currentReadPos += dataLen
	}

	return currentReadPos, nil
}

// ringOffset 返回从数据区起点偏移 off 处的字节指针。
func (r *ShmRing) ringOffset(off int64) *byte {
	return (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(r.ringHeader)) + unsafe.Sizeof(RingHeader{}) + uintptr(off)))
}

// ===========================================================================
// IoRecord 辅助方法
// ===========================================================================

// SortByDiskID 按 DiskID 对 IO 记录分组排序，将同一设备的记录连续排列。
// 原地修改切片，保持同一设备内的原始顺序。
func SortByDiskID(records []*IoRecord) {
	if len(records) <= 1 {
		return
	}

	// 简单排序：使用稳定排序保持同一设备内顺序
	for i := 1; i < len(records); i++ {
		j := i
		for j > 0 {
			prev := j - 1
			if compareDiskID(records[prev].Header.DiskID, records[j].Header.DiskID) <= 0 {
				break
			}
			records[prev], records[j] = records[j], records[prev]
			j--
		}
	}
}

func compareDiskID(a, b biotrkmeta.DiskID) int {
	return bytes.Compare(a.ID[:], b.ID[:])
}

// ===========================================================================
// IoHeader 序列化辅助
// ===========================================================================

// PackIoHeader 将 IoHeader 按 packed LittleEndian 编码为二进制。
func PackIoHeader(h *IoHeader) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := struc.PackWithOptions(buf, h, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, errors.Wrapf(err, "pack IoHeader")
	}
	return buf.Bytes(), nil
}

// UnpackIoHeader 将 packed LittleEndian 二进制解码为 IoHeader。
func UnpackIoHeader(data []byte) (*IoHeader, error) {
	var h IoHeader
	if err := struc.UnpackWithOptions(bytes.NewReader(data), &h, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, errors.Wrapf(err, "unpack IoHeader")
	}
	return &h, nil
}
