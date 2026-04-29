package recovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

type linuxSystemFixer struct {
	ctx            context.Context
	opts           *FixerCreateOptions
	logs           <-chan LogEntry
	psinfo         *info.PsInfo
	fsDevices      []string
	sysDevRoot     string
	sysDevBoot     string
	SysDevEfi      string
	rootMountPoint string
	initrdTool     string
	kernels        []info.LinuxKernel
	grubVer        int
	grubCfg        string // 相对于/
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

	fsDevs, e := enumFsDevice(fixer.opts.OfflineSysDisks)
	if e != nil {
		return errors.Wrap(e, "enum filesystem")
	}
	fixer.fsDevices = fsDevs
	logger.Debugf("Prepare: fsDevices:\n%s", extend.Pretty(fixer.fsDevices))

	if err := fixer.cleanDattoSnapshot(); err != nil {
		return errors.Wrap(err, "cleaning datto snapshot")
	}

	if err := fixer.detectSysDevice(); err != nil {
		return errors.Wrap(err, "detect sys device")
	}

	if err := fixer.mountSys(); err != nil {
		return errors.Wrap(err, "mounting offline system")
	}

	if err := fixer.detectInitrdTool(); err != nil {
		return errors.Wrap(err, "detect initrd tool")
	}

	if err := fixer.detectKernels(); err != nil {
		return errors.Wrap(err, "detect kernels")
	}

	if err := fixer.detectGrub(); err != nil {
		return errors.Wrap(err, "detect grub")
	}

	return nil
}

// Repair 执行修复流程
func (fixer *linuxSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	// TODO

	return errors.New("not implemented")
}

// Cleanup 清理修复环境（卸载/释放资源）
func (fixer *linuxSystemFixer) Cleanup() error {
	// TODO
	return errors.New("implement me")
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

	if fixer.sysDevRoot == "" {
		return errors.New("root filesystem is empty")
	}

	//mountpoint, err := os.MkdirTemp("", "mountSys-*")
	//if err != nil {
	//	return err
	//}

	// 固定离线系统的挂载点
	mountpoint := filepath.Join("/mnt/offline")
	_ = os.MkdirAll(mountpoint, 0755)

	if _, err := Mount(fixer.ctx, fixer.sysDevRoot, mountpoint, false); err != nil {
		return err
	}

	if fixer.sysDevBoot != "" {
		// FIXME：项目上发现，未修复的Boot可能影响新的initrd文件生成
		bootMountpoint := filepath.Join(mountpoint, "boot")
		if _, err := Mount(fixer.ctx, bootMountpoint, mountpoint, false); err != nil {
			return err
		}
	}

	if fixer.SysDevEfi != "" {
		efiMountpoint := filepath.Join(mountpoint, "boot", "efi")
		if extend.IsExisted(efiMountpoint) {
			if _, err := Mount(fixer.ctx, efiMountpoint, mountpoint, false); err != nil {
				return err
			}
		}
	}

	chrootDevPath := filepath.Join(mountpoint, "dev")
	chrootDevPtsPath := filepath.Join(chrootDevPath, "pts")
	chrootProcPath := filepath.Join(mountpoint, "proc")
	chrootSysPath := filepath.Join(mountpoint, "sys")

	if _, _, err := command.Execute(fmt.Sprintf("mount --rbind /dev %s", chrootDevPath)); err != nil {
		return err
	}
	if _, _, err := command.Execute(fmt.Sprintf("mount --make-rslave %s", chrootDevPath)); err != nil {
		return err
	}
	if _, _, err := command.Execute(fmt.Sprintf("mount -t proc proc %s", chrootProcPath)); err != nil {
		return err
	}
	if _, _, err := command.Execute(fmt.Sprintf("mount -t sysfs sys %s", chrootSysPath)); err != nil {
		return err
	}

	// 可选
	if _, _, err := command.Execute(fmt.Sprintf("mount --rbind /dev/pts %s", chrootDevPtsPath)); err != nil {
		return err
	}

	fixer.rootMountPoint = mountpoint
	return nil
}

// activeLVM 激活LVM
func (fixer *linuxSystemFixer) activeLVM() error {
	logger.Debugf("activeLVM ++")
	defer logger.Debugf("activeLVM --")

	_, _, e := command.Execute("vgchange -an", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("rm -f /etc/lvm/devices/system.devices", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("pvscan", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("vgscan", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("vgchange -ay", command.WithDebug())
	if e != nil {
		return e
	}
	return nil
}

// detectSysDevice 探测系统根环境
func (fixer *linuxSystemFixer) detectSysDevice() error {
	logger.Debugf("detectSysDevice ++")
	defer logger.Debugf("detectSysDevice --")

	if len(fixer.fsDevices) == 0 {
		fsDevs, err := enumFsDevice(fixer.opts.OfflineSysDisks)
		if err != nil {
			return err
		}
		fixer.fsDevices = fsDevs
	}

	for _, dev := range fixer.fsDevices {
		switch {
		case IsRootDevice(fixer.ctx, dev):
			fixer.sysDevRoot = dev
		case IsBootDevice(fixer.ctx, dev):
			fixer.sysDevBoot = dev
		case IsEfiDevice(fixer.ctx, dev):
			fixer.SysDevEfi = dev
		}
	}

	logger.Debugf("detectSysDevice: root=`%s`, boot=`%s`, efi=`%s`",
		fixer.sysDevRoot, fixer.sysDevBoot, fixer.SysDevEfi)

	if fixer.sysDevRoot == "" {
		return errors.Errorf("no root filesystem device detected from candidates: %v", fixer.fsDevices)
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

	if fixer.rootMountPoint == "" {
		return ErrorRootEnvNotMounted
	}

	for _, tool := range initrdTools {
		// 1. feature file 检测（如果有）
		if tool.featureFile != "" {
			path := filepath.Join(fixer.rootMountPoint, tool.featureFile)
			if !extend.IsExisted(path) {
				continue
			}
		}

		// 2. 命令探测
		rc, stdout, err := fixer.executeWithChroot(tool.cmd)
		if err != nil {
			logger.Debugf("initrd tool %s exec error: %v", tool.name, err)
			continue
		}

		// 3. 127 = not found
		if rc == 127 {
			logger.Debugf("initrd tool %s not found (rc=127)", tool.name)
			continue
		}

		// 4. 成功命中
		logger.Debugf("initrd tool detected: %s, stdout=%s", tool.name, stdout)
		fixer.initrdTool = tool.name
		return nil
	}

	return errors.Errorf("no initramfs tool detected")
}

// detectKernels 探测离线系统的内核
func (fixer *linuxSystemFixer) detectKernels() error {
	logger.Debugf("detectKernels: ++")
	defer logger.Debugf("detectKernels: --")

	if fixer.rootMountPoint == "" {
		return ErrorRootEnvNotMounted
	}

	ks, err := info.QueryLinuxKernels(fixer.rootMountPoint)
	if err != nil {
		return err
	}

	// 过滤掉非启动内核
	ks2 := make([]info.LinuxKernel, 0)
	for _, k := range ks {
		if !k.Bootable {
			continue
		}
		ks2 = append(ks2, k)
	}

	fixer.kernels = ks2

	logger.Debugf("detectKernels: bootable kernels:\n%s", extend.Pretty(ks2))

	return nil
}

// linuxSystemFixer 探测离线系统的grub
func (fixer *linuxSystemFixer) detectGrub() error {
	logger.Debugf("detectGrub: ++")
	defer logger.Debugf("detectGrub: --")

	if fixer.rootMountPoint == "" {
		return ErrorRootEnvNotMounted
	}

	ver, cfg := DetectGrub(fixer.rootMountPoint)
	if ver == -1 {
		return errors.New("grub not found")
	}

	fixer.grubVer, fixer.grubCfg = ver, cfg
	return nil
}

// executeWithChroot 在chroot环境执行命令
func (fixer *linuxSystemFixer) executeWithChroot(cmdline string) (exitcode int, output string, err error) {
	if fixer.rootMountPoint == "" {
		return -1, "", ErrorRootEnvNotMounted
	}

	chrootCmdline := fmt.Sprintf(`chroot %s /bin/bash -c "export PATH=/sbin:/bin:/usr/sbin:/usr/bin:$PATH; %s"`,
		fixer.rootMountPoint, cmdline)

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
	defer os.RemoveAll(tmpMp)

	candDirs := []string{
		filepath.Join(tmpMp, "lost+found"),
		filepath.Join(tmpMp, ".runstorsnap"),
	}

	for _, dev := range fixer.fsDevices {
		_ = Umount(tmpMp)

		_, em := Mount(fixer.ctx, dev, tmpMp, false)
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

	if fixer.rootMountPoint == "" {
		return ErrorRootEnvNotMounted
	}

	ts := time.Now().Unix()
	fstabPath := filepath.Join(fixer.rootMountPoint, "etc/fstab")
	fstabBkPath := filepath.Join(fixer.rootMountPoint, fmt.Sprintf("etc/fstab.bk.%d", ts))

	//
	// 备份
	//

	_, _, e := command.Execute("cp " + fstabPath + " " + fstabBkPath)
	if e != nil {
		return errors.Wrap(e, "backup /etc/fstab")
	}

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
	newContentLines := make([]string, 0)

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
			funk.InStrings([]string{"tmpfs", "devpts", "sysfs", "proc"}, items[2]) ||
			strings.HasPrefix(items[0], "/dev/mapper") {
			newContentLines = append(newContentLines, line)
			continue
		}

		if devUuid, e := DetectUuidByBlkid(items[0]); e == nil && devUuid != "" {
			items[0] = fmt.Sprintf("UUID=%s", devUuid)
			newContentLines = append(newContentLines, strings.Join(items, "    "))
			continue
		}

		// TODO 抛出警告，提示可能存在恢复后系统无法启动的情况
		newContentLines = append(newContentLines, line)
	}

	newContent := strings.Join(newContentLines, "\n")

	return os.WriteFile(fstabPath, []byte(newContent), 0644)
}

// fixEfiBootConf 修复Efi的启动配置
func (fixer *linuxSystemFixer) fixEfiBootConf() error {
	logger.Debugf("fixEfiBootConf: ++")
	defer logger.Debugf("fixEfiBootConf: --")

	if fixer.rootMountPoint == "" {
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

func enumFsDevice(offline []string) ([]string, error) {
	logger.Debugf("enumFsDevice: ++")
	defer logger.Debugf("enumFsDevice: --")

	logger.Debugf("enumFsDevice: offline = %s", extend.Pretty(offline))

	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return nil, err
	}

	logger.Debugf("enumFsDevice: psinfo=\n%s", psinfo.Pretty())

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
	var devs []string
	for _, dev := range fsDevList {
		fsStr, _ := DetectFSTypeByBlkid(dev)
		if funk.InStrings(SupportedFsTypes, fsStr) {
			devs = append(devs, dev)
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
