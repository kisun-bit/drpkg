package extend

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"golang.org/x/sys/unix"
)

func GetFileSize(fileName string) (size uint64, err error) {
	var errno syscall.Errno
	info, err := os.Stat(fileName)
	if err != nil {
		return 0, err
	}
	fm := info.Mode()
	if fm&os.ModeDevice != 0 {
		f, err := os.Open(fileName)
		if err != nil {
			return 0, err
		}
		defer f.Close()

		if runtime.GOARCH == "386" {
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize, uintptr(unsafe.Pointer(&size)))
			size <<= 9
		} else {
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize64, uintptr(unsafe.Pointer(&size)))
		}
		if errno != 0 {
			return 0, errno
		}
		return size, nil
	} else {
		return uint64(info.Size()), nil
	}
}

func MatchDevLinkName(base string, deviceName string) string {
	if IsExisted(base) {
		files, err := os.ReadDir(base)
		if err != nil {
			return ""
		}
		for _, file := range files {
			filename := file.Name()
			path := filepath.Join(base, filename)
			linkTarget, err := filepath.EvalSymlinks(path)
			if err != nil {
				return ""
			}
			if linkTarget == filepath.Join("/dev", deviceName) {
				return filename
			}
		}
	}
	return ""
}

func DevBlockSize(dev string) (uint32, error) {
	fd, err := os.OpenFile(dev, os.O_RDONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("fail to open %s: %w", dev, err)
	}
	defer fd.Close()

	var blksize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), uintptr(LinuxIOCTLGetBLKBSZ), uintptr(unsafe.Pointer(&blksize)))
	if errno != 0 {
		return 0, os.NewSyscallError("ioctl", errno)
	}
	return blksize, nil
}

func DevPhysicalBlockSize(dev string) (uint32, error) {
	fd, err := os.OpenFile(dev, os.O_RDONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("fail to open %s: %w", dev, err)
	}
	defer fd.Close()

	var physicalBlksize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), uintptr(LinuxIOCTLGetBLKPBSZ), uintptr(unsafe.Pointer(&physicalBlksize)))
	if errno != 0 {
		return 0, os.NewSyscallError("ioctl", errno)
	}
	return physicalBlksize, nil
}

type DevMountpoint struct {
	Device     string
	Major      DevMajor
	Mountpoint string
	Filesystem string
}

type DevMajor string

func (dm DevMajor) splitMajor() (major, minor string) {
	parts := strings.SplitN(string(dm), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (dm DevMajor) MajorNum() string {
	major, _ := dm.splitMajor()
	return major
}

func (dm DevMajor) MinNum() string {
	_, minor := dm.splitMajor()
	return minor
}

func (dm DevMajor) IsLV() bool {
	return dm.MajorNum() == "253"
}

func VolumeMountpoints() (volumeMountpoints []DevMountpoint, err error) {
	mountpointWithDev := make(map[string]string, 0)
	mountBinText, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(mountBinText))
	for scanner.Scan() {
		lineGroups := strings.Split(scanner.Text(), " - ")
		if len(lineGroups) != 2 {
			continue
		}
		fieldsPre := strings.Fields(lineGroups[0])
		fieldsSuf := strings.Fields(lineGroups[1])

		if len(fieldsPre) < 5 || len(fieldsSuf) < 2 {
			continue
		}
		mountpoint, mountdev, major := fieldsPre[4], fieldsSuf[1], fieldsPre[2]
		if _, ok := mountpointWithDev[mountpoint]; ok {
			// 挂载点被设备重复挂载的，始终以第一挂载设备为主
			continue
		}
		if !strings.HasPrefix(mountdev, "/dev") {
			continue
		}
		mountpointWithDev[mountpoint] = mountdev
		volumeMountpoints = append(volumeMountpoints, DevMountpoint{
			Device:     mountdev,
			Major:      DevMajor(major),
			Mountpoint: mountpoint,
			Filesystem: fieldsSuf[0],
		})
	}

	return volumeMountpoints, nil
}

func DeviceMajorTable(filter ...string) (map[DevMajor]string, error) {
	dms := make(map[DevMajor]string)

	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		devPath := filepath.Join("/dev", entry.Name())
		if !funk.InStrings(filter, devPath) {
			continue
		}

		var stat syscall.Stat_t
		if err := syscall.Stat(devPath, &stat); err != nil {
			continue
		}

		// 检查是否是设备文件
		if stat.Mode&syscall.S_IFCHR != 0 || stat.Mode&syscall.S_IFBLK != 0 {
			major := uint64(stat.Rdev) >> 8
			minor := uint64(stat.Rdev) & 0xff
			dms[DevMajor(fmt.Sprintf("%d:%d", major, minor))] = devPath
		}
	}

	return dms, nil
}

// MountpointUsage 存储使用情况 (available bytes, byte capacity, byte usage, total inodes, inodes free, inode usage, error)
func MountpointUsage(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	available := int64(statfs.Bavail) * statfs.Bsize

	capacity := int64(statfs.Blocks) * statfs.Bsize

	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * statfs.Bsize

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}

func GetBootTime() (time.Time, error) {
	content, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "read /proc/uptime")
	}

	fields := strings.Fields(string(content))
	if len(fields) < 1 {
		return time.Time{}, errors.Wrapf(err, "fields /proc/uptime")
	}

	uptimeSeconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse uptime from /proc/uptime")
	}

	bootTime := time.Now().Add(-time.Duration(uptimeSeconds * float64(time.Second)))
	return bootTime, nil
}

func ListDisks() (disks []string, err error) {
	sysBlockDir := "/sys/class/block"
	subFiles, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return nil, err
	}
	for _, file := range subFiles {
		filename := file.Name()
		path := filepath.Join(sysBlockDir, filename, "device/type")
		devType, err := ReadIntFromFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if devType != 0 {
			continue
		}
		disks = append(disks, filepath.Join("/dev", filename))
	}
	return disks, nil
}

func GetDiskSectors(disk string) (int64, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "size"))
	return r, errors.Wrapf(e, "get disk sectos")
}

func GetDiskSectorSize(disk string) (int64, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "queue/hw_sector_size"))
	return r, errors.Wrapf(e, "get disk secto-size")
}

func GetDiskVendor(disk string) (string, error) {
	ret, err := ReadStringFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/vendor"))
	if err == nil {
		return ret, nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk vendor")
}

func GetDiskModel(disk string) (string, error) {
	ret, err := ReadStringFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/model"))
	if err == nil {
		return ret, nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk model")
}

func GetDiskSerialNumber(disk string) (string, error) {
	ret, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/wwid"))
	if err == nil && len(ret) > 4 {
		return strings.TrimSpace(string(ret[4:])), nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		// 获取vpd_80序列号
		vpdret, vpderr := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/vpd_pg80"))
		if vpderr == nil && len(vpdret) > 4 {
			return strings.TrimSpace(string(vpdret[4:])), nil
		}
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk serial number")
}

func IsDiskReadonly(disk string) (bool, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "ro"))
	return r != 0, errors.Wrapf(e, "get disk read-only attribute")
}
