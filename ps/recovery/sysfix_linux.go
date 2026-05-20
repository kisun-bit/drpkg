package recovery

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

const (
	InitrdToolDracut          = "dracut"
	InitrdToolUpdateInitramfs = "update-initramfs"
	InitrdToolMkinitrd        = "mkinitrd"
)

const (
	ChipsetQ35    = "q35"
	ChipsetI440fx = "i440fx"
)

var (
	rootDir = "/mnt/sysroot"
)

type linuxSystemFixer struct {
	ctx    context.Context
	opts   *FixerCreateOptions // 恢复参数
	logs   <-chan LogEntry     // 日志缓存通道
	psinfo *info.PsInfo        // 修复虚机的系统信息（已附加离线系统）
	offsys offlineSystem       // 离线系统的私有信息
}

type offlineSystem struct {
	fsList      []fsDevice        // 文件系统设备集合
	fsLastMount map[string]string // 文件系统设备最近一次的挂载点,map的key为fsDevice,val为最近一次挂载点
	devRoot     string            // root设备
	devBoot     string            // boot设备
	devEfi      string            // efi设备
	devVar      string            // var设备
	devSwaps    []string          // swap设备
	chipset     string            // 机器类型（即：主板芯片组模型），可取i440fx、q35等
	devMaps     []DeviceMap       // 设备映射表
	mounts      []string          // 挂载点（顺序：从顶层到最下层）
	root        string            // root挂载点
	initrdTl    string            // initrd生成工具
	kernels     []kernel          // 内核集合
	grubVer     int               // grub版本
	grubCfg     string            // grub配置文件，相对于/
	distro      DistroInfo        // 发行版信息
}

type kernel struct {
	info.LinuxKernel
	KConfigs map[string]string `json:"-"`
}

func NewSysFixer(ctx context.Context, opts *FixerCreateOptions) (fixer SysFixer, err error) {
	logger.Debugf("NewSysFixer: opts:\n%s", extend.Pretty(opts))
	if err = CheckFixerCreateOptions(opts); err != nil {
		return nil, err
	}
	return &linuxSystemFixer{ctx: ctx, opts: opts, logs: make(<-chan LogEntry, 1000)}, nil
}

// Prepare 准备修复环境（挂载/加载离线系统）
func (fixer *linuxSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	if err := fixer.activeLVM(); err != nil {
		return errors.Wrap(err, "active lvm")
	}

	fsDevs, e := enumFilesystem(fixer.opts.OfflineSysDisks)
	if e != nil {
		return errors.Wrap(e, "enum filesystem")
	}
	fixer.offsys.fsList = fsDevs
	logger.Debugf("Prepare: fsList:\n%s", extend.Pretty(fixer.offsys.fsList))

	if err := fixer.detectLastMount(); err != nil {
		return errors.Wrap(err, "detect last mount")
	}

	if err := fixer.cleanDattoSnapshot(); err != nil {
		return errors.Wrap(err, "cleaning datto snapshot")
	}

	if err := fixer.detectSysDevice(); err != nil {
		return errors.Wrap(err, "detect sys device")
	}

	if err := fixer.mountSys(); err != nil {
		return errors.Wrap(err, "mounting offline system")
	}

	if err := fixer.detectKernels(); err != nil {
		return errors.Wrap(err, "detect kernels")
	}

	if err := fixer.detectGrub(); err != nil {
		return errors.Wrap(err, "detect grub")
	}

	if err := fixer.detectDistro(); err != nil {
		return errors.Wrap(err, "detect distro")
	}

	if err := fixer.detectInitrdTool(); err != nil {
		return errors.Wrap(err, "detect initrd tool")
	}

	if err := fixer.detectDeviceMaps(); err != nil {
		return errors.Wrap(err, "detect device uuid")
	}

	fixer.detectChipset()

	return nil
}

// Repair 执行修复流程
func (fixer *linuxSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	if err := fixer.fixFstab(); err != nil {
		return errors.Wrap(err, "fix fstab")
	}

	// TODO

	return errors.New("not implemented")
}

// Cleanup 清理修复环境（卸载/释放资源）
func (fixer *linuxSystemFixer) Cleanup() error {
	logger.Debugf("Cleanup: ++")
	defer logger.Debugf("Cleanup: --")

	if err := fixer.umountSys(); err != nil {
		return errors.Wrap(err, "umount sys")
	}

	return nil
}

// GetLog 获取日志
func (fixer *linuxSystemFixer) GetLog() (LogEntry, bool) {
	select {
	case entry := <-fixer.logs:
		return entry, true
	default:
		return LogEntry{}, false
	}
}

// mountSys 挂载离线系统
func (fixer *linuxSystemFixer) mountSys() error {
	logger.Debugf("mountSys: ++")
	defer logger.Debugf("mountSys: --")

	if fixer.offsys.devRoot == "" {
		return errors.New("root filesystem is empty")
	}

	// 固定离线系统的挂载点
	if e := os.MkdirAll(rootDir, 0755); e != nil {
		return errors.Wrapf(e, "mkdir %s", rootDir)
	}
	_ = Umount(rootDir, true)

	if _, err := Mount(fixer.ctx, fixer.offsys.devRoot, rootDir, false); err != nil {
		return err
	}
	fixer.offsys.root = rootDir
	fixer.offsys.mounts = append(fixer.offsys.mounts, rootDir)

	if fixer.offsys.devBoot != "" {
		// FIXME：项目上发现，未修复的Boot可能影响新的initrd文件生成
		bootMountpoint := filepath.Join(rootDir, "boot")
		if _, err := Mount(fixer.ctx, fixer.offsys.devBoot, bootMountpoint, false); err != nil {
			return err
		}
		fixer.offsys.mounts = append(fixer.offsys.mounts, bootMountpoint)
	}

	if fixer.offsys.devEfi != "" {
		efiMountpoint := filepath.Join(rootDir, "boot", "efi")
		if extend.IsExisted(efiMountpoint) {
			if _, err := Mount(fixer.ctx, fixer.offsys.devEfi, efiMountpoint, false); err != nil {
				return err
			}
			fixer.offsys.mounts = append(fixer.offsys.mounts, efiMountpoint)
		}
	}

	if fixer.offsys.devVar != "" {
		varMountpoint := filepath.Join(rootDir, "var")
		if _, err := Mount(fixer.ctx, fixer.offsys.devVar, varMountpoint, false); err != nil {
			return err
		}
		fixer.offsys.mounts = append(fixer.offsys.mounts, varMountpoint)
	}

	chrootDevPath := filepath.Join(rootDir, "dev")
	chrootProcPath := filepath.Join(rootDir, "proc")
	chrootSysPath := filepath.Join(rootDir, "sys")
	chrootRunPath := filepath.Join(rootDir, "run")

	releatedMounts := map[string]string{
		chrootDevPath:  fmt.Sprintf("mount --bind /dev %s", chrootDevPath),
		chrootProcPath: fmt.Sprintf("mount -t proc procfs %s", chrootProcPath),
		chrootSysPath:  fmt.Sprintf("mount -t sysfs sysfs %s", chrootSysPath),
		chrootRunPath:  fmt.Sprintf("mount --bind /run %s", chrootRunPath),
	}

	// NOTE: 不要使用--rbind和--make-rslave，会造成卸载rootDir时vg资源释放不干净

	for mp, cmdline := range releatedMounts {
		if !extend.IsExisted(mp) {
			_ = os.MkdirAll(mp, 0755)
		}
		if _, _, e := command.Execute(cmdline, command.WithDebug()); e != nil {
			return e
		}

		fixer.offsys.mounts = append(fixer.offsys.mounts, mp)
	}

	fixer.offsys.root = rootDir
	return nil
}

func (fixer *linuxSystemFixer) umountSys() error {
	logger.Debugf("umountSys: ++")
	defer logger.Debugf("umountSys: --")

	for _, mp := range funk.ReverseStrings(fixer.offsys.mounts) {
		if err := Umount(mp, false); err != nil {
			logger.Warnf("umountSys: umount %s failed: %s", mp, err)
			return err
		}
	}

	return nil
}

// activeLVM 激活LVM
func (fixer *linuxSystemFixer) activeLVM() error {
	logger.Debugf("activeLVM ++")
	defer logger.Debugf("activeLVM --")

	if _, _, e := command.Execute("vgchange -an", command.WithDebug()); e != nil {
		return e
	}
	if _, _, e := command.Execute("rm -f /etc/lvm/devices/system.devices", command.WithDebug()); e != nil {
		return e
	}
	if _, _, e := command.Execute("pvscan", command.WithDebug()); e != nil {
		return e
	}
	if _, _, e := command.Execute("vgscan", command.WithDebug()); e != nil {
		return e
	}
	if _, _, e := command.Execute("vgchange -ay", command.WithDebug()); e != nil {
		return e
	}
	return nil
}

// detectSysDevice 探测系统根环境
func (fixer *linuxSystemFixer) detectSysDevice() error {
	logger.Debugf("detectSysDevice ++")
	defer logger.Debugf("detectSysDevice --")

	if len(fixer.offsys.fsList) == 0 {
		fsDevs, err := enumFilesystem(fixer.opts.OfflineSysDisks)
		if err != nil {
			return err
		}
		fixer.offsys.fsList = fsDevs
	}

	for _, dev := range fixer.offsys.fsList {
		if dev.fsType == "swap" {
			continue
		}
		switch {
		case IsRootDevice(fixer.ctx, dev.device):
			fixer.offsys.devRoot = dev.device
		case IsBootDevice(fixer.ctx, dev.device):
			fixer.offsys.devBoot = dev.device
		case IsEfiDevice(fixer.ctx, dev.device):
			fixer.offsys.devEfi = dev.device
		case IsVarDevice(fixer.ctx, dev.device):
			fixer.offsys.devVar = dev.device
		}
	}

	logger.Debugf("detectSysDevice: root=`%s`, boot=`%s`, efi=`%s`, var=`%s`",
		fixer.offsys.devRoot, fixer.offsys.devBoot, fixer.offsys.devEfi, fixer.offsys.devVar)

	fixer.offsys.devSwaps = make([]string, 0)
	for _, dev := range fixer.offsys.fsList {
		if dev.fsType == "swap" {
			fixer.offsys.devSwaps = append(fixer.offsys.devSwaps, dev.device)
			continue
		}
	}
	logger.Debugf("detectSysDevice: swap=`%s`", strings.Join(fixer.offsys.devSwaps, ","))

	if fixer.offsys.devRoot == "" {
		return errors.Errorf("no root filesystem device detected from candidates: %v", fixer.offsys.fsList)
	}

	return nil
}

type initrdTool struct {
	name        string
	featureFile string
	cmd         string
}

var initrdTools = []initrdTool{
	{
		name:        InitrdToolDracut,
		featureFile: "/etc/dracut.conf",
		cmd:         InitrdToolDracut,
	},
	{
		name:        InitrdToolUpdateInitramfs,
		featureFile: "/etc/initramfs-tools/update-initramfs.conf",
		cmd:         InitrdToolUpdateInitramfs,
	},
	{
		name: InitrdToolMkinitrd,
		cmd:  InitrdToolMkinitrd,
	},
}

// detectInitrdTool 探测initrd生成工具
func (fixer *linuxSystemFixer) detectInitrdTool() error {
	logger.Debugf("detectInitrdTool ++")
	defer logger.Debugf("detectInitrdTool --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	for _, tool := range initrdTools {
		// 1. feature file 检测（如果有）
		if tool.featureFile != "" {
			path := filepath.Join(fixer.offsys.root, tool.featureFile)
			if !extend.IsExisted(path) {
				continue
			}
		}

		// 2. 命令探测
		rc, _, _ := fixer.executeWithChroot(tool.cmd + " --e1y728ety172eg")

		// 3. 127 = not found
		if rc == 127 {
			logger.Debugf("initrd tool %s not found (rc=127)", tool.name)
			continue
		}

		// 4. 成功命中
		logger.Debugf("detectInitrdTool: initrd tool detected: %s", tool.name)
		fixer.offsys.initrdTl = tool.name

		return nil
	}

	return errors.New("no initramfs tool detected")
}

// detectLastMount 探测文件系统最近一次挂载点
func (fixer *linuxSystemFixer) detectLastMount() error {
	logger.Debugf("detectLastMount ++")
	defer logger.Debugf("detectLastMount --")

	fixer.offsys.fsLastMount = make(map[string]string)

	for _, fdev := range fixer.offsys.fsList {
		ft, _ := DetectFSTypeByBlkid(fdev.device)

		switch ft {
		case "ext2", "ext3", "ext4":

			logger.Debugf(
				"detectLastMount: ext filesystem detected: %s, start to query last mountpoint",
				fdev,
			)

			_, output, err := command.Execute("dumpe2fs -h " + fdev.device)
			if err != nil {
				logger.Warnf(
					"detectLastMount: dumpe2fs failed for %s: %v",
					fdev,
					err,
				)
				continue
			}

			var (
				volumeName string
				lastMount  string
			)

			scanner := bufio.NewScanner(strings.NewReader(output))

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())

				// Filesystem volume name:   /boot
				if strings.HasPrefix(line, "Filesystem volume name:") {
					v := strings.TrimSpace(
						strings.TrimPrefix(
							line,
							"Filesystem volume name:",
						),
					)

					if strings.HasPrefix(v, "/") &&
						v != "<none>" &&
						v != "<not available>" {
						volumeName = v
					}

					continue
				}

				// Last mounted on:          /
				if strings.HasPrefix(line, "Last mounted on:") {
					v := strings.TrimSpace(
						strings.TrimPrefix(
							line,
							"Last mounted on:",
						),
					)

					if strings.HasPrefix(v, "/") &&
						v != "<none>" &&
						v != "<not available>" {
						lastMount = v
					}

					continue
				}
			}

			if err = scanner.Err(); err != nil {
				logger.Warnf(
					"detectLastMount: scanner failed for %s: %v",
					fdev,
					err,
				)
			}

			// 原则：
			// 优先 Filesystem volume name
			// 其次 Last mounted on
			if volumeName != "" {
				fixer.offsys.fsLastMount[fdev.device] = volumeName

				logger.Debugf(
					"detectLastMount: %s -> %s (from `volume name`)",
					fdev,
					volumeName,
				)
			} else if lastMount != "" {
				fixer.offsys.fsLastMount[fdev.device] = lastMount

				logger.Debugf(
					"detectLastMount: %s -> %s (from `last mounted on`)",
					fdev,
					lastMount,
				)
			} else {
				logger.Debugf(
					"detectLastMount: %s no valid mountpoint found",
					fdev,
				)
			}
		}
	}

	return nil
}

// detectDeviceMaps 探测设备映射表
func (fixer *linuxSystemFixer) detectDeviceMaps() error {
	logger.Debugf("detectDeviceMaps ++")
	defer logger.Debugf("detectDeviceMaps --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if len(fixer.opts.RecoveryParam.SourceDeviceMap) != 0 {
		logger.Debugf("detectDeviceMaps: source device map detected")
		fixer.offsys.devMaps = fixer.opts.RecoveryParam.SourceDeviceMap
		return nil
	}

	fixer.offsys.devMaps = make([]DeviceMap, 0)

	//
	// 基于文件系统最近一次挂载点进行补充
	//

	for fsDev, lastMount := range fixer.offsys.fsLastMount {
		uuidStr, _ := DetectUuidByBlkid(fsDev)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: lastMount,
				UUID:       uuidStr,
			},
		)
	}

	//
	// 基于探测的系统卷设备进行补充
	//

	if fixer.offsys.devRoot != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devRoot)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devBoot != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devBoot)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/boot",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devEfi != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devEfi)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/boot/efi",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devVar != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devVar)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/var",
				UUID:       uuidStr,
			},
		)
	}

	//
	// 基于blkid的缓存文件进行探测
	//

	//
	// /run/blkid/blkid.tab：新版 Linux（systemd / util-linux 新版本），处于内存文件系统不可作为探测文件
	// /etc/blkid.tab：老系统，可作为探测文件
	// /dev/.blkid.tab：老系统，处于内存文件系统不可作为探测文件
	//

	blkidCacheFile := filepath.Join(fixer.offsys.root, "etc/blkid.tab")
	if extend.IsExisted(blkidCacheFile) {
		// <device DEVNO="0xfd01" TIME="1779244003" PRI="45" TYPE="swap">/dev/mapper/VolGroup00-LogVol01</device>
		// <device DEVNO="0xfd00" TIME="1779244003" PRI="45" UUID="c007fd05-8d6d-47ee-bf30-7bffb2fc2896" TYPE="ext3">/dev/mapper/VolGroup00-LogVol00</device>
		// <device DEVNO="0x0801" TIME="1779244003" LABEL="/boot" UUID="60a80797-2810-48e7-93ed-599c3648c5d7" TYPE="ext3" SEC_TYPE="ext2">/dev/sda1</device>
		// <device DEVNO="0x0300" TIME="1631714685" LABEL="VMware Tools" TYPE="iso9660">/dev/hda</device>
		// <device DEVNO="0xfd00" TIME="1779244003" UUID="c007fd05-8d6d-47ee-bf30-7bffb2fc2896" TYPE="ext3">/dev/VolGroup00/LogVol00</device>
		// <device DEVNO="0xfd01" TIME="1779244003" TYPE="swap">/dev/VolGroup00/LogVol01</device>
		// <device DEVNO="0x0300" TIME="1669787575" LABEL="CentOS_5.6_Final" TYPE="iso9660">/dev/cdrom</device>
		blkidContent, err := os.ReadFile(blkidCacheFile)
		if err != nil {
			logger.Warnf("detectDeviceMaps: failed to read blkid.tab: %v", err)
		}
		logger.Debugf("detectDeviceMaps: blkid.tab: %s", string(blkidContent))
		blkidCacheRe := regexp.MustCompile(`UUID="([0-9a-fA-F-]+)".*?>(/dev/[^<]+)</device>`)
		matches := blkidCacheRe.FindAllStringSubmatch(string(blkidContent), -1)
		for _, m := range matches {
			uuid := m[1]
			dev := m[2]
			fixer.offsys.devMaps = append(fixer.offsys.devMaps,
				DeviceMap{
					Origin:     dev,
					Mountpoint: "",
					UUID:       uuid,
				})
			logger.Debugf("detectDeviceMaps: Detecting blkid.tab. uuid=%s device=%s ", uuid, dev)
		}
	}

	//
	// 基于系统启动日志进行补充
	//

	// 文件系统：btrfs
	// 命令：grep -i "btrfs" /var/log/messages* | grep fsid
	// 结果：
	// 2025-09-23T17:05:40.910399+08:00 localhost kernel: [    4.514460] BTRFS: device fsid b3334026-ccb6-4c25-82f4-e94e72dab24e devid 1 transid 63 /dev/dm-1
	// 2026-05-12T14:27:56.262124+08:00 localhost kernel: [    6.154912] BTRFS info (device dm-0): device fsid b3334026-ccb6-4c25-82f4-e94e72dab24e devid 1 moved old:/dev/dm-0 new:/dev/mapper/system-root

	syslogDetectBtrfsCmd := fmt.Sprintf(`grep -i "btrfs" %s/var/log/messages* | grep fsid`, fixer.offsys.root)
	_, o, _ := command.Execute(syslogDetectBtrfsCmd)
	btrfsUuids := make([]string, 0)
	btrfsUuidRe := regexp.MustCompile(`fsid\s+([a-fA-F0-9-]+).*?(/dev/\S+)`)
	for _, line := range strings.Split(o, "\n") {
		matches := btrfsUuidRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			fsid := matches[1]
			dev := matches[2]
			if funk.InStrings(btrfsUuids, fsid) {
				continue
			}
			fixer.offsys.devMaps = append(fixer.offsys.devMaps,
				DeviceMap{
					Origin:     dev,
					Mountpoint: "",
					UUID:       fsid,
				})
			btrfsUuids = append(btrfsUuids, fsid)
			logger.Debugf("detectDeviceMaps: Detecting btrfs by syslog. line=`%s` uuid=`%s` device=`%s`", line, fsid, dev)
		}
	}

	// 文件系统：xfs
	// 命令：grep -i "kernel: xfs" /var/log/* | grep -i mounting
	// 结果：
	// /var/log/messages-20260517:May  9 16:04:22 node1 kernel: XFS (dm-0): Mounting V5 Filesystem 48153c2d-a7a3-4f37-8998-dfe87705ebcb
	// /var/log/messages-20260517:May  9 16:04:24 node1 kernel: XFS (sda2): Mounting V5 Filesystem 9d6bbc00-4ff1-42fd-8b34-5ffad8f2fbc8

	syslogDetectXfsCmd := fmt.Sprintf(
		`grep -i "kernel: xfs" %s/var/log/* | grep -i mounting`,
		fixer.offsys.root,
	)

	_, o, _ = command.Execute(syslogDetectXfsCmd)

	xfsUuids := make([]string, 0)

	// 匹配：XFS (dm-0): Mounting V5 Filesystem UUID
	xfsUuidRe := regexp.MustCompile(
		`XFS\s+\(([^)]+)\):.*?Filesystem\s+([a-fA-F0-9-]+)`,
	)

	for _, line := range strings.Split(o, "\n") {
		matches := xfsUuidRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			dev := matches[1]
			uuid := matches[2]

			// 补齐 /dev/
			if !strings.HasPrefix(dev, "/dev/") {
				dev = "/dev/" + dev
			}

			if funk.InStrings(xfsUuids, uuid) {
				continue
			}

			fixer.offsys.devMaps = append(
				fixer.offsys.devMaps,
				DeviceMap{
					Origin:     dev,
					Mountpoint: "",
					UUID:       uuid,
				},
			)

			xfsUuids = append(xfsUuids, uuid)

			logger.Debugf(
				"detectDeviceMaps: Detecting xfs by syslog. line=`%s` uuid=`%s` device=`%s`",
				line,
				uuid,
				dev,
			)
		}
	}

	logger.Debugf("detectDeviceMaps: Device map:\n%s", extend.Pretty(fixer.offsys.devMaps))

	return nil
}

// detectChipset 探测主板芯片组的兼容类型
func (fixer *linuxSystemFixer) detectChipset() {
	logger.Debugf("detectChipset ++")
	defer logger.Debugf("detectChipset --")

	// virt-v2v 的 machine type 原则：
	//
	// "以 2007 为分界时间线，早于 2007 发布的 Linux 使用 i440fx，
	// 2007 及之后发布的 Linux 使用 q35。"

	chipset := ChipsetQ35

	switch fixer.offsys.distro.ID {
	case "fedora":
		break
	case "rhel", "centos", "circle", "scientificlinux", "redhat-based", "oraclelinux", "rocky":
		//if fixer.offlineSystem.distro.Major <= 4 {
		//	chipset = ChipsetI440fx
		//}
		if fixer.offsys.distro.Major <= 6 {
			chipset = ChipsetI440fx
		}
	case "sles", "suse-based", "opensuse":
		//if fixer.offlineSystem.distro.Major <= 10 {
		//	chipset = ChipsetI440fx
		//}
		if fixer.offsys.distro.Major <= 12 {
			chipset = ChipsetI440fx
		}
	case "debian", "ubuntu", "linuxmint", "kaillinux":
		if fixer.offsys.distro.Major <= 4 {
			chipset = ChipsetI440fx
		}
	}

	logger.Debugf("detectChipset: chipset detected: %s", chipset)

	fixer.offsys.chipset = chipset

	// FIXME:
	// 但在实际测试中发现，SUSE11 SP4（kernel 3.0.101）是一个例外：
	//
	// 当 initrd 已包含 VirtIO 驱动：
	//   virtio
	//   virtio_ring
	//   virtio_pci
	//   virtio_blk
	//   virtio_scsi
	//   virtio_net
	//
	// 启动兼容性如下：
	//
	// 1. root disk 使用 virtio-blk / virtio-scsi：
	//    - i440fx：可正常启动
	//    - q35：无法启动
	//
	//    原因：
	//    SUSE11 SP4 的 virtio 驱动较旧，对 q35 (PCIe +
	//    modern virtio) 兼容性较差，表现为：
	//      - lspci 可见 virtio-scsi controller
	//      - virtio_scsi 驱动无法 bind controller
	//      - root disk 无法枚举
	//      - initrd 无法找到 root device
	//
	// 2. root disk 使用 IDE / SATA（AHCI）：
	//    - i440fx：可正常启动
	//    - q35：可正常启动
	//
	// 因此：
	// 对于 SUSE11 SP4，如果 root disk bus 为
	// virtio / virtio-scsi，建议强制使用 i440fx。
	// 若使用 IDE/SATA，则可允许 q35。
}

// detectKernels 探测离线系统的内核
func (fixer *linuxSystemFixer) detectKernels() error {
	logger.Debugf("detectKernels: ++")
	defer logger.Debugf("detectKernels: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	ks, err := info.QueryLinuxKernels(fixer.offsys.root)
	if err != nil {
		return err
	}

	// 过滤掉非启动内核
	ks2 := make([]kernel, 0)
	for _, k := range ks {
		if !k.Bootable {
			continue
		}

		k2 := kernel{}
		k2.LinuxKernel = k
		k2.KConfigs = make(map[string]string)

		if k.Config != "" {
			cfgs, ec := Kconfig(filepath.Join(fixer.offsys.root, "boot", k.Config))
			if ec != nil {
				logger.Warnf("detectKernels: failed to parse kernel configs for %s: %v", k.Name, ec)
			} else {
				k2.KConfigs = cfgs
			}
		}

		ks2 = append(ks2, k2)
	}

	fixer.offsys.kernels = ks2
	logger.Debugf("detectKernels: bootable kernels:\n%s", extend.Pretty(ks2))

	return nil
}

// linuxSystemFixer 探测离线系统的grub
func (fixer *linuxSystemFixer) detectGrub() error {
	logger.Debugf("detectGrub: ++")
	defer logger.Debugf("detectGrub: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	ver, cfg := DetectGrub(fixer.offsys.root)
	if ver == -1 {
		return errors.New("grub not found")
	}

	fixer.offsys.grubVer, fixer.offsys.grubCfg = ver, cfg
	logger.Debugf("detectGrub: version=%d config=%s", ver, cfg)

	return nil
}

// linuxSystemFixer 探测Linux发行版信息
func (fixer *linuxSystemFixer) detectDistro() error {
	logger.Debugf("detectDistro: ++")
	defer logger.Debugf("detectDistro: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	distro, err := DetectDistro(fixer.offsys.root)
	if err != nil {
		return err
	}

	fixer.offsys.distro = *distro
	logger.Debugf("detectDistro: distro=\n%s", extend.Pretty(distro))

	return nil
}

// executeWithChroot 在chroot环境执行命令
func (fixer *linuxSystemFixer) executeWithChroot(cmdline string) (exitcode int, output string, err error) {
	if fixer.offsys.root == "" {
		return -1, "", ErrorRootEnvNotMounted
	}

	chrootCmdline := fmt.Sprintf(
		`chroot %s /bin/bash -c "export PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin:$PATH; %s"`,
		fixer.offsys.root, cmdline)

	return command.Execute(chrootCmdline, command.WithDebug())
}

// cleanDattoSnapshot 清理datto(/elastio)快照
func (fixer *linuxSystemFixer) cleanDattoSnapshot() error {
	logger.Debugf("cleanDattoSnapshot: ++")
	defer logger.Debugf("cleanDattoSnapshot: --")

	tmpMp, err := os.MkdirTemp("", "cleanDattoCow-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = Umount(tmpMp, false)
		_ = os.RemoveAll(tmpMp)
	}()

	logger.Debugf("cleanDattoSnapshot: tmpMp=%s", tmpMp)

	candDirs := []string{
		filepath.Join(tmpMp, "lost+found"),
		filepath.Join(tmpMp, ".runstorsnap"),
	}

	for _, dev := range fixer.offsys.fsList {
		if dev.fsType == "swap" {
			continue
		}
		_ = Umount(tmpMp, false)

		_, em := Mount(fixer.ctx, dev.device, tmpMp, false)
		if em != nil {
			logger.Warnf("cleanDattoSnapshot: Mount %s failed: %v", dev, em)
			continue
		}

		logger.Debugf("cleanDattoSnapshot: device=%s", dev)

		for _, cand := range candDirs {
			foundCmdline := fmt.Sprintf("find %s -name *.cow", cand)
			_, o, ef := command.Execute(foundCmdline)
			if ef != nil {
				continue
			}
			for _, line := range strings.Split(o, "\n") {
				cowPath := strings.TrimSpace(line)
				if cowPath == "" {
					continue
				}
				rmCmdline := fmt.Sprintf("chattr -i %s; rm -f %s", cowPath, cowPath)
				_, _, er := command.Execute(rmCmdline)
				if er != nil {
					logger.Warnf("cleanDattoSnapshot: rm -f %s failed: %v", cowPath, er)
					continue
				}
				logger.Debugf("cleanDattoSnapshot: rm -f %s", cowPath)
			}
		}
	}

	return nil
}

// fixFstab 修复fstab
func (fixer *linuxSystemFixer) fixFstab() error {
	logger.Debugf("fixFstab: ++")
	defer logger.Debugf("fixFstab: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	fstabPath := filepath.Join(fixer.offsys.root, "etc/fstab")

	//
	// 编辑
	// fstab结构如下：
	// <file system> <mount point>   <type>  <options>       <dump>  <pass>
	//

	contentBin, err := os.ReadFile(fstabPath)
	if err != nil {
		return err
	}
	content := string(contentBin)
	logger.Debugf("fixFstab: history configuration:\n%s", content)

	newContentLines := make([]string, 0)
	changed := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			newContentLines = append(newContentLines, line)
			continue
		}
		items := strings.Fields(line)
		if len(items) < 6 {
			newContentLines = append(newContentLines, line)
			continue
		}

		// 避免迁移后设备ID出现变化，
		// 一般而言稳定性如下：
		// 安全：by-partuuid、by-uuid、by-label、tmpfs/proc/sysfs/devpts、lv

		if strings.HasPrefix(items[0], "/dev/disk/by-partuuid") ||
			strings.HasPrefix(items[0], "/dev/disk/by-uuid") ||
			strings.HasPrefix(items[0], "/dev/disk/by-label") ||
			strings.HasPrefix(strings.ToUpper(items[0]), "PARTUUID=") ||
			strings.HasPrefix(strings.ToUpper(items[0]), "UUID=") ||
			strings.HasPrefix(strings.ToUpper(items[0]), "LABEL=") ||
			funk.InStrings([]string{"tmpfs", "devpts", "sysfs", "proc", "debugfs"}, items[2]) ||
			strings.HasPrefix(items[0], "/dev/mapper") {
			newContentLines = append(newContentLines, line)
			continue
		}

		if strings.HasPrefix(items[0], "/dev/") {
			r, _, e := command.Execute("lvdisplay " + items[0])
			if e == nil || r == 0 {
				newContentLines = append(newContentLines, line)
				continue
			}
		}

		uuid := ""

		// 修复swap
		if len(fixer.offsys.devSwaps) == 1 && items[2] == "swap" {
			logger.Debugf("fixFstab: Prepare to fix `%s`", line)
			swapDev := fixer.offsys.devSwaps[0]
			uuid, _ = DetectUuidByBlkid(swapDev)
		}

		// 基于匹配`挂载路径`进行`设备`的修复
		if uuid == "" {
			for _, dm := range fixer.offsys.devMaps {
				if dm.Mountpoint == items[1] && strings.HasPrefix(dm.Mountpoint, "/") {
					uuid = dm.UUID
					break
				}
			}
		}

		// 基于匹配`设备`进行`设备`的修复
		if uuid == "" {
			for _, dm := range fixer.offsys.devMaps {
				if dm.Origin == items[0] && dm.UUID != "" {
					uuid = dm.UUID
					break
				}
			}
		}

		if uuid != "" {
			logger.Debugf("fixFstab: Old configuration: `%s`", line)
			items[0] = fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)
			newLine := strings.Join(items, "    ")
			logger.Debugf("fixFstab: New configuration: `%s`", newLine)
			newContentLines = append(newContentLines, "# "+line)
			newContentLines = append(newContentLines, newLine)
			changed = true
			continue
		}

		// TODO 抛出警告，提示可能存在恢复后系统无法启动的情况
		logger.Warnf("fixFstab: Warn-Config: `%s`", line)
		newContentLines = append(newContentLines, line)
	}

	if !changed {
		logger.Debugf("fixFstab: No changes detected")
		return nil
	}

	// 备份历史配置
	fstabBkPath := filepath.Join(fixer.offsys.root, fmt.Sprintf("etc/fstab.bk.%d", time.Now().Unix()))
	_, _, e := command.Execute("cp " + fstabPath + " " + fstabBkPath)
	if e != nil {
		return errors.Wrap(e, "backup /etc/fstab")
	}
	logger.Debugf("fixFstab: copy `etc/fstab` to `%s`", fstabBkPath)

	newContent := strings.Join(newContentLines, "\n")
	logger.Debugf("fixFstab: new configuration:\n%s", newContent)

	return os.WriteFile(fstabPath, []byte(newContent), 0644)
}

// fixEfiBootConf 修复Efi的启动配置
func (fixer *linuxSystemFixer) fixEfiBootConf() error {
	logger.Debugf("fixEfiBootConf: ++")
	defer logger.Debugf("fixEfiBootConf: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	// TODO

	return errors.New("fixEfiBootConf: not implemented yet")
}

// fixEfiFirmware 修复Efi固件
func (fixer *linuxSystemFixer) fixEfiFirmware() error {
	logger.Debugf("fixEfiFirmware: ++")
	defer logger.Debugf("fixEfiFirmware: --")

	// TODO

	return errors.New("fixEfiFirmware: not implemented yet")
}

func (fixer *linuxSystemFixer) disableSeLinux() error {
	logger.Debugf("disableSeLinux: ++")
	defer logger.Debugf("disableSeLinux: --")

	// 1. 修改 /etc/selinux/config
	configPath := filepath.Join(rootDir, "etc", "selinux", "config")

	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return errors.Wrapf(err, "read selinux config failed")
		}

		content := string(data)

		// 替换 SELINUX=
		lines := strings.Split(content, "\n")
		found := false

		for i, line := range lines {
			trimLine := strings.TrimSpace(line)

			// 跳过注释
			if strings.HasPrefix(trimLine, "#") {
				continue
			}

			if strings.HasPrefix(trimLine, "SELINUX=") {
				lines[i] = "SELINUX=disabled"
				found = true
			}
		}

		if !found {
			lines = append(lines, "SELINUX=disabled")
		}

		newContent := strings.Join(lines, "\n")

		if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
			return fmt.Errorf("write selinux config failed: %w", err)
		}

		logger.Infof("DisableSELinux: updated %s", configPath)
	}

	// 2. 清理 grub cmdline 中的 selinux 参数

	removeKernelArgs := func(content string) string {
		reList := []*regexp.Regexp{
			regexp.MustCompile(`\s+selinux=\S+`),
			regexp.MustCompile(`\s+enforcing=\S+`),
		}

		for _, re := range reList {
			content = re.ReplaceAllString(content, "")
		}

		return content
	}

	if _, err := os.Stat(fixer.offsys.grubCfg); err != nil {
		return err
	}

	data, err := os.ReadFile(fixer.offsys.grubCfg)
	if err != nil {
		logger.Warnf("DisableSELinux: read grub file failed: %s err=%v",
			fixer.offsys.grubCfg, err)
		return nil
	}

	newContent := removeKernelArgs(string(data))

	if err = os.WriteFile(fixer.offsys.grubCfg, []byte(newContent), 0644); err != nil {
		logger.Warnf("DisableSELinux: write grub file failed: %s err=%v",
			fixer.offsys.grubCfg, err)
		return nil
	}

	logger.Infof("DisableSELinux: updated grub file %s", fixer.offsys.grubCfg)

	// 3. 创建 autorelabel 标记（迁移后更安全）
	autorelabelPath := filepath.Join(rootDir, ".autorelabel")

	if err = os.WriteFile(autorelabelPath, []byte{}, 0644); err != nil {
		logger.Warnf("DisableSELinux: create .autorelabel failed: %v", err)
	} else {
		logger.Infof("DisableSELinux: created %s", autorelabelPath)
	}

	return nil
}

func (fixer *linuxSystemFixer) disableMultipathModule() error {
	logger.Debugf("disableMultipathModule: ++")
	defer logger.Debugf("disableMultipathModule: --")

	// TODO

	return errors.New("disableMultipathModule: not implemented yet")
}

// fixGrub 修复Grub
func (fixer *linuxSystemFixer) fixGrub() error {
	logger.Debugf("fixGrub: ++")
	defer logger.Debugf("fixGrub: --")

	// 参考：https://blog.csdn.net/weixin_39833509/article/details/115633386

	// TODO

	return errors.New("fixGrub: not implemented yet")
}

type fsDevice struct {
	device string
	fsType string
}

func enumFilesystem(offline []string) ([]fsDevice, error) {
	logger.Debugf("enumFilesystem: ++")
	defer logger.Debugf("enumFilesystem: --")

	logger.Debugf("enumFilesystem: offline = %s", extend.Pretty(offline))

	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return nil, err
	}

	logger.Debugf("enumFilesystem: psinfo=\n%s", psinfo.Pretty())

	slaveSet := make(map[string]struct{})
	addSlaves := func(slaves []string) {
		for _, s := range slaves {
			slaveSet[s] = struct{}{}
		}
	}

	for _, mp := range psinfo.Private.Linux.Multipath {
		addSlaves(mp.Slaves)
	}
	for _, rd := range psinfo.Private.Linux.Raid {
		addSlaves(rd.Slaves)
	}

	var fsDevList []string

	// 统一处理函数
	handleDevice := func(device string, dt info.DiskTable) {
		switch dt.Type {
		case table.TableTypeMBR, table.TableTypeGPT:
			for _, p := range dt.Partitions {
				if isInvalidPartition(p.Type) {
					continue
				}
				fsDevList = append(fsDevList, p.Device)
			}
		default:
			fsDevList = append(fsDevList, device)
		}
	}

	// 1. 普通磁盘
	for _, d := range psinfo.Public.Disks {
		if !funk.InStrings(offline, d.Device) {
			continue
		}
		if _, ok := slaveSet[d.Device]; ok {
			continue
		}
		handleDevice(d.Device, d.Table)
	}

	// 2. multipath
	for _, mp := range psinfo.Private.Linux.Multipath {
		if !allInOffline(mp.Slaves, offline) {
			continue
		}
		handleDevice(mp.Device, mp.Table)
	}

	// 3. raid
	for _, rd := range psinfo.Private.Linux.Raid {
		if !allInOffline(rd.Slaves, offline) {
			continue
		}
		handleDevice(rd.Device, rd.Table)
	}

	// 4. lvm
	for _, vg := range psinfo.Private.Linux.LVM.VGList {
		for _, lv := range vg.LVList {
			// 非标准卷
			if len(lv.Segments) == 0 {
				continue
			}
			lvDisks := make([]string, 0)
			for _, seg := range lv.Segments {
				lvDisks = append(lvDisks, seg.Device)
			}
			if !allInOffline(lvDisks, offline) {
				continue
			}
			fsDevList = append(fsDevList, lv.Device)
		}
	}

	fsDevList = funk.UniqString(fsDevList)

	// 过滤文件系统类型
	var devs []fsDevice
	for _, dev := range fsDevList {
		fsStr, _ := DetectFSTypeByBlkid(dev)
		if funk.InStrings(MountSupportedFsTypes, fsStr) || fsStr == "swap" {
			devs = append(devs, fsDevice{dev, fsStr})
		}
	}

	return devs, nil
}

func isInvalidPartition(ptype string) bool {
	return funk.InStrings(table.MBRExtendPartTypes, ptype) ||
		ptype == table.GPT_LINUX_LVM ||
		ptype == table.MBR_LINUX_LVM_PARTITION
}

func allInOffline(slaves []string, offline []string) bool {
	if len(slaves) == 0 {
		return false
	}
	for _, s := range slaves {
		if !funk.InStrings(offline, s) {
			return false
		}
	}
	return true
}
