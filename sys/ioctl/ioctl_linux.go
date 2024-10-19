package ioctl

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/kisun-bit/drpkg/util"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

func GeneratePartDeviceName(diskPath string, partIndex int) string {
	endWithDigit := unicode.IsDigit(rune(diskPath[len(diskPath)-1]))
	partSuffix := strconv.Itoa(partIndex)
	if endWithDigit {
		partSuffix = "p" + partSuffix
	}
	return diskPath + partSuffix
}

// UsageInfo 存储使用情况 (available bytes, byte capacity, byte usage, total inodes, inodes free, inode usage, error)
func UsageInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	available := int64(statfs.Bavail) * int64(statfs.Bsize)

	capacity := int64(statfs.Blocks) * int64(statfs.Bsize)

	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize)

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}

func QueryFileSize(fileName string) (size uint64, err error) {
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

func sysfsExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// sysLinkTargetExists 用于查找某目录下是否存在某一链接文件的目标文件为指定名称的文件，若存在则返回true.
func sysLinkTargetExists(searchDir, name string, recursive bool) bool {
	var found bool
	var walkFunc filepath.WalkFunc = func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if filepath.Base(target) == name {
				found = true
				return filepath.SkipDir
			}
		}
		if !recursive && info.IsDir() && path != searchDir {
			return filepath.SkipDir
		}
		return nil
	}
	err := filepath.Walk(searchDir, walkFunc)
	if err != nil {
		return false
	}
	return found
}

func GetMountIDs() (mis map[string]deviceMountInfo, err error) {
	mis = make(map[string]deviceMountInfo)
	mountInfo, err := os.ReadFile(ProcSelfMountInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", ProcSelfMountInfo, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(mountInfo))
	for scanner.Scan() {
		line := scanner.Text()
		lineGroups := strings.Split(line, " - ")
		if len(lineGroups) < 2 {
			continue
		}

		fieldsPre := strings.Fields(lineGroups[0])
		fieldsSuf := strings.Fields(lineGroups[1])

		if len(fieldsPre) < 5 || len(fieldsSuf) < 2 {
			continue
		}

		// line示例:
		// 情况一: `120 95 253:2 / /home rw,relatime shared:67 - ext4 /dev/mapper/openeuler_runstor-home rw`
		// 情况二: `25 21 252:17 / /data2 rw,relatime - ext4 /dev/vdb1 rw,barrier=1,data=ordered`
		mis[fieldsPre[2]] = deviceMountInfo{
			Mount:      true,
			MountPath:  fieldsPre[4],
			ReadOnly:   strings.Contains(fieldsSuf[2], "ro"),
			Filesystem: fieldsSuf[0],
		}
	}
	return mis, nil
}

// GetStorage 获取.
func GetStorage() (*ResourcesStorage, error) {
	storage := ResourcesStorage{}
	storage.Disks = []ResourcesStorageDisk{}

	// 检测所有块设备.
	if sysfsExists(SysClassBlock) {
		entries, err := os.ReadDir(SysClassBlock)
		if err != nil {
			return nil, fmt.Errorf("failed to list %q: %w", SysClassBlock, err)
		}

		// 获取挂载点信息.
		mountedIDs, err := GetMountIDs()
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			entryName := entry.Name()
			entryPath := filepath.Join(SysClassBlock, entryName)
			devicePath := filepath.Join(entryPath, "device")

			if !sysfsExists(devicePath) {
				continue
			}

			disk := ResourcesStorageDisk{}
			disk.Name = entryName
			disk.Path = filepath.Join("/dev", disk.Name)

			if sysfsExists(filepath.Join(devicePath, "firmware_rev")) {
				firmwareRevision, err := os.ReadFile(filepath.Join(devicePath, "firmware_rev"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(devicePath, "firmware_rev"), err)
				}

				disk.FirmwareVersion = strings.TrimSpace(string(firmwareRevision))
			}

			// 设备节点.
			diskDev, err := os.ReadFile(filepath.Join(entryPath, "dev"))
			if err != nil {
				if os.IsNotExist(err) {
					// 此情况仅出现在多路径设备中, 直接跳过就行, 因为我们仅关心主要的节点.
					continue
				}

				return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(entryPath, "dev"), err)
			}

			disk.DeviceNumber = strings.TrimSpace(string(diskDev))

			// PCI 地址信息.
			pciAddr, err := pciAddress(devicePath)
			if err != nil {
				return nil, fmt.Errorf("failed to find PCI address for %q: %w", devicePath, err)
			}

			if pciAddr != "" {
				disk.PCIAddress = pciAddr
			}

			// USB 地址信息.
			usbAddr, err := usbAddress(devicePath)
			if err != nil {
				return nil, fmt.Errorf("failed to find USB address for %q: %w", devicePath, err)
			}

			if usbAddr != "" {
				disk.USBAddress = usbAddr
			}

			// NUMA node
			if sysfsExists(filepath.Join(devicePath, "numa_node")) {
				numaNode, err := readInt(filepath.Join(devicePath, "numa_node"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(devicePath, "numa_node"), err)
				}

				if numaNode > 0 {
					disk.NUMANode = uint64(numaNode)
				}
			}

			// Disk model
			if sysfsExists(filepath.Join(devicePath, "model")) {
				diskModel, err := os.ReadFile(filepath.Join(devicePath, "model"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(devicePath, "model"), err)
				}

				disk.Model = strings.TrimSpace(string(diskModel))
			}

			// 磁盘类型.
			if sysfsExists(filepath.Join(devicePath, "subsystem")) {
				diskSubsystem, err := filepath.EvalSymlinks(filepath.Join(devicePath, "subsystem"))
				if err != nil {
					return nil, fmt.Errorf("failed to find %q: %w", filepath.Join(devicePath, "subsystem"), err)
				}

				// 例如: "../../../../../../../bus/scsi"中取"scsi"
				disk.Type = filepath.Base(diskSubsystem)

				if disk.Type == "rbd" {
					// 忽略rbd设备，因为rbd设备并非是本地存储设备.
					continue
				}
			}

			// Read-only
			diskRo, _ := readUint(filepath.Join(entryPath, "ro"))
			if err != nil {
				return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(entryPath, "ro"), err)
			}

			disk.ReadOnly = diskRo == 1

			// Size
			diskSize, err := readUint(filepath.Join(entryPath, "size"))
			if err != nil {
				return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(entryPath, "size"), err)
			}

			disk.Size = diskSize * 512

			// Removable
			diskRemovable, err := readUint(filepath.Join(entryPath, "removable"))
			if err != nil {
				return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(entryPath, "removable"), err)
			}

			disk.Removable = diskRemovable == 1

			// WWN
			if sysfsExists(filepath.Join(entryPath, "wwid")) {
				diskWWN, err := os.ReadFile(filepath.Join(entryPath, "wwid"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(entryPath, "wwid"), err)
				}

				disk.WWN = strings.TrimSpace(string(diskWWN))
			}

			if strings.HasPrefix(disk.Name, "sr") && disk.Removable {
				disk.Type = "cdrom"

				// 大多数 cdrom 驱动器都会将其报告为此大小，无论介质大小如何
				if disk.Size == 0x1fffff*512 {
					disk.Size = 0
				}
			}

			if v, ok := mountedIDs[disk.DeviceNumber]; ok {
				disk.Mounted = v.Mount
				disk.Filesystem = v.Filesystem
				if !disk.ReadOnly {
					disk.ReadOnly = v.ReadOnly
				}
				disk.MountPath = v.MountPath
			}

			// 查询分区信息.
			disk.Partitions = []ResourcesStorageDiskPartition{}
			for _, subEntry := range entries {
				subEntryName := subEntry.Name()
				subEntryPath := filepath.Join(SysClassBlock, subEntryName)

				if !strings.HasPrefix(subEntryName, entryName) {
					continue
				}

				if !sysfsExists(filepath.Join(subEntryPath, "partition")) {
					continue
				}

				partition := ResourcesStorageDiskPartition{}
				partition.Name = subEntryName

				// 匹配partuuid
				partition.PartUUID = MatchDiskBy(DevDiskByPartUUID, subEntryName)

				// 匹配uuid
				partition.UUID = MatchDiskBy(DevDiskByUUID, subEntryName)

				// 查找udev Name
				partition.DeviceID = MatchDiskBy(DevDiskByID, subEntryName)

				// 分区号.
				partitionNumber, err := readUint(filepath.Join(subEntryPath, "partition"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(subEntryPath, "partition"), err)
				}

				partition.PartitionIndex = partitionNumber

				// 设备节点.
				partitionDev, err := os.ReadFile(filepath.Join(subEntryPath, "dev"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(subEntryPath, "dev"), err)
				}

				partition.DeviceNumber = strings.TrimSpace(string(partitionDev))

				if v, ok := mountedIDs[partition.DeviceNumber]; ok {
					partition.Mounted = v.Mount
					partition.Filesystem = v.Filesystem
					if !partition.ReadOnly {
						partition.ReadOnly = v.ReadOnly
					}
					partition.MountPath = v.MountPath
				}

				// 若磁盘某一分区处于挂载状态, 那么就修正此磁盘状态也为挂载状态.
				if partition.Mounted {
					disk.Mounted = true
				}

				// Read-only
				// centos6环境下, 无此ro文件, 所以忽略错误.
				partitionRo, _ := readUint(filepath.Join(subEntryPath, "ro"))

				partition.ReadOnly = partitionRo == 1

				partitionSize, err := readUint(filepath.Join(subEntryPath, "size"))
				if err != nil {
					return nil, fmt.Errorf("failed to read %q: %w", filepath.Join(subEntryPath, "size"), err)
				}

				partition.Size = partitionSize * 512

				disk.Partitions = append(disk.Partitions, partition)
			}

			// 查找udev路径.
			disk.DevicePCIPath = MatchDiskBy(DevDiskByPath, entryName)

			// 匹配partuuid
			disk.PartUUID = MatchDiskBy(DevDiskByPartUUID, entryName)

			// 匹配uuid
			disk.UUID = MatchDiskBy(DevDiskByUUID, entryName)

			// 查找udev Name
			disk.DeviceID = MatchDiskBy(DevDiskByID, entryName)

			// 直接拉取磁盘信息.
			err = storageAddDriveInfo(filepath.Join("/dev", entryName), &disk)
			if err != nil {
				return nil, fmt.Errorf("failed to retrieve disk information from %q: %w", filepath.Join("/dev", entryName), err)
			}

			// 如果未设置 RtationRateRPM 并且驱动器是旋转的，请将 RtationRateRPM 设置为 1.
			diskRotationalPath := filepath.Join("/sys/class/block/", entryName, "queue/rotational")
			if disk.RtationRateRPM == 0 && sysfsExists(diskRotationalPath) {
				diskRotational, err := readUint(diskRotationalPath)
				if err == nil {
					disk.RtationRateRPM = diskRotational
				}
			}

			storage.Disks = append(storage.Disks, disk)
		}
	}

	FixResourcesStorage(&storage)
	return &storage, nil
}

func FixResourcesStorage(rs *ResourcesStorage) {
	rs.Total = 0
	for _, card := range rs.Disks {
		if rs.Disks != nil {
			rs.Total += uint64(len(card.Partitions))
		}

		rs.Total++
	}
}

func pciAddress(devicePath string) (string, error) {
	deviceDeviceDir, err := getDeviceDir(devicePath)
	if err != nil {
		return "", err
	}

	// 检查我们是否列出了子系统.
	if !sysfsExists(filepath.Join(deviceDeviceDir, "subsystem")) {
		return "", nil
	}

	// 追踪设备.
	linkTarget, err := filepath.EvalSymlinks(deviceDeviceDir)
	if err != nil {
		return "", fmt.Errorf("failed to find %q: %w", deviceDeviceDir, err)
	}

	// 提取子系统.
	subsystemTarget, err := filepath.EvalSymlinks(filepath.Join(linkTarget, "subsystem"))
	if err != nil {
		return "", fmt.Errorf("failed to find %q: %w", filepath.Join(deviceDeviceDir, "subsystem"), err)
	}

	subsystem := filepath.Base(subsystemTarget)

	if subsystem == "virtio" {
		// 如果是 virtio，请考虑父级.
		linkTarget = filepath.Dir(linkTarget)
		subsystemTarget, err := filepath.EvalSymlinks(filepath.Join(linkTarget, "subsystem"))
		if err != nil {
			return "", fmt.Errorf("failed to find %q: %w", filepath.Join(deviceDeviceDir, "subsystem"), err)
		}

		subsystem = filepath.Base(subsystemTarget)
	}

	if subsystem != "pci" {
		return "", nil
	}

	//地址是最后一个条目.
	return filepath.Base(linkTarget), nil
}

// getDeviceDir 返回包含设备所需设备信息的目录。
// 它将 /device 附加到路径，直到它最终成为一个常规文件的子 /device。
// 对于包含 wwan 等子设备的设备，需要此函数。
func getDeviceDir(devicePath string) (string, error) {
	for {
		deviceDir := filepath.Join(devicePath, "device")
		fileInfo, err := os.Stat(deviceDir)
		if os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", fmt.Errorf("unable to get file info for %q: %w", deviceDir, err)
		} else if fileInfo.Mode().IsRegular() {
			break
		}

		devicePath = deviceDir
	}

	return devicePath, nil
}

// usbAddress 推测设备的 USB 地址 (bus:dev).
func usbAddress(devicePath string) (string, error) {
	devicePath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve device symlink: %w", err)
	}

	if !strings.Contains(devicePath, "/usb") {
		return "", nil
	}

	path_ := devicePath
	for {
		// 避免无限循环.
		if path_ == "" || path_ == "/" {
			return "", nil
		}

		// 检查我们是否找到 USB 设备路径.
		if !sysfsExists(filepath.Join(path_, "busnum")) || !sysfsExists(filepath.Join(path_, "devnum")) {
			path_ = filepath.Dir(path_)
			continue
		}

		// 总线地址信息.
		bus, err := readUint(filepath.Join(path_, "busnum"))
		if err != nil {
			return "", fmt.Errorf("unable to parse USB bus addr: %w", err)
		}

		// 设备地址信息.
		dev, err := readUint(filepath.Join(path_, "devnum"))
		if err != nil {
			return "", fmt.Errorf("unable to parse USB device addr: %w", err)
		}

		return fmt.Sprintf("%d:%d", bus, dev), nil
	}
}

func readUint(path string) (uint64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func readInt(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}

	value, err := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return -1, err
	}

	return value, nil
}

func storageAddDriveInfo(devicePath string, disk *ResourcesStorageDisk) error {
	// 尝试打开设备文件.
	f, err := os.Open(devicePath)
	if err == nil {
		defer func() { _ = f.Close() }()

		// 获取块大小.
		// 这不能仅使用 unix.Ioctl 来完成，因为该特定返回值是 32 位并将其填充到大端系统上的 64 位变量中.
		var res int32

		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.BLKPBSZGET, uintptr(unsafe.Pointer(&res)))
		if errno != 0 {
			return fmt.Errorf("failed to BLKPBSZGET: %w", errno)
		}

		disk.BlockSize = uint64(res)
	}

	// 获取udev信息.
	udevInfo := filepath.Join(RunUdevData, fmt.Sprintf("b%s", disk.DeviceNumber))
	if sysfsExists(udevInfo) {
		f, err := os.Open(udevInfo)
		if err != nil {
			return fmt.Errorf("failed to open %q: %w", udevInfo, err)
		}

		defer func() { _ = f.Close() }()

		udevProperties := map[string]string{}
		udevInfo := bufio.NewScanner(f)
		for udevInfo.Scan() {
			line := strings.TrimSpace(udevInfo.Text())

			if !strings.HasPrefix(line, "E:") {
				continue
			}

			fields := strings.SplitN(line, "=", 2)
			if len(fields) != 2 {
				continue
			}

			key := strings.TrimSpace(fields[0])
			value := strings.TrimSpace(fields[1])
			udevProperties[key] = value
		}

		// 更细粒度的磁盘类型.
		if udevProperties["E:ID_CDROM"] == "1" {
			disk.Type = "cdrom"
		} else if udevProperties["E:ID_USB_DRIVER"] == "usb-storage" {
			disk.Type = "usb"
		} else if udevProperties["E:ID_ATA_SATA"] == "1" {
			disk.Type = "sata"
		}

		// 固件信息.
		if udevProperties["E:ID_REVISION"] != "" && disk.FirmwareVersion == "" {
			disk.FirmwareVersion = udevProperties["E:ID_REVISION"]
		}

		// 序列号.
		serial := udevProperties["E:SCSI_IDENT_SERIAL"]
		if serial == "" {
			serial = udevProperties["E:ID_SCSI_SERIAL"]
		}

		if serial == "" {
			serial = udevProperties["E:ID_SERIAL_SHORT"]
		}

		if serial == "" {
			serial = udevProperties["E:ID_SERIAL"]
		}

		disk.Serial = serial

		// 型号（尝试从编码值获取原始字符串）.
		if udevProperties["E:ID_MODEL_ENC"] != "" {
			model, err := udevDecode(udevProperties["E:ID_MODEL_ENC"])
			if err == nil {
				disk.Model = strings.TrimSpace(model)
			} else if udevProperties["E:ID_MODEL"] != "" {
				disk.Model = udevProperties["E:ID_MODEL"]
			}
		} else if udevProperties["E:ID_MODEL"] != "" {
			disk.Model = udevProperties["E:ID_MODEL"]
		}

		// 获取 每分钟转速.
		if udevProperties["E:ID_ATA_ROTATION_RATE_RPM"] != "" && disk.RtationRateRPM == 0 {
			valueUint, err := strconv.ParseUint(udevProperties["E:ID_ATA_ROTATION_RATE_RPM"], 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse RtationRateRPM value: %w", err)
			}

			disk.RtationRateRPM = valueUint
		}
	}

	return nil
}

func udevDecode(s string) (string, error) {
	// 参考 https://github.com/systemd/systemd/blob/main/src/shared/device-nodes.c#L19
	ret := ""
	for i := 0; i < len(s); i++ {
		// udev 将非 devnode 支持的字符转换为四字节编码的十六进制字符串.
		if s[i] == '\\' && i+4 <= len(s) && s[i+1] == 'x' {
			hexValue := s[i+2 : i+4]
			strValue, err := hex.DecodeString(hexValue)
			if err == nil {
				ret += string(strValue)
				i += 3
			} else {
				return ret, err
			}
		} else {
			ret += s[i : i+1]
		}
	}

	return ret, nil
}

func MatchDiskBy(base string, deviceName string) string {
	if sysfsExists(base) {
		links, err := os.ReadDir(base)
		if err != nil {
			return ""
		}
		for _, link := range links {
			linkName := link.Name()
			linkPath := filepath.Join(base, linkName)
			linkTarget, err := filepath.EvalSymlinks(linkPath)
			if err != nil {
				return ""
			}
			if linkTarget == filepath.Join("/dev", deviceName) {
				return linkName
			}
		}
	}
	return ""
}

func QueryLVDmName(vgName, lvName string) (dm string, err error) {
	lvDev := fmt.Sprintf("/dev/mapper/%s-%s", vgName, lvName)
	if sysfsExists(lvDev) {
		linkTarget, err := filepath.EvalSymlinks(lvDev)
		if err == nil {
			return filepath.Base(linkTarget), nil
		}
	}
	lvDev = fmt.Sprintf("/dev/%s/%s", vgName, lvName)
	linkTarget, err := filepath.EvalSymlinks(lvDev)
	if err == nil {
		return filepath.Base(linkTarget), nil
	}
	return "", errors.Errorf("failed to query dm-name for %s/%s, %v", vgName, lvName, err)
}

func QueryDeviceNumber(device string) string {
	if sysfsExists(SysClassBlock) {
		devFile := filepath.Join(SysClassBlock, device, "dev")
		b, err := os.ReadFile(devFile)
		if err == nil {
			return util.TrimAllSpace(string(b))
		}
	}
	return ""
}

func QueryOSRelease() (name, version, id, versionID, prettyName string, err error) {
	o, e := exec.Command("sh", "-c", "cat /etc/*release").Output()
	if e != nil {
		return "", "", "", "", "", errors.Errorf("failed to query os release info, output(`%s`) error(%v)", o, e)
	}

	prefixNAME := "NAME"
	prefixVERSION := "VERSION"
	prefixID := "ID"
	prefixVERSIONID := "VERSION_ID"
	prefixPRETTYNAME := "PRETTY_NAME"

	formatValueFunc := func(_value string) string {
		return strings.TrimSuffix(strings.TrimPrefix(_value, "\""), "\"")
	}

	for _, line := range strings.Split(string(o), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		lineItems := strings.Split(line, "=")
		if len(lineItems) < 2 {
			continue
		}
		switch lineItems[0] {
		case prefixNAME:
			name = formatValueFunc(lineItems[1])
		case prefixPRETTYNAME:
			prettyName = formatValueFunc(lineItems[1])
		case prefixID:
			id = formatValueFunc(lineItems[1])
		case prefixVERSIONID:
			versionID = formatValueFunc(lineItems[1])
		case prefixVERSION:
			version = formatValueFunc(lineItems[1])
		}
	}

	if name == "" && version == "" && id == "" && versionID == "" && prettyName == "" {
		// 尝试使用 lsb_release 命令解析.
		o, e = exec.Command("sh", "-c", "lsb_release -ds").Output()
		if e == nil {
			prettyName = formatValueFunc(string(o))
		}
		o, e = exec.Command("sh", "-c", "lsb_release -is").Output()
		if e == nil {
			name = formatValueFunc(string(o))
			id = name
		}
		o, e = exec.Command("sh", "-c", "lsb_release -rs").Output()
		if e == nil {
			versionID = formatValueFunc(string(o))
		}
		o, e = exec.Command("sh", "-c", "lsb_release -cs").Output()
		if e == nil {
			version = fmt.Sprintf("%s (%s)", versionID, formatValueFunc(string(o)))
		}
	}

	if name == "" && version == "" && id == "" && versionID == "" && prettyName == "" {
		err = errors.Errorf("unable to detect release info from /etc/*release")
	}
	return
}

func DefaultBootKernelInfo(osDistribute string) (kernel, kernelImg, initrd string, err error) {
	var (
		bootDir = "/boot"
		//systemMapPrefix = "System.map-"
	)
	//ds, err := os.ReadDir(bootDir)
	//if err != nil {
	//	return "", "", "", errors.Errorf("failed to list files from %s for detecting kernel version, %v", bootDir, err)
	//}
	//for _, d := range ds {
	//	_, kernelVer, found := strings.Cut(d.Name(), systemMapPrefix)
	//	if !found {
	//		continue
	//	}
	//	kernel = kernelVer
	//}
	//if kernel == "" {
	//	return "", "", "", errors.Errorf("failed to query default kernel")
	//}
	o, e := exec.Command("sh", "-c", "uname -r").Output()
	if e != nil {
		return "", "", "", errors.Errorf("failed to exec `uname -r` for detecting kernel version, output(`%s`) error(`%v`)", o, e)
	}
	kernel = strings.TrimSpace(string(o))
	kernelImg = path.Join(bootDir, "vmlinuz"+"-"+kernel)
	// 对于内存操作系统而言. 其内核包可能是一个链接文件.
	if !(sysfsExists(kernelImg) || (!sysfsExists(kernelImg) && sysLinkTargetExists(bootDir, kernelImg, false))) {
		return "", "", "", fmt.Errorf("kernel image %q not found", kernel)
	}
	kernelImg = filepath.Base(kernelImg)
	switch strings.ToLower(osDistribute) {
	case "ubuntu":
		fallthrough
	case "debian":
		initrd = fmt.Sprintf("initrd.img-%s", kernel)
	default:
		initrd = fmt.Sprintf("initramfs-%s.img", kernel)
	}
	initrdAbs := filepath.Join(bootDir, initrd)
	// 对于内存操作系统而言. 其内存启动镜像可能是一个链接文件.
	if !(sysfsExists(initrdAbs) || (!sysfsExists(initrdAbs) && sysLinkTargetExists(bootDir, initrd, false))) {
		return "", "", "", errors.Errorf("ramdisk image %q not found", initrdAbs)
	}
	return
}

func Kernels() (ks []string, err error) {
	if sysfsExists(KernelModels) {
		ds, err := os.ReadDir(KernelModels)
		if err != nil {
			return ks, err
		}
		for _, d := range ds {
			ks = append(ks, d.Name())
		}
	}
	if len(ks) == 0 {
		return ks, errors.Errorf("kernels not found")
	}
	return ks, nil
}

//func IsBootByUEFI() (bool, error) {
//	efiDirs := []string{
//		"/sys/firmware/efi/efivars",
//		"/sys/firmware/efi/vars",
//	}
//	for _, efiDir := range efiDirs {
//		if !sysfsExists(efiDir) {
//			return false, nil
//		}
//	}
//	return true, nil
//}

func IsLiveCDEnv() bool {
	o, _ := exec.Command("sh", "-c", `mount | grep 'on / ' | awk '{print $5}'`).Output()
	if strings.Contains(string(o), "tmpfs") || strings.Contains(string(o), "overlay") {
		return true
	}
	o, _ = exec.Command("sh", "-c", `mount | grep 'on /run/initramfs/live ' | awk '{print $5}'`).Output()
	if strings.Contains(string(o), "iso9660") {
		return true
	}
	if _, eStat := os.Stat("/etc/initrd-release"); eStat == nil {
		return true
	}
	//if _, eStat := os.Stat("/run/initramfs/live"); eStat == nil {
	//	return true
	//}
	//r, _, _ := command.ExecV1(`ps aux | grep -E 'casper|live-boot' | grep -v grep > /dev/null`)
	//if r == 0 {
	//	return true
	//}
	return false
}

func GrubVersionAndPathForOnlineSystem() (version int, grubInstallPath, grubMkConfigPath string, err error) {
	_parseBinPath := func(_str, _bin string) (_binPath string, _ok bool) {
		_ss := strings.Fields(_str)
		for _, _s := range _ss {
			if filepath.IsAbs(_s) && filepath.Base(_s) == _bin {
				return _s, true
			}
		}
		return "", false
	}
	o, _ := exec.Command("sh", "-c", "whereis -b grub2-install").Output()
	binPath, ok := _parseBinPath(string(o), "grub2-install")
	if ok {
		return 2, binPath, filepath.Join(filepath.Dir(binPath), "grub2-mkconfig"), nil
	}

	o, e := exec.Command("sh", "-c", "grub-install --version").Output()
	if e != nil {
		return -1, "", "", errors.Errorf("failed to detect version for GRUB, ouput(`%s`) error(`%v`)", o, e)
	}
	versionStr := strings.TrimSpace(string(o))
	if strings.HasPrefix(versionStr, "grub-install (GRUB) 2.") { // ubuntu系列
		version = 2
	} else if strings.HasPrefix(versionStr, "grub2-install (GRUB) 2.") {
		version = 2
	} else if strings.HasPrefix(versionStr, "grub-install (GNU GRUB 0.97)") {
		version = 1
	} else {
		// 通用的判定, 匹配出第一个数字版本号进行判定
		re := regexp.MustCompile(`.*GRUB.*(\d+\.\d+)`)
		match := re.FindStringSubmatch(versionStr)
		if len(match) > 1 {
			ver := match[1]
			verInt64 := util.MustInt64(string(ver[0]))
			if verInt64 > 0 {
				version = int(verInt64)
			}
		}
	}
	if version <= 0 {
		return -1, "", "", errors.Errorf("failed to parse version from %s", o)
	}
	o, _ = exec.Command("sh", "-c", "whereis -b grub-install").Output()
	binPath, ok = _parseBinPath(string(o), "grub-install")
	if ok {
		grubMkConfigPath = ""
		if version >= 2 {
			grubMkConfigPath = filepath.Join(filepath.Dir(binPath), "grub-mkconfig")
		}
		return version, binPath, grubMkConfigPath, nil
	}
	return 0, "", "", errors.Errorf("unable to get GRUB version and path")
}

// GRUB2Target 获取系统默认的GRUB Target.
// 参考于GRUB2项目中函数：get_default_platform.
func GRUB2Target(goarch string, bootMode string) (string, error) {
	isEFISystem := bootMode == "UEFI"
	if goarch == "amd64" && isEFISystem {
		return "x86_64-efi", nil
	} else if goarch == "amd64" && !isEFISystem {
		return "i386-pc", nil
	} else if goarch == "386" && isEFISystem {
		return "i386-efi", nil
	} else if goarch == "386" && !isEFISystem {
		return "i386-pc", nil
	} else if goarch == "arm64" && isEFISystem {
		return "arm64-efi", nil
	} else if goarch == "arm" && isEFISystem {
		return "arm-efi", nil
	} else if goarch == "arm" && !isEFISystem {
		return "arm-uboot", nil
	} else if goarch == "loong64" && isEFISystem {
		return "loongarch64-efi", nil
	}
	return "", errors.Errorf("default GRUB2 target not found")
}

func IsPhysicalEthernet(name string) bool {
	devicePath := filepath.Join(SysClassNet, name, "device")
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		return false
	}
	return true
}

func QueryExtraInfoForEth(name string) (info EthernetExtraInfo, ok bool) {
	info.Physical = IsPhysicalEthernet(name)
	ifMgr, err := DetectIfCfgAndGenerateManager("/", name)
	if err != nil {
		return
	}
	info.IPv4bootProto = ifMgr.GetIPv4BootProto()
	ipv4gateway := ifMgr.GetIPv4Gateway()
	if ipv4gateway != "" {
		info.IPv4gatewayList = append(info.IPv4gatewayList, ipv4gateway)
	}
	info.IPv4dnsList = ifMgr.GetIPv4DNS()
	info.IfCfgPath = ifMgr.IfCfgPath()
	return info, true
}

func StartService(serviceName string) (bool, error) {
	// TODO
	_ = serviceName
	return false, nil
}

func FlushBuffers(_ string) error {
	syscall.Sync()
	return nil
}

func FindMountPoint(path string) (string, error) {
	absPath, err := filepath.Abs(path) // 获取绝对路径
	if err != nil {
		return "", err
	}

	for {
		var stat syscall.Stat_t
		err = syscall.Stat(absPath, &stat)
		if err != nil {
			return "", err
		}

		// 获取父目录的绝对路径
		parentPath := filepath.Dir(absPath)

		// 获取父目录的stat信息
		var parentStat syscall.Stat_t
		err = syscall.Stat(parentPath, &parentStat)
		if err != nil {
			return "", err
		}

		// 如果当前路径与父目录的设备ID不同，则当前路径是挂载点
		if stat.Dev != parentStat.Dev {
			return absPath, nil
		}

		// 如果已经到达根目录，直接返回根目录
		if absPath == parentPath {
			return absPath, nil
		}

		// 逐级向上检查
		absPath = parentPath
	}
}

func IsEncryptedByBitlocker(_ string, _ int64) (bool, error) {
	return false, nil
}

func SystemManufacturer() string {
	out, _ := exec.Command("sh", "-c", "dmidecode -s system-manufacturer").Output()
	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		outStr = "unknown"
	}
	return outStr
}
