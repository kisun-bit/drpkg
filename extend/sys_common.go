package extend

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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

// QueryMsVolumeTypeTable 查询Windows平台的卷类型表
func QueryMsVolumeTypeTable() (map[string]VolumeType, error) {
	script := "list volume"

	p := filepath.Join(ExecDir(), fmt.Sprintf("%d.volumetype.ds", time.Now().Unix()))
	if err := os.WriteFile(p, []byte(script), 0644); err != nil {
		return nil, err
	}
	defer os.Remove(p)

	cmdline := fmt.Sprintf("chcp 437 & diskpart /s %s", p)
	out, err := exec.Command("cmd.exe", "/c", cmdline).CombinedOutput()
	if err != nil {
		return nil, err
	}

	table_ := make(map[string]VolumeType)

	//
	// 输出示例：
	//  DISKPART> list volume
	//
	//  Volume ###  Ltr  Label        Fs     Type        Size     Status     Info
	//  ----------  ---  -----------  -----  ----------  -------  ---------  --------
	//  Volume 0         ???          NTFS   Spanned     1056 MB  Healthy
	//  Volume 1     E   ???          NTFS   Spanned     4121 MB  Healthy
	//  Volume 2                      RAW    Simple        30 MB  Healthy
	//  Volume 3     C                NTFS   Simple        39 GB  Healthy    Boot
	//  Volume 4         ????         NTFS   Simple       350 MB  Healthy    System
	//  Volume 5     L   ???          NTFS   Spanned     2847 MB  Healthy
	//  Volume 6     H   ???          NTFS   Mirror      2014 MB  Healthy
	//  Volume 7     G   ???          NTFS   Stripe      4028 MB  Healthy
	//  Volume 8     K   ???          NTFS   Simple       200 MB  Healthy
	//  Volume 9     J   RAID5?       NTFS   RAID-5        38 MB  Healthy
	//  Volume 10    D                       DVD-ROM         0 B  No Media
	//  Volume 11    F   ???          NTFS   Partition   2045 MB  Healthy
	//

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		lineItems := strings.Fields(line)
		if len(lineItems) < 3 || lineItems[0] != "volume" {
			continue
		}
		// 卷索引不存在
		if _, e := strconv.Atoi(lineItems[1]); e != nil {
			continue
		}
		// 卷标不存在
		if !isSingleUppercase(strings.ToUpper(lineItems[2])) {
			continue
		}
		volumeLtr := lineItems[2]

		switch {
		case strings.Contains(line, VolumeTypeMsStripe):
			table_[volumeLtr] = VolumeTypeMsStripe
		case strings.Contains(line, VolumeTypeMsMirror):
			table_[volumeLtr] = VolumeTypeMsMirror
		case strings.Contains(line, VolumeTypeMsRaid5):
			table_[volumeLtr] = VolumeTypeMsRaid5
		case strings.Contains(line, VolumeTypeMsSpanned):
			table_[volumeLtr] = VolumeTypeMsSpanned
		default:
			table_[volumeLtr] = VolumeTypeSimple
		}
	}

	return table_, nil
}

func isSingleUppercase(s string) bool {
	if len(s) != 1 {
		return false
	}
	ch := s[0]
	return ch >= 'A' && ch <= 'Z'
}

func GetSystemRoot() string {
	if runtime.GOOS == "windows" {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		return filepath.Clean(drive)
	} else {
		return "/"
	}
}
