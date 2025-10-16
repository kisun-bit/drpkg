package image

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/sys/unix"
)

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
	shmKey   int
	shmId    int
	shmSize  int64
	shmData  []byte

	//
	// 事件
	//

	efdMutex sync.Mutex
	efd      int
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

	logger.Debugf("virtual size is %d, format is %s", size, format)

	efd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open eventfd2")
	}
	defer func() {
		if err != nil {
			_ = unix.Close(efd)
		}
	}()

	cmdline := fmt.Sprintf("%s -f %s -e %d", ioToolPath, path, efd)
	logger.Debugf("qemu cmdline: %s", cmdline)

	proc := exec.Command("sh", "-c", cmdline)
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

	logPipe := func(rc io.ReadCloser) {
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			logger.Debugf("<PROCESS(%s)>:%s", filepath.Base(path), scanner.Text())
		}
	}
	go logPipe(procStdout)
	go logPipe(procStderr)

	// TODO: 未来建议改为事件等待，而不是 Sleep
	time.Sleep(3 * time.Second)

	shmKey := proc.Process.Pid
	shmId, err := unix.SysvShmGet(shmKey, 0, 0o666)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmGet")
	}
	shmData, err := unix.SysvShmAttach(shmId, 0, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmAttach")
	}

	img = &Image{
		Path:        path,
		virtualSize: size,
		format:      format,
		proc:        proc,
		shmKey:      shmKey,
		shmId:       shmId,
		shmSize:     shmSize,
		shmData:     shmData,
		efd:         efd,
	}

	go img.monitorQemuProcess()

	logger.Debugf("%s is opened", img.String())
	return img, nil
}

func (img *Image) String() string {
	return fmt.Sprintf("<IMAGE(name=%s,vsize=%v,fmt=%s)>", filepath.Base(img.Path), img.virtualSize, img.format)
}

func (img *Image) ReadAt(b []byte, off int64) (n int, err error) {
	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	if err = img.checkQemuProcessAlive(); err != nil {
		return 0, errors.Wrapf(err, "ReadAt")
	}

	remain := img.virtualSize - off
	if remain <= 0 {
		return 0, io.EOF
	}

	readLen := int64(len(b))
	if remain < readLen {
		readLen = remain
	}

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

		// 将请求写入共享内存
		copy(img.shmData[requestOffset:], req.Bytes())

		// 通知QEMU进程处理请求
		if _, err = unix.Write(img.efd, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			return totalRead, errors.Wrapf(err, "failed to notify QEMU process")
		}

		// 等待QEMU进程处理完成
		var buf [8]byte
		if _, err = unix.Read(img.efd, buf[:]); err != nil {
			img.shmMutex.Unlock()
			return totalRead, errors.Wrapf(err, "failed to wait for QEMU process")
		}

		// 从共享内存中读取响应
		respData := img.shmData[responseOffset:]
		resp, err := loadReadResponse(respData)
		if err != nil {
			img.shmMutex.Unlock()
			return totalRead, errors.Wrapf(err, "failed to read response")
		}

		if resp.ErrorCode != 0 {
			img.shmMutex.Unlock()
			return totalRead, errors.Errorf("QEMU read error: %d", resp.ErrorCode)
		}

		// 复制数据到用户缓冲区
		readSize := int(resp.Length)
		copy(b[totalRead:totalRead+readSize], respData[unsafe.Sizeof(readResponse{}):unsafe.Sizeof(readResponse{})+uintptr(readSize)])

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

		// 将请求写入共享内存
		copy(img.shmData[requestOffset:], req.Bytes())

		// 通知QEMU进程处理请求
		if _, err = unix.Write(img.efd, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			return totalWritten, errors.Wrapf(err, "failed to notify QEMU process")
		}

		// 等待QEMU进程处理完成
		var buf [8]byte
		if _, err = unix.Read(img.efd, buf[:]); err != nil {
			img.shmMutex.Unlock()
			return totalWritten, errors.Wrapf(err, "failed to wait for QEMU process")
		}

		// 从共享内存中读取响应
		respData := img.shmData[responseOffset:]
		resp, err := loadWriteResponse(respData)
		if err != nil {
			img.shmMutex.Unlock()
			return totalWritten, errors.Wrapf(err, "failed to unpack response")
		}

		if resp.ErrorCode != 0 {
			img.shmMutex.Unlock()
			return totalWritten, errors.Errorf("QEMU write error: %d", resp.ErrorCode)
		}

		// 更新已写入的字节数
		writtenSize := int(resp.Length)
		totalWritten += writtenSize

		img.shmMutex.Unlock()

		if writtenSize < chunkSize {
			break // 写入中断
		}
	}

	return totalWritten, nil
}

func (img *Image) Sync() error {
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

	// 将请求写入共享内存
	copy(img.shmData[requestOffset:], req.Bytes())

	// 通知QEMU进程处理请求
	if _, err := unix.Write(img.efd, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		return errors.Wrapf(err, "failed to notify QEMU process")
	}

	// 等待QEMU进程处理完成
	var buf [8]byte
	if _, err := unix.Read(img.efd, buf[:]); err != nil {
		return errors.Wrapf(err, "failed to wait for QEMU process")
	}

	// 从共享内存中读取响应
	respData := img.shmData[responseOffset:]
	resp, err := loadFlushResponse(respData)
	if err != nil {
		return errors.Wrapf(err, "failed to unpack response")
	}

	if resp.ErrorCode != 0 {
		return errors.Errorf("QEMU flush error: %d", resp.ErrorCode)
	}

	return nil
}

func (img *Image) Close() error {
	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	if extend.IsProcessRunning(img.proc) {
		if eSync := img.Sync(); eSync != nil {
			logger.Warnf("%s.Close() Sync: %v", img.String(), eSync)
		}

		// 发送Close指令
		img.shmMutex.Lock()
		req := closeRequest{
			shmBaseResponse: shmBaseResponse{
				shmBaseRequest: shmBaseRequest{
					Type:     _Close,
					Sequence: uint64(time.Now().UnixNano()),
				},
			},
		}

		// 将请求写入共享内存
		copy(img.shmData[requestOffset:], req.Bytes())

		// 通知QEMU进程处理请求
		if _, err := unix.Write(img.efd, []byte{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
			img.shmMutex.Unlock()
			logger.Warnf("%s.Close() notify QEMU: %v", img.String(), err)
		} else {
			// 等待QEMU进程处理完成
			var buf [8]byte
			if _, err := unix.Read(img.efd, buf[:]); err != nil {
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

	if img.efd == 0 {
		return nil
	}

	if eClose := unix.Close(img.efd); eClose != nil {
		return errors.Wrapf(eClose, "CloseEventfd2")
	}

	img.efd = 0
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
