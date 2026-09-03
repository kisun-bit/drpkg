//go:build linux

package repairvm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

// Create 创建一个修复虚拟机资源
func Create(ctx context.Context, opt *Option) (*Vm, error) {
	if opt == nil {
		return nil, errors.New("opt is nil")
	}

	if err := opt.Validate(); err != nil {
		return nil, err
	}

	vm := &Vm{
		ctx:   ctx,
		opt:   opt,
		logs:  make(chan x2xcore.LogEntry, 1000),
		uuid_: uuid.New(),
	}

	vm.infof(LogTplReceiveRepairRequest)

	sourceHP := hardwarePlatform(
		opt.RecoveryParams.Source.Base,
		opt.RecoveryParams.Source.Virt,
	)
	targetHP := hardwarePlatform(
		opt.RecoveryParams.Target.Base,
		opt.RecoveryParams.Target.Virt,
	)

	vm.infof(
		LogTplRepairRequestDetails,
		len(opt.RecoveryParams.OfflineSystemDisks),
		opt.RecoveryParams.Source.Arch,
		opt.RecoveryParams.OSType,
		opt.RecoveryParams.FsckFs,
		sourceHP,
		targetHP,
	)

	vm.cancel = func() {}
	if opt.RecoveryParams.TimeoutSeconds > 0 {
		vm.ctx, vm.cancel = context.WithTimeout(ctx, time.Duration(opt.RecoveryParams.TimeoutSeconds)*time.Second)
	}

	vm.cacheDir = filepath.Join(
		os.TempDir(),
		vm.uuid_.String(),
	)

	if err := os.MkdirAll(vm.cacheDir, 0755); err != nil {
		return nil, err
	}

	ok := false

	defer func() {
		if !ok {
			_ = os.RemoveAll(vm.cacheDir)
		}
	}()

	if err := vm.prepare(); err != nil {
		return nil, err
	}

	if err := vm.buildCommand(); err != nil {
		return nil, err
	}

	ok = true

	logger.Debugf(
		"qemu command:\n%s %s",
		vm.cmdCaller, strings.Join(vm.cmdArgs, " "),
	)

	return vm, nil
}

func (vm *Vm) prepare() error {

	// virtio serial socket
	vm.reqSockFile = filepath.Join(
		vm.cacheDir,
		"request.sock",
	)
	vm.logSockFile = filepath.Join(
		vm.cacheDir,
		"log.sock")

	if err := vm.prepareBoot(); err != nil {
		return err
	}

	if err := vm.prepareSimulator(); err != nil {
		return err
	}

	return nil
}

func (vm *Vm) prepareBoot() error {

	path := vm.opt.VmBootDiskFile

	ok, err := isQCOW2(path)

	if err != nil {
		return err
	}

	if ok {

		overlay, err := createBootOverlay(
			context.Background(),
			path,
		)

		if err != nil {
			return err
		}

		vm.vmBootDisk = overlay

	} else {

		vm.vmBootImage = path

	}

	return nil
}

func (vm *Vm) prepareSimulator() error {

	arch := vm.opt.RecoveryParams.Source.Arch

	if arch == "" {
		return errors.New(
			"unsupported architecture",
		)
	}

	sims, err := loadSimulatorsFromFile(
		vm.opt.SimulatorConfigFile,
	)

	if err != nil {
		return err
	}

	sim, ok := sims[arch]

	if !ok {
		return errors.Errorf(
			"simulator not found: %s",
			arch,
		)
	}

	vm.arch = arch
	vm.simulator = sim
	vm.cmdCaller = sim

	return nil
}

func (vm *Vm) buildCommand() error {

	// 根据架构选择机型
	vm.machine = "q35"
	if vm.arch == "arm64" {
		vm.machine = "virt"
	}

	// 解析 UEFI 固件（arm64 强制 UEFI）
	firmware, err := resolveFirmware(
		vm.arch,
		vm.opt.BootMode,
		vm.opt.Firmware,
		vm.cacheDir,
	)
	if err != nil {
		return err
	}
	vm.firmware = firmware

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-name", fmt.Sprintf(
			"guest=repairvm.%s.%s.%s",
			vm.opt.RecoveryParams.OSType,
			vm.arch,
			vm.uuid_.String(),
		),

		"-machine", vm.machine,

		"-uuid", vm.uuid_.String(),

		"-smp", "4",

		"--display", "none",
	)

	if vm.opt.RecoveryParams.OSType == "windows" {
		vm.cmdArgs = append(vm.cmdArgs, "-m", "2G")
		// NOTE：不要将windows pe的内存设置为>2G的，实际测试发现32位windows pe内存配置为3G或4G时出现启动卡死在windows图标的阶段
	} else {
		vm.cmdArgs = append(vm.cmdArgs, "-m", "1G")
	}

	// UEFI 固件（pflash），仅 UEFI 启动时追加
	vm.cmdArgs = vm.firmware.addArgs(vm.cmdArgs)

	// KVM 仅在修复虚拟机架构与宿主机架构一致时可用；
	// 跨架构（如 amd64 主机运行 arm64 修复虚拟机）只能使用 TCG。
	useKvm := vm.arch == runtime.GOARCH &&
		IsKVMAvailable() &&
		!vm.opt.ForceUseTcg &&
		vm.opt.RecoveryParams.OSType == define.OsLinux
	if useKvm {
		vm.cmdArgs = append(vm.cmdArgs, "-enable-kvm")
	} else {
		vm.cmdArgs = append(vm.cmdArgs, "-accel", "tcg,thread=multi")
	}

	switch vm.arch {
	case "amd64":
		if useKvm {
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "host")
		} else {
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "qemu64")
		}

	case "386":
		// 实际测试发现386的winpe，只有qemu64才能启动成功
		if vm.opt.RecoveryParams.OSType == "windows" {
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "qemu64")
		} else {
			// 使用 x86_64 QEMU 模拟 32 位 x86 CPU
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "qemu32")
		}

	case "arm64":
		if useKvm {
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "host")
		} else {
			vm.cmdArgs = append(vm.cmdArgs, "-cpu", "cortex-a72")
		}

	default:
		return errors.Errorf("unsupported architecture: %s", vm.arch)
	}

	vm.addVirtioSerial()

	vm.addStorage()

	vm.addCDROM()

	return nil
}

func (vm *Vm) addVirtioSerial() {
	vm.reqSockName = x2xcore.RequestVirtioPortName
	vm.logSockName = x2xcore.LogVirtioPortName

	// 1. 只需要一个 virtio-serial 控制器（支持最多 31 个端口）
	vm.cmdArgs = append(vm.cmdArgs, "-device", "virtio-serial")

	// 2. Request 通道
	vm.addVirtioSerialPort(
		"req-ch",
		vm.reqSockFile,
		vm.reqSockName,
	)

	// 3. Log 通道
	vm.addVirtioSerialPort(
		"log-ch",
		vm.logSockFile,
		vm.logSockName,
	)
}

// addVirtioSerialPort 添加一个 virtio-serial 端口
// chardevID: QEMU 内部标识符，必须全局唯一
// sockPath:  Host 侧 Unix Socket 文件路径
// portName:  Guest 侧可见的通道名称（/sys/class/virtio-ports/<vport>/name）
func (vm *Vm) addVirtioSerialPort(chardevID, sockPath, portName string) {
	if sockPath == "" {
		logger.Warnf("virtio-serial port %q skipped: empty socket path", portName)
		return
	}

	vm.cmdArgs = append(vm.cmdArgs,
		"-chardev", fmt.Sprintf("socket,id=%s,path=%s,server=on,wait=off", chardevID, sockPath),
		"-device", fmt.Sprintf("virtserialport,chardev=%s,name=%s", chardevID, portName),
	)
}

func (vm *Vm) addStorage() {
	var scsiIndex int

	// ========== Boot Disk: virtio-blk ==========
	if vm.vmBootDisk != "" {
		format := "qcow2"
		if ok, _ := isQCOW2(vm.vmBootDisk); ok {
			format = "qcow2"
		} else {
			format = "raw"
		}

		vm.cmdArgs = append(vm.cmdArgs,
			"-blockdev", fmt.Sprintf("node-name=boot-disk,driver=%s,file.driver=file,file.filename=%s",
				format, vm.vmBootDisk),
			"-device", "virtio-blk-pci,drive=boot-disk,bootindex=1",
		)
	}

	// ========== Offline Disks: virtio-scsi ==========
	if len(vm.opt.RecoveryParams.OfflineSystemDisks) > 0 {
		// Only add SCSI controller when there are SCSI disks
		vm.cmdArgs = append(vm.cmdArgs,
			"-device", "virtio-scsi-pci,id=scsi0",
		)

		for i, d := range vm.opt.RecoveryParams.OfflineSystemDisks {
			format := "raw"
			if ok, _ := isQCOW2(d.Path); ok {
				format = "qcow2"
			}

			driveID := fmt.Sprintf("scsi-disk%d", scsiIndex)

			vm.cmdArgs = append(vm.cmdArgs,
				"-drive", fmt.Sprintf("id=%s,if=none,file=%s,format=%s,file.locking=on",
					driveID, d.Path, format),
				"-device", fmt.Sprintf("scsi-hd,drive=%s,bus=scsi0.0",
					driveID),
			)

			switch vm.opt.RecoveryParams.OSType {
			case "linux":
				suffix := 'a' + i
				vm.offlineDisks = append(vm.offlineDisks, "/dev/sd"+string(rune(suffix)))
			case "windows":
				vm.offlineDisks = append(vm.offlineDisks, fmt.Sprintf(`\\.\PHYSICALDRIVE%d`, i))
			}

			scsiIndex++
		}

		logger.Debugf("offlineDisks: %v", extend.Pretty(vm.offlineDisks))
	}
}

func (vm *Vm) addCDROM() {

	// arm64 的 virt 机型没有 IDE/AHCI 控制器，光驱必须走 virtio-scsi；
	// amd64 的 q35 机型沿用原有的 SATA(ich9-ahci) + ide-cd 方式。
	isArm64 := vm.machine == "virt"

	if isArm64 {
		vm.cmdArgs = append(vm.cmdArgs,
			"-device", "virtio-scsi-pci,id=cd-scsi",
		)
	} else {
		vm.cmdArgs = append(vm.cmdArgs,
			"-device", "ich9-ahci,id=sata",
		)
	}

	// pe

	if vm.vmBootImage != "" {

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-drive", fmt.Sprintf(
				"id=tmpos,if=none,media=cdrom,file=%s",
				vm.vmBootImage,
			),
		)

		if isArm64 {
			vm.cmdArgs = append(vm.cmdArgs,
				"-device", "scsi-cd,drive=tmpos,bus=cd-scsi.0",
			)
		} else {
			vm.cmdArgs = append(vm.cmdArgs,
				"-device", "ide-cd,drive=tmpos,bus=sata.2",
			)
		}

	}

	// driver iso

	if vm.opt.DriverDBImageFile != "" {

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-drive", fmt.Sprintf(
				"id=driver,if=none,media=cdrom,file=%s",
				vm.opt.DriverDBImageFile,
			),
		)

		if isArm64 {
			vm.cmdArgs = append(vm.cmdArgs,
				"-device", "scsi-cd,drive=driver,bus=cd-scsi.0",
			)
		} else {
			vm.cmdArgs = append(vm.cmdArgs,
				"-device", "ide-cd,drive=driver,bus=sata.3",
			)
		}

	}

	bootOrderStr := "c"
	if vm.vmBootImage != "" {
		bootOrderStr = "d"
	}

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-boot", fmt.Sprintf(
			"menu=off,order=%s",
			bootOrderStr,
		),
	)

}

func (vm *Vm) Repair() (extra map[string]string, err error) {
	defer func() {
		if err == nil {
			vm.infof(x2xcore.LogTplForRepairSuccessWith0Args)
			return
		}
		vm.infof(x2xcore.LogTplForRepairFailedWith1Args, err)
		return
	}()

	vm.cmd = exec.CommandContext(vm.ctx, vm.cmdCaller, vm.cmdArgs...)
	vm.cmd.Stdin = nil
	vm.cmd.Stdout = nil
	vm.cmd.Stderr = &vm.cmdStdout

	vm.infof(LogTplCreateRepairVM)

	// 启动 guest
	if err = vm.cmd.Start(); err != nil {
		return nil, errors.Wrapf(err, "start guest")
	}

	// 检查 guest 是否已经立即退出
	guestExitCh := make(chan error, 1)
	go func() {
		exitErr := vm.cmd.Wait()
		if exitErr != nil {
			exitErr = errors.Wrapf(exitErr, "qemu exited: %s", vm.cmdStdout.String())
		}
		guestExitCh <- exitErr
	}()

	vm.infof(LogTplCreateCommunicationChannel)

	// 等待serial就绪
	vm.reqSockConn, err = WaitSerialSocketReady(vm.ctx, guestExitCh, vm.reqSockFile, 60, 10*time.Second)
	if err != nil {
		return nil, errors.Wrapf(err, "WaitSerialSocketReady(ReqSock)")
	}

	vm.logSockConn, err = WaitSerialSocketReady(vm.ctx, guestExitCh, vm.logSockFile, 60, 10*time.Second)
	if err != nil {
		return nil, errors.Wrapf(err, "WaitSerialSocketReady(LogSock)")
	}

	vm.infof(LogTplWaitRepairVMReady)

	// 等待 guest 系统启动完成
	if err = x2xcore.ReadReceivedSerialMessageTypeGuestReady(*vm.reqSockConn); err != nil {
		return nil, errors.Wrapf(err, "ReadReceivedSerialMessageTypeGuestReady")
	}

	vm.infof(LogTplSendRepairRequest)

	// 通知 guest 执行修复操作
	fixParam := &x2xcore.FixerCreateOptions{
		OfflineSysDisks: vm.offlineDisks,
		RecoveryParam:   vm.opt.RecoveryParams,
		InRepairVM:      true,
	}
	logger.Debugf("fixParam: %s", extend.Pretty(fixParam))
	if err = x2xcore.WriteSerialMessageTypeStartRepair(*vm.reqSockConn, *fixParam); err != nil {
		return nil, errors.Wrapf(err, "WriteSerialMessageTypeStartRepair")
	}

	// 收集程序日志
	go func() {
		scanner := bufio.NewScanner(*vm.logSockConn)
		// 增大缓冲区，防止大消息截断
		scanner.Buffer(make([]byte, 1<<20), 1<<20)

		for scanner.Scan() {
			line := scanner.Bytes()
			logger.Debugf("[repair] ----------- %s", strings.TrimSuffix(string(line), "\n"))
		}
	}()

	// 等待 guest 日志和修复结果
	for {

		sm := &x2xcore.SerialMessage{}
		if err = struc.Unpack(*vm.reqSockConn, sm); err != nil {
			return nil, errors.Wrapf(err, "Unpack SerialMessage")
		}

		switch sm.Type {
		case x2xcore.SerialMessageTypeRepairLog:
			logE := x2xcore.LogEntry{}
			if err = json.Unmarshal(sm.Body, &logE); err != nil {
				return nil, err
			}
			vm.cacheLog(logE)

		case x2xcore.SerialMessageTypeRepairResult:
			repairResult := x2xcore.RepairResult{}
			if err = json.Unmarshal(sm.Body, &repairResult); err != nil {
				return nil, err
			}
			if repairResult.Success {
				return repairResult.Extra, nil
			}
			return nil, errors.New(repairResult.ErrorMsg)

		default:
			return nil, errors.Errorf("unknown serial message (type: %v)", sm.Type)
		}

	}
}

// Logs 返回修复虚拟机的日志通道（只读）。
//
// 调用方必须持续消费该通道：通道缓冲容量有限，写满后修复过程中
// 产生的新日志会阻塞修复流程。
func (vm *Vm) Logs() <-chan x2xcore.LogEntry {
	return vm.logs
}

func (vm *Vm) Release() error {
	vm.infof(LogTplReleaseRepairVM)

	if vm == nil {
		return nil
	}

	vm.cancel()

	if vm.reqSockConn != nil {
		_ = (*vm.reqSockConn).Close()
		vm.reqSockConn = nil
	}

	if vm.logSockConn != nil {
		_ = (*vm.logSockConn).Close()
		vm.logSockConn = nil
	}

	if vm.cmd != nil && vm.cmd.Process != nil {

		// 优雅关闭
		if err := vm.cmd.Process.Signal(os.Interrupt); err != nil {

			// 强制结束
			if err := vm.cmd.Process.Kill(); err != nil {
				return err
			}
		}

		// 等待进程退出
		_, _ = vm.cmd.Process.Wait()
	}

	// 清理临时目录
	if vm.cacheDir != "" {
		_ = os.RemoveAll(vm.cacheDir)
	}

	// 清理修复虚拟机系统的overlay
	if vm.vmBootDisk != "" {
		_ = os.RemoveAll(vm.vmBootDisk)
	}

	return nil
}

func (vm *Vm) logf(level x2xcore.LogLevel, tpl x2xcore.LangTpl, v ...interface{}) {
	le := x2xcore.LogEntry{
		Level: level,
		MsgEn: fmt.Sprintf(tpl.En, v...),
		MsgZh: fmt.Sprintf(tpl.Zh, v...),
	}
	vm.cacheLog(le)
}

func (vm *Vm) cacheLog(le x2xcore.LogEntry) {
	vm.logs <- le
	le.Println()
}

func (vm *Vm) infof(tpl x2xcore.LangTpl, v ...interface{}) {
	vm.logf(x2xcore.LogInfo, tpl, v...)
}

func (vm *Vm) warnf(tpl x2xcore.LangTpl, v ...interface{}) {
	vm.logf(x2xcore.LogWarn, tpl, v...)
}

func (vm *Vm) errorf(tpl x2xcore.LangTpl, v ...interface{}) {
	vm.logf(x2xcore.LogError, tpl, v...)
}
