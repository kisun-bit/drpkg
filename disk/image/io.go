//go:build linux

package image

import (
	"bufio"
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
	"golang.org/x/sys/unix"
)

type Image struct {
	Path string
	// VirtualSize 虚拟磁盘大小
	VirtualSize int64
	// Format 虚拟磁盘格式
	Format string

	// proc 托管的Qemu进程
	proc *exec.Cmd
	// rwLock 读写锁
	rwLock sync.Mutex

	opt openopt

	//
	// IPC
	//

	shmMutex    sync.Mutex
	shmId       int
	shmSize     int64
	shmAttached bool
	shmData     []byte

	//
	// 事件
	//

	efdMutex sync.Mutex
	efdr     int
	efdp     int
}

type openopt struct {
	debug bool
}

type OpenOption func(*openopt)

// EnableDebug 启用调试模式
func EnableDebug() OpenOption {
	return func(i *openopt) {
		i.debug = true
	}
}

// Open 打开虚拟磁盘文件
func Open(path string, opts ...OpenOption) (_ *Image, err error) {
	logger.Debugf("Start opening image: %s", path)

	absPath := path
	if !filepath.IsAbs(path) {
		absPath, err = filepath.Abs(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get absolute path for %s", path)
		}
	}
	path = absPath

	img := &Image{Path: path}
	for _, opt := range opts {
		opt(&img.opt)
	}
	defer func() {
		if err == nil {
			return
		}
		if e := releaseImageObject(img); e != nil {
			logger.Warnf("failed to release image: %s", e)
		}
	}()

	if err = checkQemuTool(); err != nil {
		return nil, err
	}

	if img.VirtualSize, img.Format, err = GetSizeAndFormat(img.Path); err != nil {
		return nil, err
	}
	logger.Debugf("Virtual size is %d, format is %s, pid is %d", img.VirtualSize, img.Format, os.Getpid())

	img.efdr, img.efdp, err = createEventfdPair()
	if err != nil {
		return nil, err
	}

	img.shmId, err = unix.SysvShmGet(os.Getpid(), shmSize, unix.IPC_CREAT|0o660)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmGet")
	}

	img.shmData, err = unix.SysvShmAttach(img.shmId, 0, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "SysvShmAttach(fd:%d)", img.shmId)
	}
	img.shmAttached = true
	img.shmSize = shmSize

	efdrFile := os.NewFile(uintptr(img.efdr), "eventfd_r")
	if efdrFile == nil {
		return nil, errors.New("failed to create os.File for eventfd(req)")
	}
	efdpFile := os.NewFile(uintptr(img.efdp), "eventfd_p")
	if efdpFile == nil {
		return nil, errors.New("failed to create os.File for eventfd(resp)")
	}

	procArgs := []string{
		"-f", absPath,
		"-r", strconv.Itoa(3),
		"-p", strconv.Itoa(4),
		"-s", strconv.Itoa(img.shmId),
	}
	if img.opt.debug {
		procArgs = append(procArgs, "-d")
	}

	img.proc = exec.Command(ioToolPath, procArgs...)
	img.proc.ExtraFiles = []*os.File{efdrFile, efdpFile}
	logger.Debugf("Qemu cmdline: `%s`", img.proc.String())

	procStdout, _ := img.proc.StdoutPipe()
	procStderr, _ := img.proc.StderrPipe()
	if err = img.proc.Start(); err != nil {
		return nil, errors.Wrapf(err, "start %s", ioToolPath)
	}
	logger.Debugf("Pid of %s: %d", ioToolPath, img.proc.Process.Pid)

	go img.onQemuExit()

	logPipe := func(tag string, rc io.ReadCloser) {
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Debugf("[QEMU] | %s: %s", tag, line)
		}
		_ = rc.Close()
	}
	go logPipe("stdout", procStdout)
	go logPipe("stderr", procStderr)

	// 等待Qemu进程始化完成并发送就绪信号
	logger.Debugf("Waiting for qemu process to be ready...")
	var readyBuf [8]byte
	n, err := unix.Read(img.efdp, readyBuf[:])
	if err != nil {
		return nil, errors.Wrapf(err, "failed to receive ready signal from C process")
	}
	readyValue := binary.LittleEndian.Uint64(readyBuf[:])
	logger.Debugf("Qemu process is ready (read %d bytes, value=%d)", n, readyValue)

	logger.Debugf("%s is opened", img.String())
	return img, nil
}

func (img *Image) String() string {
	return fmt.Sprintf("<IMAGE(name=%s,vsize=%v,fmt=%s)>", filepath.Base(img.Path), img.VirtualSize, img.Format)
}

func (img *Image) ReadAt(b []byte, off int64) (n int, err error) {
	img.debugf("%s.ReadAt() ++ off=%v", img.String(), off)
	defer img.debugf("%s.ReadAt() --", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	defer func() {
		if err == io.EOF {
			return
		}
		err = errors.Wrapf(err, "ReadAt")
	}()

	remain := img.VirtualSize - off
	if remain <= 0 {
		return 0, io.EOF
	}

	readLen := int64(len(b))
	if remain < readLen {
		readLen = remain
	}

	//
	// 发送读指令，每次最多读1MiB，若readLen的长度超过1MiB，则分多次读
	//

	var totalRead int
	for totalRead < int(readLen) {
		chunkSize := int(readLen) - totalRead
		if chunkSize > rwMaxLen {
			chunkSize = rwMaxLen
		}

		img.shmMutex.Lock()

		req := readRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _READ,
				Sequence: uint64(time.Now().UnixNano()),
			},
			Offset: off + int64(totalRead),
			Length: int32(chunkSize),
		}
		if err = img.sendRequest(&req); err != nil {
			img.shmMutex.Unlock()
			return 0, err
		}
		resp, err := loadReadResponse(img.shmData)
		if err != nil {
			img.shmMutex.Unlock()
			return 0, err
		}
		copy(b[totalRead:totalRead+int(resp.Length)], resp.ResponseBody[resp.DataRelStart:resp.DataRelStart+int(resp.Length)])

		img.shmMutex.Unlock()

		totalRead += int(resp.Length)
		if int(resp.Length) < chunkSize {
			break // 读取到EOF
		}
	}

	return totalRead, nil
}

func (img *Image) WriteAt(b []byte, off int64) (n int, err error) {
	img.debugf("%s.WriteAt() ++ off=%v, len=%v", img.String(), off, len(b))
	defer img.debugf("%s.WriteAt() --", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	defer func() {
		err = errors.Wrapf(err, "WriteAt")
	}()

	//
	// 发送写指令，每次最多写1MiB，若b的长度超过1MiB，则分多次写
	//

	var totalWritten int
	for totalWritten < len(b) {
		chunkSize := len(b) - totalWritten
		if chunkSize > rwMaxLen {
			chunkSize = rwMaxLen
		}

		img.shmMutex.Lock()

		req := writeRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _WRITE,
				Sequence: uint64(time.Now().UnixNano()),
			},
			Offset: off + int64(totalWritten),
			Length: int32(chunkSize),
			Data:   b[totalWritten : totalWritten+chunkSize],
		}
		if err = img.sendRequest(&req); err != nil {
			img.shmMutex.Unlock()
			return 0, err
		}
		resp, err := loadWriteResponse(img.shmData)
		if err != nil {
			img.shmMutex.Unlock()
			return 0, err
		}

		writtenSize := int(resp.Length)
		totalWritten += writtenSize

		img.shmMutex.Unlock()

		if writtenSize < chunkSize {
			break // 写入中断
		}
	}

	return totalWritten, nil
}

func (img *Image) Sync() (err error) {
	img.debugf("%s.Sync() ++", img.String())
	defer img.debugf("%s.Sync() --", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	defer func() {
		err = errors.Wrapf(err, "Sync")
	}()

	return img.flush()
}

func (img *Image) Close() (err error) {
	img.debugf("%s.Close() ++", img.String())
	defer img.debugf("%s.Close() --", img.String())
	defer logger.Debugf("%s is closed", img.String())

	img.rwLock.Lock()
	defer img.rwLock.Unlock()

	defer func() {
		err = errors.Wrapf(err, "Close")
	}()

	if extend.IsProcessRunning(img.proc) {
		if eSync := img.flush(); eSync != nil {
			return eSync
		}

		img.shmMutex.Lock()

		req := closeRequest{
			shmBaseRequest: shmBaseRequest{
				Type:     _Close,
				Sequence: uint64(time.Now().UnixNano()),
			},
		}
		if err = req.buildRequest(img.shmData); err != nil {
			img.shmMutex.Unlock()
			return err
		}
		// 只用通知事件，不用等待事件
		if err = img.notifyQemu(img.efdr); err != nil {
			img.shmMutex.Unlock()
			return err
		}

		img.shmMutex.Unlock()

		// 等待QEMU进程退出
		_ = img.proc.Wait()
	}

	return releaseImageObject(img)
}

func (img *Image) Size() int64 {
	stat, err := os.Stat(img.Path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

func (img *Image) debugf(format string, args ...interface{}) {
	if !img.opt.debug {
		return
	}
	logger.Debugf(format, args...)
}

func (img *Image) flush() (err error) {
	img.shmMutex.Lock()
	defer img.shmMutex.Unlock()

	req := flushRequest{
		shmBaseRequest: shmBaseRequest{
			Type:     _FLUSH,
			Sequence: uint64(time.Now().UnixNano()),
		},
	}
	if err = img.sendRequest(&req); err != nil {
		return err
	}
	if _, err = loadFlushResponse(img.shmData); err != nil {
		return err
	}
	return nil
}

func (img *Image) sendRequest(req shmRequest) (err error) {
	if err = req.buildRequest(img.shmData); err != nil {
		return err
	}
	if err = img.notifyQemu(img.efdr); err != nil {
		return err
	}
	if err = img.waitQemu(img.efdp); err != nil {
		return err
	}
	return nil
}

// notifyQemu 通知QEMU进程处理请求
func (img *Image) notifyQemu(eventfd int) error {
	img.debugf("%s.notifyQemu() ++ event(%d)", img.String(), eventfd)
	defer img.debugf("%s.notifyQemu() -- event(%d)", img.String(), eventfd)

	if _, err := unix.Write(eventfd, eventSignalBytes); err != nil {
		return errors.Wrapf(err, "failed to notify qemu process")
	}

	return nil
}

// waitQemu 等待QEMU进程处理完毕
func (img *Image) waitQemu(eventfd int) error {
	img.debugf("%s.waitQemu() ++ event(%d)", img.String(), eventfd)
	defer img.debugf("%s.waitQemu() -- event(%d)", img.String(), eventfd)

	var buf [8]byte
	if _, err := unix.Read(eventfd, buf[:]); err != nil {
		return errors.Wrapf(err, "waitQemu")
	}

	if exited, _ := img.getQemuExitStat(); exited {
		return errors.New("waitQemu: qemu process exited unexpectedly")
	}
	return nil
}

func (img *Image) getQemuExitStat() (exited bool, code int) {
	if img.proc.ProcessState != nil {
		return true, img.proc.ProcessState.ExitCode()
	}
	return false, -1
}

func (img *Image) onQemuExit() {
	_ = img.proc.Wait()

	if _, code := img.getQemuExitStat(); code != 0 {
		// 预写一个，避免 waitQemuEx 因时序性调用问题发生阻塞
		_ = img.notifyQemu(img.efdp)
		//_ = img.notifyQemuEx(img.efdr)
	}
}

func (img *Image) checkQemuAlive() error {
	if exited, _ := img.getQemuExitStat(); exited {
		return errors.New("checkQemuAlive: qemu process exited unexpectedly")
	}
	return nil
}

func createEventfdPair() (req, resp int, err error) {
	efdr, err := createEventfd("request")
	if err != nil {
		return 0, 0, err
	}
	efdp, err := createEventfd("response")
	if err != nil {
		_ = unix.Close(efdr)
		return 0, 0, err
	}
	return efdr, efdp, nil
}

func createEventfd(name string) (int, error) {
	fd, err := unix.Eventfd(0, unix.EFD_SEMAPHORE)
	if err != nil {
		return -1, errors.Wrapf(err, "failed to open eventfd(%s)", name)
	}
	return fd, nil
}

func releaseImageObject(img *Image) error {
	if img == nil {
		return nil
	}
	if img.efdr > 0 {
		if err := unix.Close(img.efdr); err != nil {
			return errors.Wrapf(err, "failed to close efdr(%d)", img.efdr)
		}
		img.efdr = 0
	}
	if img.efdp > 0 {
		if err := unix.Close(img.efdp); err != nil {
			return errors.Wrapf(err, "failed to close efdp(%d)", img.efdp)
		}
		img.efdp = 0
	}

	// 更多释放逻辑...

	img.shmMutex.Lock()
	defer img.shmMutex.Unlock()

	if img.shmAttached {
		if err := unix.SysvShmDetach(img.shmData); err != nil {
			return errors.Wrapf(err, "failed to attach shm(%d)", img.shmId)
		}
		img.shmData = nil
		img.shmAttached = false
	}
	if img.shmId > 0 {
		if _, err := unix.SysvShmCtl(img.shmId, unix.IPC_RMID, nil); err != nil {
			return errors.Wrapf(err, "failed to remove shm(%d)", img.shmId)
		}
		img.shmId = 0
	}

	return nil
}
