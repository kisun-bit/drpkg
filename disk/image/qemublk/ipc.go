//go:build linux

package qemublk

import (
	"encoding/binary"

	"github.com/pkg/errors"
)

const (
	// shmSize 共享内存固定大小
	//
	// 性能优化说明：原先为 4MiB（请求区/响应区各 2MiB，单块读写上限 1MiB）。
	// 对于大文件顺序读写场景（如整盘备份/恢复），单块过小会导致一次读写被拆成
	// 大量分片，每个分片都要走一次完整的 eventfd 往返（Go 写 -> C 读 -> C 处理
	// -> C 写 -> Go 读），往返次数越多，固定的系统调用/上下文切换开销占比越高。
	// 现将共享内存扩大到 64MiB，单块上限提到 16MiB，往返次数降为原来的 1/16。
	// 注意：此常量必须与 imgio.c 中的 SHM_SIZE/RW_MAX_LEN 保持一致，修改后需要
	// 重新编译 imgio 可执行文件。
	shmSize = 64 << 20
	// requestOffset 共享内存中的请求区域起始偏移
	requestOffset = 0
	// requestOffset 共享内存中的响应区域起始偏移
	responseOffset = 32 << 20
)

// rwMaxLen 读写IO的最大数据长度（不能超过 shmSize/2 ，且需与C端 RW_MAX_LEN 一致）
const rwMaxLen = 16 << 20

var eventSignalBytes = []byte{1, 0, 0, 0, 0, 0, 0, 0}

type requestType uint32

const (
	_READ requestType = iota + 1
	_WRITE
	_FLUSH
	_Close
)

type shmRequest interface {
	buildRequest(shmData []byte) error
}

type shmBaseRequest struct {
	Type     requestType
	Sequence uint64
}

type shmBaseResponse struct {
	shmBaseRequest
	ErrorCode int32
}

//
// 读
//

type readRequest struct {
	shmBaseRequest
	Offset int64
	Length int32
}

func (req *readRequest) buildRequest(shmData []byte) error {
	if err := checkShm(shmData); err != nil {
		return err
	}
	// 结构：type(4) + sequence(8) + offset(8) + length(4) = 24字节
	binary.LittleEndian.PutUint32(shmData[requestOffset+0:], uint32(req.Type))
	binary.LittleEndian.PutUint64(shmData[requestOffset+4:], req.Sequence)
	binary.LittleEndian.PutUint64(shmData[requestOffset+12:], uint64(req.Offset))
	binary.LittleEndian.PutUint32(shmData[requestOffset+20:], uint32(req.Length))
	return nil
}

type readResponse struct {
	shmBaseResponse
	Length int32
	// DataRelStart 数据起始偏移，
	// C端数据紧跟在基础响应结构后：type(4) + sequence(8) + errorCode(4) + length(4) = 20字节
	DataRelStart int
	ResponseBody []byte
}

func loadReadResponse(shmData []byte) (r readResponse, err error) {
	if err = checkShm(shmData); err != nil {
		return r, err
	}
	respData := shmData[responseOffset:]
	// C端结构： type(4) + sequence(8) + errorCode(4) + length(4)
	r.Type = requestType(binary.LittleEndian.Uint32(respData[0:4]))
	r.Sequence = binary.LittleEndian.Uint64(respData[4:12])
	r.ErrorCode = int32(binary.LittleEndian.Uint32(respData[12:16]))
	r.Length = int32(binary.LittleEndian.Uint32(respData[16:20]))

	if r.ErrorCode != 0 {
		return r, errors.Errorf("loadReadResponse: %d", r.ErrorCode)
	}
	r.DataRelStart = 20
	r.ResponseBody = shmData[responseOffset:]
	return r, nil
}

//
// 写
//

type writeRequest struct {
	shmBaseRequest
	Offset int64
	Length int32
	Data   []byte
}

func (req *writeRequest) buildRequest(shmData []byte) error {
	if err := checkShm(shmData); err != nil {
		return err
	}
	// 结构：type(4) + sequence(8) + offset(8) + length(4) + data
	binary.LittleEndian.PutUint32(shmData[requestOffset+0:], uint32(req.Type))
	binary.LittleEndian.PutUint64(shmData[requestOffset+4:], req.Sequence)
	binary.LittleEndian.PutUint64(shmData[requestOffset+12:], uint64(req.Offset))
	binary.LittleEndian.PutUint32(shmData[requestOffset+20:], uint32(req.Length))
	copy(shmData[requestOffset+24:], req.Data)
	return nil
}

type writeResponse struct {
	shmBaseResponse
	Length int32
}

func loadWriteResponse(shmData []byte) (r writeResponse, err error) {
	if err = checkShm(shmData); err != nil {
		return r, err
	}
	respData := shmData[responseOffset:]
	// C端结构： type(4) + sequence(8) + errorCode(4) + length(4)
	r.Type = requestType(binary.LittleEndian.Uint32(respData[0:4]))
	r.Sequence = binary.LittleEndian.Uint64(respData[4:12])
	r.ErrorCode = int32(binary.LittleEndian.Uint32(respData[12:16]))
	r.Length = int32(binary.LittleEndian.Uint32(respData[16:20]))

	if r.ErrorCode != 0 {
		return r, errors.Errorf("loadWriteResponse: %d", r.ErrorCode)
	}
	return r, nil
}

//
// 刷盘
//

type flushRequest struct {
	shmBaseRequest
}

func (req *flushRequest) buildRequest(shmData []byte) error {
	if err := checkShm(shmData); err != nil {
		return err
	}
	// 结构：type(4) + sequence(8) = 12字节
	binary.LittleEndian.PutUint32(shmData[requestOffset+0:], uint32(req.Type))
	binary.LittleEndian.PutUint64(shmData[requestOffset+4:], req.Sequence)
	return nil
}

type flushResponse struct {
	shmBaseResponse
}

func loadFlushResponse(shmData []byte) (r flushResponse, err error) {
	if err = checkShm(shmData); err != nil {
		return r, err
	}
	respData := shmData[responseOffset:]
	// C端结构： type(4) + sequence(8) + errorCode(4)
	r.Type = requestType(binary.LittleEndian.Uint32(respData[0:4]))
	r.Sequence = binary.LittleEndian.Uint64(respData[4:12])
	r.ErrorCode = int32(binary.LittleEndian.Uint32(respData[12:16]))

	if r.ErrorCode != 0 {
		return r, errors.Errorf("loadFlushResponse: %d", r.ErrorCode)
	}
	return r, nil
}

//
// 关闭
//

type closeRequest struct {
	shmBaseRequest
}

func (req *closeRequest) buildRequest(shmData []byte) error {
	if err := checkShm(shmData); err != nil {
		return err
	}
	// 结构：type(4) + sequence(8) = 12字节
	binary.LittleEndian.PutUint32(shmData[requestOffset+0:], uint32(req.Type))
	binary.LittleEndian.PutUint64(shmData[requestOffset+4:], req.Sequence)
	return nil
}

func checkShm(shmData []byte) error {
	if len(shmData) == 0 {
		return errors.New("shm has already been destroyed")
	}
	return nil
}
