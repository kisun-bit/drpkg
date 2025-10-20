//go:build linux

package image

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/sys/unix"
)

//var shmIdRegex = regexp.MustCompile(`ShmID:\s*(\d+)`)

type Image struct {
	Path string
	// virtualSize 虚拟磁盘大小
	virtualSize int64
	// format 虚拟磁盘格式
	format string
	// proc 托管的Qemu进程
	proc *exec.Cmd
	// rwLock 读写锁
	rwLock sync.Mutex

	//
	// IPC
	//

	shmMutex sync.Mutex
	shmId    int
	shmSize  int64
	shmData  []byte

	//
	// 事件
	//

	efdMutex sync.Mutex
	efdr     int
	efdp     int
}

// Open 打开虚拟磁盘文件
func Open(path string) (img *Image, err error) {
	logger.Debugf("start opening image: %s", path)

	if err = checkQemuTool(); err != nil {
		return nil, err
	}

	imgInfo, err := ImageJsonInfo(context.Background(), path)
	if err != nil {
		return nil, err
	}

	format := gjson.Get(imgInfo, "format").String()
	size := gjson.Get(imgInfo, "virtual-size").Int()
	if size <= 0 {
		return nil, errors.New("virtual size is 0")
	}

	logger.Debugf("virtual size is %d, format is %s, pid is %d", size, format, os.Getpid())

	// 使用 EFD_SEMAPHORE 标志，这样每次 read() 只会递减 1，避免重复处理
	efdr, err := unix.Eventfd(0, unix.EFD_SEMAPHORE)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open eventfd(req)")
	}
	defer func() {
		if err != nil {
			_ = unix.Close(efdr)
		}
	}()

	// 使用 EFD_SEMAPHORE 标志，这样每次 read() 只会递减 1，避免重复处理
	efdp, err := unix.Eventfd(0, unix.EFD_SEMAPHORE)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open eventfd(resp)")
	}
	defer func() {
		if err != nil {
			_ = unix.Close(efdp)
		}
	}()

	shmId, err := unix.SysvShmGet(os.Getpid(), shmSize, unix.IPC_CREAT|0o660)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmGet")
	}
	defer func() {
		if err != nil {
			_, _ = unix.SysvShmCtl(shmId, unix.IPC_RMID, nil)
		}
	}()

	shmData, err := unix.SysvShmAttach(shmId, 0, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmAttach(fd:%d)", shmId)
	}
	defer func() {
		if err != nil {
			_ = unix.SysvShmDetach(shmData)
		}
	}()

	efdrFile := os.NewFile(uintptr(efdr), "eventfd_r")
	if efdrFile == nil {
		return nil, errors.New("failed to create os.File for eventfd(req)")
	}
	efdpFile := os.NewFile(uintptr(efdp), "eventfd_p")
	if efdpFile == nil {
		return nil, errors.New("failed to create os.File for eventfd(resp)")
	}

	// 确保使用绝对路径
	absPath := path
	if !filepath.IsAbs(path) {
		absPath, err = filepath.Abs(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get absolute path for %s", path)
		}
	}

	proc := exec.Command(ioToolPath,
		"-f", absPath,
		"-r", strconv.Itoa(3),
		"-p", strconv.Itoa(4),
		"-s", strconv.Itoa(shmId))
	proc.ExtraFiles = []*os.File{efdrFile, efdpFile}
	logger.Debugf("qemu cmdline: %s", proc.String())

	procStdout, _ := proc.StdoutPipe()
	procStderr, _ := proc.StderrPipe()
	if err = proc.Start(); err != nil {
		return nil, errors.Wrapf(err, "start %s", ioToolPath)
	}
	defer func() {
		if err != nil && extend.IsProcessRunning(proc) {
			_ = proc.Process.Kill()
		}
	}()

	logger.Debugf("pid of %s: %d", ioToolPath, proc.Process.Pid)

	logPipe := func(tag string, rc io.ReadCloser) {
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debugf("<PROCESS(%d)>(%s): %s", proc.Process.Pid, tag, line)
		}
	}
	go logPipe("stdout", procStdout)
	go logPipe("stderr", procStderr)

	// 等待 C 端初始化完成并发送就绪信号
	logger.Debugf("waiting for C process to be ready...")
	var readyBuf [8]byte
	n, err := unix.Read(efdp, readyBuf[:])
	if err != nil {
		return nil, errors.Wrapf(err, "failed to receive ready signal from C process")
	}
	readyValue := binary.LittleEndian.Uint64(readyBuf[:])
	logger.Debugf("C process is ready (read %d bytes, value=%d)", n, readyValue)

	img = &Image{
		Path:        path,
		virtualSize: size,
		format:      format,
		proc:        proc,
		shmId:       shmId,
		shmSize:     shmSize,
		shmData:     shmData,
		efdr:        efdr,
		efdp:        efdp,
	}

	go img.monitorQemuProcess()

	logger.Debugf("%s is opened", img.String())
	return img, nil
}

func (img *Image) String() string {
	return fmt.Sprintf("<IMAGE(name=%s,vsize=%v,fmt=%s)>", filepath.Base(img.Path), img.virtualSize, img.format)
}

func (img *Image) ReadAt(b []byte, off int64) (n int, err error) {
	logger.Debugf("%s.ReadAt() ++ off=%v", img.String(), off)
	defer logger.Debugf("%s.ReadAt() --", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	if err = img.checkQemuProcessAlive(); err != nil {
		return 0, errors.Wrapf(err, "ReadAt")
	}

	remain := img.virtualSize - off
	logger.Debugf("%s.ReadAt() virtualSize=%d, off=%d, remain=%d", img.String(), img.virtualSize, off, remain)
	if remain <= 0 {
		return 0, io.EOF
	}

	readLen := int64(len(b))
	if remain < readLen {
		readLen = remain
	}

	logger.Debugf("%s.ReadAt() len(b)=%d, remain=%d, readLen=%d", img.String(), len(b), remain, readLen)

	// 发送读指令，每次最多读1MiB，若readLen的长度超过1MiB，则分多次读
	var totalRead int
	for totalRead < int(readLen) {
		chunkSize := int(readLen) - totalRead
		if chunkSize > rwMaxLen {
			chunkSize = rwMaxLen
		}

		img.shmMutex.Lock()

		// 准备读请求
		req := readRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _READ,
				Sequence: uint64(time.Now().UnixNano()),
			},
			Offset: off + int64(totalRead),
			Length: int32(chunkSize),
		}

		// 手动将请求写入共享内存（使用小端序）
		// 结构：type(4) + sequence(8) + offset(8) + length(4) = 24字节
		binary.LittleEndian.PutUint32(img.shmData[requestOffset+0:], uint32(req.Type))
		binary.LittleEndian.PutUint64(img.shmData[requestOffset+4:], req.Sequence)
		binary.LittleEndian.PutUint64(img.shmData[requestOffset+12:], uint64(req.Offset))
		binary.LittleEndian.PutUint32(img.shmData[requestOffset+20:], uint32(req.Length))

		// 调试：读回验证
		verifyType := binary.LittleEndian.Uint32(img.shmData[requestOffset+0:])
		verifySeq := binary.LittleEndian.Uint64(img.shmData[requestOffset+4:])
		verifyOff := int64(binary.LittleEndian.Uint64(img.shmData[requestOffset+12:]))
		verifyLen := int32(binary.LittleEndian.Uint32(img.shmData[requestOffset+20:]))
		logger.Debugf("%s.ReadAt() verify written: Type=%d, Seq=%d, Offset=%d, Length=%d",
			img.String(), verifyType, verifySeq, verifyOff, verifyLen)

		// 打印原始字节
		rawBytes := img.shmData[requestOffset : requestOffset+24]
		logger.Debugf("%s.ReadAt() raw bytes: % x", img.String(), rawBytes)

		logger.Debugf("%s.ReadAt() sending request: offset=%d, length=%d", img.String(), req.Offset, req.Length)

		// 通知QEMU进程处理请求
		if _, err = unix.Write(img.efdr, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			return totalRead, errors.Wrapf(err, "failed to notify QEMU process")
		}

		logger.Debugf("%s.ReadAt() waiting for response...", img.String())

		// 等待QEMU进程处理完成
		var buf [8]byte
		if _, err = unix.Read(img.efdp, buf[:]); err != nil {
			img.shmMutex.Unlock()
			return totalRead, errors.Wrapf(err, "failed to wait for QEMU process")
		}

		logger.Debugf("%s.ReadAt() received response", img.String())

		// 从共享内存中读取响应
		respData := img.shmData[responseOffset:]

		// 手动解析响应（使用小端序，与 C 端一致）
		// C端结构： type(4) + sequence(8) + errorCode(4) + length(4)
		respType := binary.LittleEndian.Uint32(respData[0:4])
		respSequence := binary.LittleEndian.Uint64(respData[4:12])
		respErrorCode := int32(binary.LittleEndian.Uint32(respData[12:16]))
		respLength := int32(binary.LittleEndian.Uint32(respData[16:20]))

		logger.Debugf("%s.ReadAt() response parsed: Type=%d, Sequence=%d, ErrorCode=%d, Length=%d",
			img.String(), respType, respSequence, respErrorCode, respLength)

		if respErrorCode != 0 {
			img.shmMutex.Unlock()
			return totalRead, errors.Errorf("QEMU read error: %d", respErrorCode)
		}

		// 复制数据到用户缓冲区
		readSize := int(respLength)
		// C端数据紧跟在基础响应结构后：type(4) + sequence(8) + errorCode(4) + length(4) = 20字节
		dataOffset := uintptr(20)
		copy(b[totalRead:totalRead+readSize], respData[dataOffset:dataOffset+uintptr(readSize)])

		img.shmMutex.Unlock()

		totalRead += readSize
		if readSize < chunkSize {
			break // 读取到EOF
		}
	}

	return totalRead, nil
}

func (img *Image) WriteAt(b []byte, off int64) (n int, err error) {
	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	if err = img.checkQemuProcessAlive(); err != nil {
		return 0, errors.Wrapf(err, "WriteAt")
	}

	// 发送写指令，每次最多写1MiB，若b的长度超过1MiB，则分多次写
	var totalWritten int
	for totalWritten < len(b) {
		chunkSize := len(b) - totalWritten
		if chunkSize > rwMaxLen {
			chunkSize = rwMaxLen
		}

		img.shmMutex.Lock()

		// 准备写请求
		req := writeRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _WRITE,
				Sequence: uint64(time.Now().UnixNano()),
			},
			Offset: off + int64(totalWritten),
			Length: int32(chunkSize),
			Data:   b[totalWritten : totalWritten+chunkSize],
		}

		// 手动将请求写入共享内存（使用小端序）
		// 结构：type(4) + sequence(8) + offset(8) + length(4) + data
		binary.LittleEndian.PutUint32(img.shmData[requestOffset+0:], uint32(req.Type))
		binary.LittleEndian.PutUint64(img.shmData[requestOffset+4:], req.Sequence)
		binary.LittleEndian.PutUint64(img.shmData[requestOffset+12:], uint64(req.Offset))
		binary.LittleEndian.PutUint32(img.shmData[requestOffset+20:], uint32(req.Length))
		copy(img.shmData[requestOffset+24:], req.Data)

		// 通知QEMU进程处理请求
		if _, err = unix.Write(img.efdr, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			return totalWritten, errors.Wrapf(err, "failed to notify QEMU process")
		}

		// 等待QEMU进程处理完成
		var buf [8]byte
		if _, err = unix.Read(img.efdp, buf[:]); err != nil {
			img.shmMutex.Unlock()
			return totalWritten, errors.Wrapf(err, "failed to wait for QEMU process")
		}

		// 从共享内存中读取响应
		respData := img.shmData[responseOffset:]
		// 手动解析响应（使用小端序）
		// C端结构： type(4) + sequence(8) + errorCode(4) + length(4)
		respErrorCode := int32(binary.LittleEndian.Uint32(respData[12:16]))
		respLength := int32(binary.LittleEndian.Uint32(respData[16:20]))

		if respErrorCode != 0 {
			img.shmMutex.Unlock()
			return totalWritten, errors.Errorf("QEMU write error: %d", respErrorCode)
		}

		// 更新已写入的字节数
		writtenSize := int(respLength)
		totalWritten += writtenSize

		img.shmMutex.Unlock()

		if writtenSize < chunkSize {
			break // 写入中断
		}
	}

	return totalWritten, nil
}

func (img *Image) Sync() error {
	logger.Debugf("%s.Sync() ++", img.String())
	defer logger.Debugf("%s.Sync() --", img.String())

	img.shmMutex.Lock()
	defer img.shmMutex.Unlock()

	if err := img.checkQemuProcessAlive(); err != nil {
		return errors.Wrapf(err, "Sync")
	}

	// 准备刷盘请求
	req := flushRequest{
		shmBaseRequest: shmBaseRequest{
			Type:     _FLUSH,
			Sequence: uint64(time.Now().UnixNano()),
		},
	}

	// 手动将请求写入共享内存（使用小端序）
	// 结构：type(4) + sequence(8) = 12字节
	binary.LittleEndian.PutUint32(img.shmData[requestOffset+0:], uint32(req.Type))
	binary.LittleEndian.PutUint64(img.shmData[requestOffset+4:], req.Sequence)

	// 通知QEMU进程处理请求
	if _, err := unix.Write(img.efdr, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		return errors.Wrapf(err, "failed to notify QEMU process")
	}

	// 等待QEMU进程处理完成
	var buf [8]byte
	if _, err := unix.Read(img.efdp, buf[:]); err != nil {
		return errors.Wrapf(err, "failed to wait for QEMU process")
	}

	// 从共享内存中读取响应
	respData := img.shmData[responseOffset:]
	// 手动解析响应（使用小端序）
	// C端结构： type(4) + sequence(8) + errorCode(4)
	respErrorCode := int32(binary.LittleEndian.Uint32(respData[12:16]))

	if respErrorCode != 0 {
		return errors.Errorf("QEMU flush error: %d", respErrorCode)
	}

	return nil
}

func (img *Image) Close() error {
	logger.Debugf("%s.Close() ++", img.String())
	defer logger.Debugf("%s.Close() --", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	if extend.IsProcessRunning(img.proc) {
		if eSync := img.Sync(); eSync != nil {
			logger.Warnf("%s.Close() Sync: %v", img.String(), eSync)
		}

		// 发送Close指令
		img.shmMutex.Lock()
		req := closeRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _Close,
				Sequence: uint64(time.Now().UnixNano()),
			},
		}

		// 手动将请求写入共享内存（使用小端序）
		// 结构：type(4) + sequence(8) = 12字节
		binary.LittleEndian.PutUint32(img.shmData[requestOffset+0:], uint32(req.Type))
		binary.LittleEndian.PutUint64(img.shmData[requestOffset+4:], req.Sequence)

		// 通知QEMU进程处理请求
		if _, err := unix.Write(img.efdr, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			logger.Warnf("%s.Close() notify QEMU: %v", img.String(), err)
		} else {
			// 等待QEMU进程处理完成
			var buf [8]byte
			if _, err := unix.Read(img.efdp, buf[:]); err != nil {
				logger.Warnf("%s.Close() wait for QEMU: %v", img.String(), err)
			}
		}
		img.shmMutex.Unlock()

		_ = img.proc.Wait()
	}

	if err := img.closeEventfd2(); err != nil {
		return err
	}
	if err := img.destroyShm(); err != nil {
		return err
	}
	return nil
}

func (img *Image) VirtualSize() int64 {
	return img.virtualSize
}

func (img *Image) Size() int64 {
	stat, err := os.Stat(img.Path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

func (img *Image) checkQemuProcessAlive() error {
	if !extend.IsProcessRunning(img.proc) {
		return errors.New("qemu process is not running")
	}
	return nil
}

func (img *Image) monitorQemuProcess() {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	for range t.C {
		if extend.IsProcessRunning(img.proc) {
			continue
		}
		if ec := img.proc.ProcessState.ExitCode(); ec != 0 {
			logger.Warnf("%s.monitorQemuProcess() qemu process exit code: %d", img.String(), ec)
		}
		_ = img.Close()
		return
	}
}

func (img *Image) closeEventfd2() error {
	img.efdMutex.Lock()
	defer img.efdMutex.Unlock()

	if img.efdr != 0 {
		if eClose := unix.Close(img.efdr); eClose != nil {
			return errors.Wrapf(eClose, "CloseEventfd2")
		}
		img.efdr = 0
	}

	if img.efdp != 0 {
		if eClose := unix.Close(img.efdp); eClose != nil {
			return errors.Wrapf(eClose, "CloseEventfd2")
		}
		img.efdp = 0
	}

	return nil
}

func (img *Image) destroyShm() error {
	img.shmMutex.Lock()
	defer img.shmMutex.Unlock()

	if img.shmData == nil {
		return nil
	}

	ed := unix.SysvShmDetach(img.shmData)
	if ed != nil {
		return ed
	}

	_, err := unix.SysvShmCtl(img.shmId, unix.IPC_RMID, nil)
	if err != nil {
		return errors.Wrapf(err, "SysvShmCtl[IPC_RMID]")
	}

	img.shmId = 0
	img.shmData = nil
	return nil
}
