package meta

import (
	"os"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

const HeaderDiskSize = 1048576

//
// ------------------------------------------------------
// |                    头部区域
// ------------------------------------------------------
// 00-16：签名。固定为'BIOTRKMETA      '。
// 16-20：状态。
// 20-24：错误码。
// 24-32：Linux使用，若不为0表示主机未正常关闭。
// 32-40：Linux适用，若为1。表示启动预处理阶段拷贝数据的模式
// 40-48：预留3。
// 48-56：预留4。
// 56-64：预留5。
// 64-72：预留6。
// 72-76：最大受保护磁盘数量。可取16、32、64、128之一，每一个对应一个磁盘位图。
// 76-84：最大受保护磁盘大小。可取64TB(64<<40)、512TB(512<<40)之一。
// 84-88：磁盘位图的位数量。依据"最大受保护磁盘大小"而变化。
//        64TB 对应2MB
//        512TB对应16MB
// 88-92：磁盘位图的每个位对应的字节数。固定为4MB。
// 92-100：首个磁盘位图的起始字节偏移。固定为1048576。
// 100-1048476：预留区域。
// ------------------------------------------------------
// |                    位图区域
// ------------------------------------------------------
// 1048576-3145728：磁盘0的位图数据区域。
// 3145728-5242880：磁盘1的位图数据区域。
// ......
// 133169152-135266304：磁盘63的位图数据区域。
// ------------------------------------------------------
//

type Header struct {
	Signature            [16]byte `struc:"little"`
	State                uint32   `struc:"little"`
	ErrorCode            uint32   `struc:"little"`
	LinuxNoShutdown      uint32   `struc:"little"`
	LinuxWorkMode        uint32   `struc:"little"`
	Reversed1            uint64   `struc:"little"`
	Reversed2            uint64   `struc:"little"`
	Reversed3            uint64   `struc:"little"`
	Reversed4            uint64   `struc:"little"`
	Reversed5            uint64   `struc:"little"`
	MaxDiskCount         uint32   `struc:"little"`
	MaxDiskSize          uint64   `struc:"little"`
	BitsPerDiskBitmap    uint32   `struc:"little"`
	BytesPerBit          uint32   `struc:"little"`
	FirstDiskBitmapStart uint64   `struc:"little"`
}

func GenerateHeader(maxDiskCount uint32, maxDiskSize uint64) (h Header, err error) {
	copy(h.Signature[:], signature)
	h.MaxDiskCount = maxDiskCount
	h.MaxDiskSize = maxDiskSize
	h.BitsPerDiskBitmap = uint32(maxDiskSize / uint64(defaultBytesPerBit))
	h.BytesPerBit = defaultBytesPerBit
	h.FirstDiskBitmapStart = DefaultFirstDiskBitmapStart
	return h, checkHeaderFields(h)
}

func loadHeader(f *os.File) (h Header, err error) {
	if err = struc.Unpack(f, &h); err != nil {
		return h, err
	}
	if err = checkHeaderFields(h); err != nil {
		return h, err
	}
	stat, err := f.Stat()
	if err != nil {
		return h, err
	}
	expectSize := int64(h.BitsPerDiskBitmap/8)*int64(h.MaxDiskCount) + DefaultFirstDiskBitmapStart
	if stat.Size() != expectSize {
		return h, errors.Errorf("file size mismatch")
	}
	return h, nil
}

func checkHeaderFields(h Header) error {
	if string(h.Signature[:]) != signature {
		return errors.Errorf("signature mismatch")
	}
	if !funk.InUInt32s(effectMaxDiskCountArr, h.MaxDiskCount) {
		return errors.Errorf("MaxDiskCount only supports one of %v, current count is %v", effectMaxDiskCountArr, h.MaxDiskCount)
	}
	if !funk.InUInt64s(effectMaxDiskSizeArr, h.MaxDiskSize) {
		return errors.Errorf("maxDiskSize only supports one of %v, current size is %v", effectMaxDiskSizeArr, h.MaxDiskSize)
	}
	return nil
}
