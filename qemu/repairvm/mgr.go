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
	"strings"
	"time"

	"github.com/google/uuid"
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
		uuid_: uuid.New(),
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

	if vm.opt.RecoveryParams.OSType == "windows" {
		arch = "amd64"
	}

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

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-name", fmt.Sprintf(
			"guest=repairvm.%s.%s.%s",
			vm.opt.RecoveryParams.OSType,
			vm.arch,
			vm.uuid_.String(),
		),

		"-machine", "q35",

		"-accel", "tcg,thread=multi",

		"-uuid", vm.uuid_.String(),

		"-m", "2G",

		"-smp", "4",
	)

	if vm.opt.RecoveryParams.Source.Arch == "amd64" {
		vm.cmdArgs = append(vm.cmdArgs, "-cpu", "qemu64")
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
	if len(vm.opt.OfflineSystemDisks) > 0 {
		// Only add SCSI controller when there are SCSI disks
		vm.cmdArgs = append(vm.cmdArgs,
			"-device", "virtio-scsi-pci,id=scsi0",
		)

		for i, d := range vm.opt.OfflineSystemDisks {
			format := "raw"
			if ok, _ := isQCOW2(d.Path); ok {
				format = "qcow2"
			}

			driveID := fmt.Sprintf("scsi-disk%d", scsiIndex)

			vm.cmdArgs = append(vm.cmdArgs,
				"-drive", fmt.Sprintf("id=%s,if=none,file=%s,format=%s",
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

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-device", "ich9-ahci,id=sata",
	)

	// pe

	if vm.vmBootImage != "" {

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-drive", fmt.Sprintf(
				"id=tmpos,if=none,media=cdrom,file=%s",
				vm.vmBootImage,
			),
		)

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-device", "ide-cd,drive=tmpos,bus=sata.2",
		)

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

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-device", "ide-cd,drive=driver,bus=sata.3",
		)

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

func (vm *Vm) Repair() (err error) {
	vm.cmd = exec.Command(vm.cmdCaller, vm.cmdArgs...)
	vm.cmd.Stdin = os.Stdin
	vm.cmd.Stdout = os.Stdout
	vm.cmd.Stderr = os.Stderr

	// 启动 guest
	if err = vm.cmd.Start(); err != nil {
		return errors.Wrapf(err, "start guest")
	}

	// 等待serial就绪
	vm.reqSockConn, err = WaitSerialSocketReady(vm.ctx, vm.reqSockFile, 60, 10*time.Second)
	if err != nil {
		return errors.Wrapf(err, "WaitSerialSocketReady(ReqSock)")
	}

	vm.logSockConn, err = WaitSerialSocketReady(vm.ctx, vm.logSockFile, 60, 10*time.Second)
	if err != nil {
		return errors.Wrapf(err, "WaitSerialSocketReady(LogSock)")
	}

	// 等待 guest 系统启动完成
	if err = x2xcore.ReadReceivedSerialMessageTypeGuestReady(*vm.reqSockConn); err != nil {
		return errors.Wrapf(err, "ReadReceivedSerialMessageTypeGuestReady")
	}

	// 通知 guest 执行修复操作
	fixParam := &x2xcore.FixerCreateOptions{
		OfflineSysDisks: vm.offlineDisks,
		RecoveryParam:   vm.opt.RecoveryParams,
		InRepairVM:      true,
	}
	logger.Debugf("fixParam: %s", extend.Pretty(fixParam))
	if err = x2xcore.WriteSerialMessageTypeStartRepair(*vm.reqSockConn, *fixParam); err != nil {
		return errors.Wrapf(err, "WriteSerialMessageTypeStartRepair")
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
			return errors.Wrapf(err, "Unpack SerialMessage")
		}

		switch sm.Type {
		case x2xcore.SerialMessageTypeRepairLog:
			logE := x2xcore.LogEntry{}
			if err = json.Unmarshal(sm.Body, &logE); err != nil {
				return err
			}
			logE.Println()
			// TODO 缓存日志

		case x2xcore.SerialMessageTypeRepairResult:
			repairResult := x2xcore.RepairResult{}
			if err = json.Unmarshal(sm.Body, &repairResult); err != nil {
				return err
			}
			if repairResult.Success {
				return nil
			}
			return errors.New(repairResult.ErrorMsg)

		default:
			return errors.Errorf("unknown serial message (type: %v)", sm.Type)
		}

	}
}

func (vm *Vm) Release() error {

	if vm == nil {
		return nil
	}

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

		if err := os.RemoveAll(vm.cacheDir); err != nil {
			return err
		}
	}

	return nil
}
