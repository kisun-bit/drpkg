package extend

import (
	"bytes"
	"io"
	"os"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

type PVLabel struct {
	ID       [8]byte  // 标签的标识符. 必须是"LABELONE"
	Sector   uint64   // 当前标签所处的扇区编号.
	CRC      uint32   // 从 Offset 开始到扇区结尾的数据的CRC校验值.
	Offset   uint32   // 标签正文起始位置偏移（从标签开始位置，以字节为单位进行计算，一般是32，也就是是label_header的大小）
	Typename [8]byte  // 标签类型，一般都是“LVM2 001”
	UUID     [32]byte // PV的UUID
}

func ScanPVLabelFromDisk(dev string) (pl PVLabel, existed bool, err error) {
	lba, err := BytesPerSector(dev)
	if err != nil {
		return PVLabel{}, false, err
	}

	fp, err := os.Open(dev)
	if err != nil {
		return PVLabel{}, false, err
	}
	defer fp.Close()

	const labelScanSectors = 4
	buf := make([]byte, lba)

	for i := 0; i < labelScanSectors; i++ {
		curOff := int64(i) * int64(lba)

		if _, err = fp.Seek(curOff, io.SeekStart); err != nil {
			return PVLabel{}, false, errors.Errorf("seek sector %d: %w", i, err)
		}

		n, er := io.ReadFull(fp, buf)
		if er != nil && er != io.EOF {
			return PVLabel{}, false, errors.Errorf("read sector %d: %w", i, er)
		}
		if n == 0 {
			break
		}

		labelReader := bytes.NewReader(buf[:n])
		if err = struc.Unpack(labelReader, &pl); err != nil {
			continue
		}

		if bytes.Equal(pl.ID[:], []byte("LABELONE")) {
			return pl, true, nil
		}
	}

	return PVLabel{}, false, nil
}

// CopyFileByDiskExtents 基于文件的磁盘分布拷贝数据
// 注意：某一些稀疏文件由于存在空洞，会导致原文件和拷贝后文件存在数据差异。
// 因为被视为空洞的区域会被文件以0填充，但是空洞区域在磁盘上可能存在脏数据
func CopyFileByDiskExtents(file string, dst io.Writer) (int64, error) {
	es, err := FileDiskExtents(file)
	if err != nil {
		return 0, err
	}

	buf := make([]byte, 4<<10)
	size := int64(0)

	for _, de := range es {
		df, eopen := os.OpenFile(de.Disk, R_DSYNC_MODE, 0666)
		if eopen != nil {
			return 0, eopen
		}

		remain := de.Size
		start := de.Start
		for {
			if remain <= 0 {
				_ = df.Close()
				break
			}
			nr, er := df.ReadAt(buf, start)
			if er != nil {
				_ = df.Close()
				return 0, errors.Wrapf(er, "failed to read extent from %s", de.Disk)
			}
			wLen := nr
			if int64(nr) > remain {
				wLen = int(remain)
			}
			nw, ew := dst.Write(buf[:wLen])
			if ew != nil {
				_ = df.Close()
				return 0, errors.Wrap(ew, "failed to write extent to writer")
			}
			size += int64(nw)
			remain -= int64(nr)
			start += int64(nr)
		}
	}

	return size, nil
}
