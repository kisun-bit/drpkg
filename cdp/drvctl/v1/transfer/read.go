package transfer

import (
	"bytes"
	"cdpctl/driver/ioctl"
	"cdpctl/utils"
	"encoding/binary"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

func TransferRead() (io []*Io, err error, status TransferStatus) {
	var ioObj *Io
	var dataSize uint64 = 0
	var currentReadPos int64
	var nextSize uint64 = 0

	if sharedMemAddr == nil {
		return nil, errors.New("sharedMemAddr is invalid"), TransferError
	}

	var ringHead *RingHead

	ringHead = (*RingHead)(unsafe.Pointer(sharedMemAddr))

	var readPos = atomic.LoadInt64(&ringHead.ReadPos)
	var writePos = atomic.LoadInt64(&ringHead.WritePos)
	var overFlow = atomic.LoadInt64(&ringHead.Overflow)

	if overFlow == 1 {
		return nil, nil, TransferOverflow
	}

	currentReadPos = readPos

	if nextReadPos > 0 {
		currentReadPos = nextReadPos
	}

	for {
		overFlow = atomic.LoadInt64(&ringHead.Overflow)

		if overFlow == 1 {
			return nil, nil, TransferOverflow
		}

		if currentReadPos == writePos {
			// 共享内存中没有数据了
			break
		}

		nextSize, err = getNextLength(ringHead, currentReadPos, writePos)

		if err != nil {
			return nil, err, TransferError
		}

		if dataSize+nextSize > MaxReadLen {
			// 已经达到代理设置的阈值
			if dataSize != 0 {
				break
			}
		}

		ioObj, err, currentReadPos = readOneIo(ringHead, currentReadPos, writePos)

		if err != nil {
			return nil, err, TransferError
		}

		io = append(io, ioObj)

		dataSize += ioObj.Header.Length
	}

	nextReadPos = currentReadPos

	if len(io) == 0 {
		return nil, nil, TransferNoData
	}

	return io, nil, TransferSuccess
}

func TransferReadConfirm(io []*Io) (err error) {
	if sharedMemAddr == nil {
		return errors.New("sharedMemAddr is invalid")
	}

	if nextReadPos == -1 {
		return errors.New("Unread data")
	}

	var ringHead *RingHead

	ringHead = (*RingHead)(unsafe.Pointer(sharedMemAddr))

	atomic.StoreInt64(&ringHead.ReadPos, nextReadPos)

	var req ioctl.DRVReqClearBitmap

	if len(io) == 0 {
		return nil
	}

	req.Guid = io[0].Header.DiskId
	req.IoType = 1
	// 清理位图的buf引用计数
	for _, ioObj := range io {
		if req.Guid != ioObj.Header.DiskId {
			// 发送清理位BUF引用计数
			err := ioctl.
				ReqDrvClearBitmap(&req)
			if err != nil {
				return errors.Wrapf(err, "ReqDrvClearBitmap")
			}
			req.Guid = ioObj.Header.DiskId
			req.ClearBitmapLen = 0
			req.ClearBitmap = nil
		}
		var segment ioctl.Segment
		segment.Start = ioObj.Header.Offset
		segment.Size = ioObj.Header.Length
		req.ClearBitmap = append(req.ClearBitmap, segment)
		req.ClearBitmapLen += 1
	}
	// 发送最后一个req
	err = ioctl.
		ReqDrvClearBitmap(&req)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvClearBitmap")
	}

	return nil
}

func getNextLength(head *RingHead, readPos, writePos int64) (uint64, error) {
	ringDataBuffer := uintptr(unsafe.Pointer(head)) + unsafe.Sizeof(*head)
	ringDataSize := head.RingSize - uint64(unsafe.Sizeof(*head))
	ioHeader := IoHeader{}
	ioHeadBuffer := make([]byte, unsafe.Sizeof(IoHeader{}))
	currentReadPos := readPos
	var err error

	// Read IO structure from the cache
	currentReadPos, err = readCache(ioHeadBuffer, GetIoHeaderRealSize(), currentReadPos, writePos, int64(ringDataSize), int64(ringDataBuffer))
	if err != nil {
		return 0, err
	}

	err = struc.UnpackWithOptions(bytes.NewBuffer(ioHeadBuffer), &ioHeader, &struc.Options{Order: binary.LittleEndian})
	if err != nil {
		return 0, err
	}

	return ioHeader.Length, nil
}

// ReadOneIo 读取一个IO操作
func readOneIo(head *RingHead, readPos, writePos int64) (*Io, error, int64) {
	ringDataBuffer := uintptr(unsafe.Pointer(head)) + unsafe.Sizeof(*head)
	ringDataSize := head.RingSize - uint64(unsafe.Sizeof(*head))
	io := Io{}
	ioHeader := IoHeader{}
	dataBuffer := make([]byte, 0)
	ioHeadBuffer := make([]byte, unsafe.Sizeof(IoHeader{}))
	currentReadPos := readPos
	var err error

	// Read IO structure from the cache
	currentReadPos, err = readCache(ioHeadBuffer, GetIoHeaderRealSize(), currentReadPos, writePos, int64(ringDataSize), int64(ringDataBuffer))
	if err != nil {
		return nil, errors.Errorf("read io header failed, err:%v", err), 0
	}

	err = struc.UnpackWithOptions(bytes.NewBuffer(ioHeadBuffer), &ioHeader, &struc.Options{Order: binary.LittleEndian})
	if err != nil {
		return nil, errors.Errorf("UnpackWithOptions io header failed, err:%v", err), 0
	}

	if runtime.GOOS == "windows" && ioHeader.Consistency == 1 {
		ioHeader.Timestamp = utils.ConvertWindowsTimeToUnixMicros(ioHeader.Timestamp)
	}

	io.Header = ioHeader
	dataBuffer = make([]byte, int64(io.Header.Length))

	// Read the actual data for this IO

	var readLen int64 = 0
	if io.Header.Type == 0 {
		readLen = int64(io.Header.Length)
	} else {
		readLen = 0
	}
	currentReadPos, err = readCache(dataBuffer, readLen, currentReadPos, writePos, int64(ringDataSize), int64(ringDataBuffer))
	if err != nil {
		return nil, errors.Errorf("read io data failed, err:%v", err), 0
	}

	// Update ALNum
	io.Buffer = dataBuffer

	return &io, nil, currentReadPos
}

func readCache(buffer []byte, size, readPos, writePos, ringDataSize, ringDataBuffer int64) (int64, error) {
	currentReadPtr := ringDataBuffer + readPos
	currentReadPos := readPos
	currentWritePos := writePos

	if size == 0 {
		return currentReadPos, nil
	}

	sys32intmax := int64((1 << 31) - 1)

	if sys32intmax < size {
		return 0, errors.Errorf("data block size too big, overflow!")
	}

	if readPos >= writePos {
		// Check if there's enough data
		if ringDataSize < size && currentWritePos == currentReadPos {
			return 0, errors.Errorf("Readable area is smaller than size -1")
		}

		if ringDataSize-currentReadPos+currentWritePos < size && currentReadPos > currentWritePos {
			return 0, errors.Errorf("Readable area is smaller than size -2")
		}

		if ringDataSize-readPos <= size {
			// Not enough space before the end of the buffer
			needRead := size
			hadRead := int64(0)

			// Copy the first part
			ptrValue := uintptr(currentReadPtr)
			unsafePtr := unsafe.Pointer(ptrValue)
			copy(buffer, unsafe.Slice((*byte)(unsafePtr), int(ringDataSize-readPos)))
			hadRead += ringDataSize - readPos
			currentReadPtr = ringDataBuffer
			currentReadPos = 0

			// Copy the remaining part
			if hadRead < needRead {
				ptrValue = uintptr(currentReadPtr)
				unsafePtr = unsafe.Pointer(ptrValue)
				copy(buffer[hadRead:], unsafe.Slice((*byte)(unsafePtr), int(needRead-hadRead)))
				currentReadPtr += needRead - hadRead
				currentReadPos += needRead - hadRead
			}

		} else {
			ptrValue := uintptr(currentReadPtr)
			unsafePtr := unsafe.Pointer(ptrValue)
			copy(buffer, unsafe.Slice((*byte)(unsafePtr), int(size)))
			currentReadPtr += size
			currentReadPos += size
		}
	} else {
		// Read available bytes: WritePos - ReadPos
		if writePos-readPos < size {
			return 0, errors.Errorf("Readable area is smaller than size :%d", size)
		}

		ptrValue := uintptr(currentReadPtr)
		unsafePtr := unsafe.Pointer(ptrValue)
		copy(buffer, unsafe.Slice((*byte)(unsafePtr), int(size)))
		currentReadPtr += size
		currentReadPos += size
	}

	return currentReadPos, nil
}
