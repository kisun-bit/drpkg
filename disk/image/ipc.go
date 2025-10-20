//go:build linux

package image

import (
	"bytes"

	"github.com/lunixbochs/struc"
)

const (
	// shmSize 共享内存固定大小
	shmSize = 4 << 20
	// requestOffset 共享内存中的请求区域起始偏移
	requestOffset = 0
	// requestOffset 共享内存中的响应区域起始偏移
	responseOffset = 2 << 20
)

// rwMaxLen 读写IO的最大数据长度（不能超过 shmSize/2 ）
const rwMaxLen = 1 << 20

type requestType uint32

const (
	_READ requestType = iota + 1
	_WRITE
	_FLUSH
	_Close
)

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

func (req *readRequest) Bytes() []byte {
	var buf bytes.Buffer
	_ = struc.Pack(&buf, req)
	return buf.Bytes()
}

type readResponse struct {
	shmBaseResponse
	Length int32 `struc:"sizeof=Data"`
	Data   []byte
}

func loadReadResponse(data []byte) (r readResponse, err error) {
	err = struc.Unpack(bytes.NewReader(data), &r)
	return
}

//
// 写
//

type writeRequest struct {
	shmBaseRequest
	Offset int64
	Length int32 `struc:"sizeof=Data"`
	Data   []byte
}

func (req *writeRequest) Bytes() []byte {
	var buf bytes.Buffer
	_ = struc.Pack(&buf, req)
	return buf.Bytes()
}

type writeResponse struct {
	shmBaseResponse
	Length int32
}

func loadWriteResponse(data []byte) (r writeResponse, err error) {
	err = struc.Unpack(bytes.NewReader(data), &r)
	return
}

//
// 刷盘
//

type flushRequest struct {
	shmBaseRequest
}

func (req *flushRequest) Bytes() []byte {
	var buf bytes.Buffer
	_ = struc.Pack(&buf, req)
	return buf.Bytes()
}

type flushResponse struct {
	shmBaseResponse
}

func loadFlushResponse(data []byte) (r flushResponse, err error) {
	err = struc.Unpack(bytes.NewReader(data), &r)
	return
}

//
// 关闭
//

type closeRequest struct {
	shmBaseRequest
}

func (req *closeRequest) Bytes() []byte {
	var buf bytes.Buffer
	_ = struc.Pack(&buf, req)
	return buf.Bytes()
}
