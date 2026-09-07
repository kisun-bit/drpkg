package ioctl

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"time"
	"unsafe"

	"github.com/kisun-bit/drpkg/logger"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func ReqDrvIoctl(code uint, inBuffer []byte, _ uint64) (outBuffer []byte, err error) {
	byteRet, drvErrCode, err := ReqDrvIoctlFun(inBuffer, code)
	if err != nil {
		return nil, err
	}

	if drvErrCode != 0 {
		if drvErrCode == ERR_NO_MEMORY { // 对应驱动中内存不足
			err = ErrorInsufficientMemSpace
		} else if drvErrCode == ERROR_RINGBUFFER_NOT_WORKING_FORBID_CLEAR_BIT { // 对应驱动中，设置切换到实时状态时，共享缓冲区满了
			err = ErrorOverflow
		} else {
			if drvErrCode > 0 {
				err = errors.Errorf("driver ioctl failed, code:%d, need to check the internal driver error code.", drvErrCode)
			}
			if drvErrCode < 0 {
				err = errors.Errorf("driver ioctl failed, code:%d, need to look up Linux system error codes.", drvErrCode)
			}
		}
		return nil, err
	}

	return byteRet, nil
}

func ReqDrvIoctlFun(reqBuf []byte, code uint) (bytes []byte, drvErrCode int32, err error) {
	// 打开设备文件
	f, err := os.OpenFile(DriverSymbolName, os.O_RDWR, 0)
	if err != nil {
		return nil, 0, errors.Errorf("open device: %v", err)
	}
	defer f.Close()
	fd := int(f.Fd())

	// 计算请求长度
	reqSize := uint64(len(reqBuf))

	// 请求长度加 长度本身的长度
	totalLen := uint64(unsafe.Sizeof(RequestSize)) + uint64(reqSize)

	// 将请求长度写入buf
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint64(buf[0:uint64(unsafe.Sizeof(RequestSize))], reqSize)

	// 拷贝 request bytes 到 buf
	copy(buf[uint64(unsafe.Sizeof(RequestSize)):], reqBuf)

	if len(buf) == 0 {
		return nil, 0, errors.Errorf("internal error: buffer is empty")
	}

	// 发送ioctl
	DrvErrCode, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(GetIoctlCode(code)), // 你的 ioctl 编号函数
		uintptr(unsafe.Pointer(&buf[0])),
	)

	if errno != 0 {
		return nil, 0, errors.Errorf("ioctl syscall failed: %v", errno)
	}

	if DrvErrCode > 0 {
		return nil, int32(DrvErrCode), nil
	}

	// 由于linux 没有outputBuffer，因此，返回的数据就在请求的内存中，这里将请求的内存返回
	return buf[uint64(unsafe.Sizeof(RequestSize)):], 0, nil
}

// runCommandWithTimeout 在指定超时内执行脚本，并返回合并的 stdout/stderr 输出。
// 如果 scriptPath 包含路径分隔符，会按该路径查找；否则会在 PATH 中查找。
func runCommandWithTimeout(scriptPath string, args []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// 确认脚本存在（支持相对/绝对路径与 PATH 中查找）
	path, err := exec.LookPath(scriptPath)
	if err != nil {
		return "", errors.Errorf("script %q not found: %v", scriptPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()

	// 使用 CombinedOutput 简洁地获取 stdout+stderr（CommandContext 会在 ctx 取消时终止子进程）
	out, err := cmd.CombinedOutput()
	outStr := string(out)

	// ctx.Err() 若为 context.DeadlineExceeded，说明发生超时
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outStr, errors.Errorf("command %q timed out after %s; output:\n%s", scriptPath, timeout.String(), outStr)
	}

	if err != nil {
		return outStr, errors.Errorf("command %q failed: %v; output:\n%s", scriptPath, err, outStr)
	}
	return outStr, nil
}

// delTaskWithInitRamFs 执行删除任务脚本，timeout 为允许的最大运行时间（<=0 时使用 60s）
func delTaskWithInitRamFs(timeout time.Duration) error {
	const script = "biotrk_del_task"

	out, err := runCommandWithTimeout(script, nil, timeout)
	logger.Infof("script output:\n%s\n", out)
	if err != nil {
		logger.Errorf("script execution failed:%v", err)
		return err
	}
	return nil
}

// addTaskWithInitRamFs 打包 startCfg 写入临时文件并作为参数传给脚本，timeout 为允许的最大运行时间（<=0 时使用 60s）
func addTaskWithInitRamFs(startCfg *DRVReqStart, timeout time.Duration) error {
	if startCfg == nil {
		return errors.New("startCfg param is nil")
	}

	// pack
	var packedReq bytes.Buffer
	if err := struc.PackWithOptions(&packedReq, startCfg, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return errors.Errorf("pack err: %v", err)
	}

	// 创建临时文件，确保最后关闭并删除
	tmpFile, err := os.CreateTemp("", "packedReq-*.bin")
	if err != nil {
		return errors.Errorf("create temp file failed: %v", err)
	}
	tmpPath := tmpFile.Name()
	// 保证关闭文件句柄并删除临时文件（即便发生错误也会尝试）
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	// 写入内容并同步关闭
	if _, err := tmpFile.Write(packedReq.Bytes()); err != nil {
		return errors.Errorf("write temp file failed: %v", err)
	}
	if err := tmpFile.Sync(); err != nil {
		// 同步失败不是致命的，但记录一下
		logger.Warnf("warning: sync temp file failed:%v", err)
	}

	_ = tmpFile.Close()

	// 调用脚本并传递临时文件路径
	const script = "biotrk_add_task"
	out, err := runCommandWithTimeout(script, []string{tmpPath}, timeout)
	logger.Infof("script output:\n%s\n", out)
	if err != nil {
		logger.Errorf("script execution failed:%v", err)
		return err
	}

	return nil
}

// RegCreateDriverParameter 创建驱动的持久化参数
func RegCreateDriverParameter(start *DRVReqStart, timeout time.Duration) error {
	if err := addTaskWithInitRamFs(start, timeout); err != nil {
		return err
	}
	reqBytes, err := json.Marshal(start)
	if err != nil {
		return err
	}
	if err = PersistWriteFile(drvParameterPersistFile, reqBytes, 0666); err != nil {
		return err
	}
	return nil
}

// RegReadDriverParameter 读取驱动的持久化参数
func RegReadDriverParameter() (req *DRVReqStart, err error) {
	req = new(DRVReqStart)

	fBytes, err := os.ReadFile(drvParameterPersistFile)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(fBytes, req)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// RegDeleteDriverParameter 删除驱动的持久化参数
func RegDeleteDriverParameter(timeout time.Duration) error {
	if err := delTaskWithInitRamFs(timeout); err != nil {
		return err
	}
	if err := os.RemoveAll(drvParameterPersistFile); err != nil {
		return errors.Wrapf(err, "Remove %s", drvParameterPersistFile)
	}
	return nil
}
