package image

type requestType uint32

const (
	_READ requestType = iota + 1
	_WRITE
	_FLUSH
)

type shmBase struct {
	Type     requestType
	Sequence uint64
}

//
// 读
//

type readRequest struct {
	shmBase
	Offset int64
	Length int
}

type readResponse struct {
	shmBase
	Length    int `struc:"sizeof=Data"`
	Data      []byte
	ErrorCode int
}

//
// 写
//

type writeRequest struct {
	shmBase
	Offset int64
	Length int
	Data   []byte
}

type writeResponse struct {
	shmBase
	Length    int
	ErrorCode int
}

//
// 刷盘
//

type flushRequest struct {
	shmBase
}

type flushResponse struct {
	shmBase
	ErrorCode int
}
