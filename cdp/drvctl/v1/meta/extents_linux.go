package meta

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

//
// 见：https://www.kernel.org/doc/Documentation/filesystems/fiemap.txt
//

const (
	IOCTL_FIEMAP = 0xc020660b

	sizeOfFiemapStruc       = 32
	sizeOfFiemapExtentStruc = 56

	FILEMAP_FLAG_SYNC  = 0x0001
	FIEMAP_EXTENT_LAST = 0x0001
)

type fiemap struct {
	start         uint64
	length        uint64
	flags         uint32
	mappedExtents uint32
	extentCount   uint32
}

type fiemapExtent struct {
	Logical   uint64 // fe_logical
	Physical  uint64 // fe_physical
	Length    uint64 // fe_length
	reserved1 uint64
	reserved2 uint64
	Flags     uint32 // fe_flags, FIEMAP_EXTENT_* flags for this extent
}

func ioctlFileMap(file *os.File, start uint64, length uint64) ([]fiemapExtent, bool, error) {
	if length == 0 {
		return nil, true, nil
	}

	extentCount := uint32(50)
	buf := make([]byte, sizeOfFiemapStruc+extentCount*sizeOfFiemapExtentStruc)
	fm := (*fiemap)(unsafe.Pointer(&buf[0]))
	fm.start = start
	fm.length = length
	fm.flags = FILEMAP_FLAG_SYNC
	fm.extentCount = extentCount
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), IOCTL_FIEMAP, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil, true, fmt.Errorf("fiemap errno %v", errno)
	}

	extents := make([]fiemapExtent, fm.mappedExtents)
	done := fm.mappedExtents == 0
	lastOffs := start
	for i := uint32(0); i < fm.mappedExtents; i++ {
		rawinfo := (*fiemapExtent)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + uintptr(sizeOfFiemapStruc) + uintptr(i*sizeOfFiemapExtentStruc)))
		if rawinfo.Logical < lastOffs {
			return nil, true, fmt.Errorf("invalid order %v", rawinfo.Logical)
		}
		lastOffs = rawinfo.Logical
		extents[i].Logical = rawinfo.Logical
		extents[i].Physical = rawinfo.Physical
		extents[i].Length = rawinfo.Length
		extents[i].Flags = rawinfo.Flags
		done = rawinfo.Flags&FIEMAP_EXTENT_LAST != 0
	}

	return extents, done, nil
}

func getFileExtentsFp(file *os.File) ([]fiemapExtent, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	var all []fiemapExtent
	start := uint64(0)
	size := uint64(fileInfo.Size())
	for {
		part, done, err := ioctlFileMap(file, start, size-start)
		if err != nil {
			return nil, err
		}

		all = append(all, part...)
		if done {
			return all, nil
		}

		if len(part) == 0 {
			return nil, errors.New("unsupported")
		}
		last := part[len(part)-1]
		start = last.Logical + last.Length
	}
}

func FileDiskExtents(file string) (es []FileDiskExtentSegment, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, errors.Wrap(err, "Open")
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "Stat")
	}
	if info.IsDir() {
		return nil, errors.Errorf("%s is a directory", file)
	}
	stat := info.Sys().(*syscall.Stat_t)
	fileSize := info.Size()
	fileMaj := unix.Major(stat.Dev)
	fileMin := unix.Minor(stat.Dev)

	devNameTable, err := DeviceMajorTable()
	if err != nil {
		return nil, errors.Wrapf(err, "DeviceMajorTable")
	}
	fileDevMaj := DevMajor(fmt.Sprintf("%d:%d", fileMaj, fileMin))

	onBtrfs := false
	volume, ok := devNameTable[fileDevMaj]
	if !ok || volume == "" {
		devStr := string(fileDevMaj)

		// 兼容btrfs
		isbtrfs, e1 := IsBtrfs(file)
		mounts, e2 := VolumeMountpoints()
		mp, e3 := FindMountPoint(file)

		if e1 != nil || e2 != nil || e3 != nil || !isbtrfs {
			return nil, errors.Errorf("device path of %s not found", devStr)
		}

		onBtrfs = true
		btrfsDev := ""
		for _, m := range mounts {
			if m.Mountpoint == mp {
				btrfsDev = m.Device
				break
			}
		}
		if btrfsDev != "" {
			volume = btrfsDev
		} else {
			return nil, errors.New("btrfs device not found")
		}
	}

	eArr, err := getFileExtentsFp(f)
	if err != nil {
		return nil, errors.Wrapf(err, "getFileExtentsFp")
	}

	fileRegionsOnVolume := make([]volumeSegment, 0)
	if !onBtrfs {
		sz := int64(0)
		for _, d := range eArr {
			if sz >= fileSize {
				break
			}
			expectSize := fileSize - sz
			if expectSize > int64(d.Length) {
				expectSize = int64(d.Length)
			}
			fileRegionsOnVolume = append(fileRegionsOnVolume, volumeSegment{
				start: int64(d.Physical),
				size:  expectSize,
			})
			sz += expectSize
		}
	}
	if onBtrfs {
		if e := btrfsSync(file); e != nil {
			return nil, errors.Wrapf(e, "btrfsSync")
		}
		sz := int64(0)
		for _, d := range eArr {
			if sz >= fileSize {
				break
			}
			expectSize := fileSize - sz
			if expectSize > int64(d.Length) {
				expectSize = int64(d.Length)
			}

			// 在 btrfs 上,FIEMAP 返回的 d.Physical 实际是 btrfs 逻辑地址,
			// 需要用 btrfs-map-logical 换算成 volume(btrfsDev) 上的真实物理偏移。
			logical := int64(d.Physical)
			mirrors, err := btrfsMapLogical(volume, logical)
			if err != nil {
				return nil, errors.Wrapf(err, "btrfsMapLogical logical=%d device=%s", logical, volume)
			}

			mirror := pickBtrfsMirror(mirrors, volume)
			if mirror.Device != volume {
				// 多设备 btrfs(RAID0/10等),数据落在了 btrfsDev 之外的成员盘上。
				// 当前实现按单个 volume 做后续 LVM/磁盘分段匹配,无法直接支持这种情况。
				return nil, errors.Errorf(
					"multi-device btrfs not supported: extent on %s but expected volume %s",
					mirror.Device, volume)
			}

			fileRegionsOnVolume = append(fileRegionsOnVolume, volumeSegment{
				start: mirror.Physical,
				size:  expectSize,
			})
			sz += expectSize
		}
	}

	if len(fileRegionsOnVolume) == 0 {
		return nil, errors.Errorf("file %s has no regions on volume", file)
	}

	volumeRegionsOnDisk := make([]Segment, 0)
	ok, err = IsLVMDevice(volume)
	if err != nil {
		return nil, errors.Wrap(err, "IsLVMDevice")
	}
	if ok {
		volumeRegionsOnDisk, err = LVSegments(volume)
		if err != nil {
			return nil, errors.Wrapf(err, "LVSegments")
		}
	} else {
		// FIXME 兼容multipath和RAID
		seg, err := DiskOrPartitionSegment(volume)
		if err != nil {
			return nil, errors.Wrapf(err, "DiskOrPartitionSegment")
		}
		volumeRegionsOnDisk = append(volumeRegionsOnDisk, seg)
	}
	if len(volumeRegionsOnDisk) == 0 {
		return nil, errors.Errorf("volume %s has no regions on disk", volume)
	}

	for _, fe := range fileRegionsOnVolume {
		extentSize := fe.size
		fileExtentVolStart := fe.start
		fileExtentVolEnd := fileExtentVolStart + extentSize
		diskDelta := fe.start

		diskVolStart := int64(0)
		for _, ve := range volumeRegionsOnDisk {
			diskVolEnd := diskVolStart + int64(ve.Size)
			diskDelta = fileExtentVolStart - diskVolStart

			if fileExtentVolStart < diskVolStart {
				return nil, errors.New("unexcepted range")
			} else if fileExtentVolStart >= diskVolStart && fileExtentVolEnd <= diskVolEnd {
				// 全包含
				es = append(es, FileDiskExtentSegment{
					Disk:  ve.Disk,
					Start: int64(ve.Start) + diskDelta,
					Size:  extentSize,
				})
				break
			} else if fileExtentVolStart < diskVolEnd && fileExtentVolEnd > diskVolEnd {
				// 部分包含，做截断处理
				deltaExtentSize := diskVolEnd - fileExtentVolStart
				es = append(es, FileDiskExtentSegment{
					Disk:  ve.Disk,
					Start: int64(ve.Start) + diskDelta,
					Size:  deltaExtentSize,
				})
				extentSize -= deltaExtentSize
				fileExtentVolStart += deltaExtentSize
			}

			diskVolStart = diskVolEnd
		}
	}

	return es, nil
}

type mountInfo struct {
	MountPoint string
	Source     string
}

func GetFileDevice(filePath string) (major, minor uint32, devPath string, err error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return 0, 0, "", err
	}

	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		// 文件可能本身不是 symlink，忽略
	}

	if _, err := os.Stat(absPath); err != nil {
		return 0, 0, "", err
	}

	mounts, err := parseMountInfo()
	if err != nil {
		return 0, 0, "", err
	}

	// 最长前缀匹配 mount point
	var best *mountInfo
	bestLen := -1

	for i := range mounts {
		m := &mounts[i]

		if !strings.HasPrefix(absPath, m.MountPoint) {
			continue
		}

		// 避免 /opt 匹配 /opt2
		if len(absPath) > len(m.MountPoint) &&
			!strings.HasSuffix(m.MountPoint, "/") &&
			absPath[len(m.MountPoint)] != '/' {
			continue
		}

		if len(m.MountPoint) > bestLen {
			best = m
			bestLen = len(m.MountPoint)
		}
	}

	if best == nil {
		return 0, 0, "", fmt.Errorf("mount not found")
	}

	source := best.Source

	// btrfs subvolume:
	// /dev/mapper/system-root[/@/opt]
	if idx := strings.Index(source, "["); idx >= 0 {
		source = source[:idx]
	}

	// pseudo fs
	if !strings.HasPrefix(source, "/dev/") {
		return 0, 0, "", fmt.Errorf(
			"not block device mount: %s",
			source,
		)
	}

	realDev, err := filepath.EvalSymlinks(source)
	if err != nil {
		realDev = source
	}

	info, err := os.Stat(realDev)
	if err != nil {
		return 0, 0, "", err
	}

	st := info.Sys().(*syscall.Stat_t)

	major = unix.Major(uint64(st.Rdev))
	minor = unix.Minor(uint64(st.Rdev))

	return major, minor, realDev, nil
}

func parseMountInfo() ([]mountInfo, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		// 老系统 fallback
		return parseProcMounts()
	}
	defer f.Close()

	var result []mountInfo

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()

		// mountinfo:
		// ... mountpoint ... - fstype source options
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}

		left := strings.Fields(line[:sep])
		right := strings.Fields(line[sep+3:])

		if len(left) < 5 || len(right) < 2 {
			continue
		}

		result = append(result, mountInfo{
			MountPoint: left[4],
			Source:     right[1],
		})
	}

	return result, scanner.Err()
}

func parseProcMounts() ([]mountInfo, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []mountInfo

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}

		result = append(result, mountInfo{
			Source:     fields[0],
			MountPoint: fields[1],
		})
	}

	return result, scanner.Err()
}
