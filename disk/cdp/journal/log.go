package journal

import (
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/disk/image/hkc"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"github.com/lunixbochs/struc"
)

// CdpRecord 表示一条CDP日志记录，对应一次系统写IO。
type CdpRecord struct {
	Header IOHeader
	Data   IOContent
}

// IOContent 表示IO的数据内容。
type IOContent = hkc.Cluster

// IOHeader 表示 CDP 日志记录的头部信息。
type IOHeader struct {
	// Mask 表示 IO 的类型及特征掩码。
	Mask proto.IOMask

	// DiskGuidLen 表示 DiskGuid 的字节长度。
	DiskGuidLen uint32 `struc:"sizeof=DiskGuid"`

	// DiskGuid 表示产生该 IO 的磁盘 GUID。
	DiskGuid string

	// Timestamp 表示 IO 发生的时间戳，单位为微秒。
	Timestamp uint64

	// Sequence 表示 IO 在 CDP 日志中的全局递增序号。
	//
	// 同一 CDP 运行周期内，Sequence 必须严格递增。
	// CDP 系统或备份服务重启后，应从 CDP 介质恢复上次的 Sequence，
	// 再继续递增，以保证日志序号的连续性。
	Sequence uint64
}

func MaskString(mask proto.IOMask) string {
	switch mask {
	case proto.IOMask_FLAG_OPEN_FULL:
		return "FLAG_OPEN_FULL"
	case proto.IOMask_FLAG_CLOSE_FULL:
		return "FLAG_CLOSE_FULL"
	case proto.IOMask_FLAG_OPEN_BITMAP:
		return "FLAG_OPEN_BITMAP"
	case proto.IOMask_FLAG_CLOSE_BITMAP:
		return "FLAG_CLOSE_BITMAP"
	case proto.IOMask_FLAG_OPEN_REPAIR:
		return "FLAG_OPEN_REPAIR"
	case proto.IOMask_FLAG_CLOSE_REPAIR:
		return "FLAG_CLOSE_REPAIR"
	case proto.IOMask_DATA_FULL:
		return "DATA_FULL"
	case proto.IOMask_DATA_BITMAP:
		return "DATA_BITMAP"
	case proto.IOMask_DATA_REPAIR:
		return "DATA_REPAIR"
	case proto.IOMask_DATA_REALTIME:
		return "DATA_REALTIME"
	default:
		return "UNKNOWN"
	}
}

// IsFlagRecordByMask 是否是标记IO
func IsFlagRecordByMask(mask proto.IOMask) bool {
	switch mask {
	case proto.IOMask_FLAG_OPEN_FULL,
		proto.IOMask_FLAG_CLOSE_FULL,
		proto.IOMask_FLAG_OPEN_BITMAP,
		proto.IOMask_FLAG_CLOSE_BITMAP,
		proto.IOMask_FLAG_OPEN_REPAIR,
		proto.IOMask_FLAG_CLOSE_REPAIR:
		return true
	}
	return false
}

// IsDataRecordByMask 是否是数据IO
func IsDataRecordByMask(mask proto.IOMask) bool {
	switch mask {
	case proto.IOMask_DATA_FULL,
		proto.IOMask_DATA_BITMAP,
		proto.IOMask_DATA_REPAIR,
		proto.IOMask_DATA_REALTIME:
		return true
	}
	return false
}

func (h *IOHeader) BinaryStructSize() uint64 {
	return 24 + uint64(h.DiskGuidLen)
}

func (r *CdpRecord) String() string {
	return fmt.Sprintf("RECORD[%s]SEQ%d\\TS%d\\DEV%s<%s>",
		MaskString(r.Header.Mask),
		r.Header.Sequence,
		r.Header.Timestamp,
		r.Header.DiskGuid,
		r.Data,
	)
}

// Pack 日志打包
func (r *CdpRecord) Pack(w io.Writer) error {
	return struc.Pack(w, r)
}

// HeaderPack 日志头部打包
func (r *CdpRecord) HeaderPack(w io.Writer) error {
	return struc.Pack(w, &r.Header)
}

// UnpackCdpRecord 日志解包
func UnpackCdpRecord(reader io.Reader) (r *CdpRecord, err error) {
	r = new(CdpRecord)
	err = struc.Unpack(reader, r)
	return
}
