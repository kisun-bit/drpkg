//go:build linux

package qcow2

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// QemuIOManager qemu-io句柄.
type QemuIOManager struct {
	Image        string
	ImageType    string
	IOMode       QemuIOCacheMode
	WriteThrough bool
	EnableAIO    bool

	extCfg qemuExtCfg

	GeneralProperties ImgGeneralInfo

	ctx    context.Context
	cancel context.CancelFunc

	mutex       sync.RWMutex
	rwSerMutex  sync.Mutex
	releaseOnce sync.Once

	// startedProcess 标识qemu-iow进程已启动的信号.
	startedProcess chan struct{}

	// exitedProcess 标识qemu-iow进程已退出的信号.
	exitedProcess chan struct{}

	// processHandle qemu-iow进程实例.
	processHandle *exec.Cmd

	// processStdin qemu-iow进程的stdin管道.
	processStdin io.WriteCloser

	// processStdout qemu-iow进程的stdout管道.
	processStdout io.ReadCloser

	// processStderr qemu-iow进程的stderr管道.
	processStderr io.ReadCloser

	// processStderrBuffer qemu-iow的错误输出.
	processStderrBuffer bytes.Buffer

	err error
}

// EnableRWSerialAccess 读写接口(ReadAt、WriteAt)串行互斥执行. 一般用于fuse寻址的场景.
func EnableRWSerialAccess() QemuIOOption {
	return func(cfg *qemuExtCfg) {
		cfg.rwSerial = true
	}
}

// NewQemuIOManager 基于指定的镜像文件初始化一个qemu-io句柄.
// NOTE：每初始化一个qemu-io句柄，就会产生一个qemu-iow进程.
func NewQemuIOManager(ctx context.Context, image, imageType string, mode QemuIOCacheMode, options ...QemuIOOption) (
	qw *QemuIOManager, err error) {
	if _, err = os.Stat(image); err != nil {
		return nil, err
	}
	if imageType == "" {
		return nil, errors.New("image type must be specified")
	}

	qw = &QemuIOManager{
		Image:          image,
		ImageType:      imageType,
		IOMode:         mode,
		startedProcess: make(chan struct{}, 1),
		exitedProcess:  make(chan struct{}, 1),
	}
	qw.ctx, qw.cancel = context.WithCancel(ctx)

	for _, opt := range options {
		opt(&qw.extCfg)
	}

	if err = qw.setupIOMode(mode); err != nil {
		return nil, errors.Wrapf(err, "setup io mode")
	}
	qw.GeneralProperties, err = GeneralInfoImage(image)
	if err != nil {
		return nil, errors.Wrapf(err, "query information of qcow2")
	}

	logger.Infof("NewQemuIOManager %s has been initialized", qw.String())
	return qw, nil
}

func (qw *QemuIOManager) String() string {
	return fmt.Sprintf("<QemuIOManager(file=%s,type=%s,mode=%s)>",
		qw.Image, qw.ImageType, qw.IOMode.String())
}

func (qw *QemuIOManager) Open() error {
	go qw.open()
	qw.waitUntilStartedProcess()
	return qw.Error()
}

func (qw *QemuIOManager) WriteAt(buf []byte, off int64) (n int, err error) {
	if qw.extCfg.rwSerial {
		qw.rwSerMutex.Lock()
		defer qw.rwSerMutex.Unlock()
	}
	if isNil(qw.processStdin) {
		return 0, errors.Errorf("can not write to an empty pipe")
	}
	if len(buf) == 0 {
		return 0, nil
	}
	if off >= qw.GeneralProperties.VirtualSize || off+int64(len(buf)) > qw.GeneralProperties.VirtualSize {
		return 0, errors.Errorf("write %v bytes at %v: out of bounds", len(buf), off)
	}
	if err = qw.checkErrorForRW(); err != nil {
		return 0, errors.Wrapf(err, "WriteAt")
	}
	block := &qemuBlock{
		Type:    blockTypeWrite,
		Off:     uint64(off),
		Len:     uint64(len(buf)),
		Payload: buf,
	}
	err = block.Write(qw.processStdin)
	n = len(buf)
	return n, errors.Wrapf(err, "write at offset(%v)", off)
}

func (qw *QemuIOManager) ReadAt(buf []byte, off int64) (n int, err error) {
	if qw.extCfg.rwSerial {
		qw.rwSerMutex.Lock()
		defer qw.rwSerMutex.Unlock()
	}
	if isNil(qw.processStdin) || isNil(qw.processStdout) {
		return 0, errors.Errorf("empty pipe")
	}
	if len(buf) == 0 {
		return 0, nil
	}
	if off >= qw.GeneralProperties.VirtualSize {
		return 0, errors.Errorf("read %v bytes at %v: out of bounds", len(buf), off)
	}
	if err = qw.checkErrorForRW(); err != nil {
		return 0, errors.Wrapf(err, "ReadAt")
	}
	req := &qemuRequestBlock{
		blockTypeRead,
		uint64(off),
		uint64(len(buf)),
	}
	// 此处一写一读为原子操作, 需保证出于互斥环境中.
	if err = req.Write(qw.processStdin); err != nil {
		return 0, errors.Wrapf(err, "ReadAt->Write(...), failed to send read request")
	}
	blk, er := readBlock(qw.processStdout)
	if er != nil && err != io.EOF {
		return 0, errors.Wrapf(er, "ReadAt->readBlock(...), off=%v, len-buffer=%d", off, len(buf))
	}
	copy(buf, blk.Payload)
	if n = int(blk.Len); n == 0 {
		err = io.EOF
	}
	return n, err
}

func (qw *QemuIOManager) Close() (err error) {
	defer func() {
		qw.setError(err)
	}()

	logger.Debugf("%s.Close called", qw.String())

	if err = qw.closePipe(qw.processStdin); err != nil {
		logger.Warnf("%s.Close failed to close stdin pipe: %v", qw.String(), err)
	}
	if err = qw.closePipe(qw.processStdout); err != nil {
		logger.Warnf("%s.Close failed to close stdout pipe: %v", qw.String(), err)
	}

	qw.waitUntilExitedProcess()
	logger.Debugf("%s.Close sub-process exit...", qw.String())

	qw.cancel()
	qw.wait()
	return err
}

func (qw *QemuIOManager) Error() error {
	qw.mutex.RLock()
	defer qw.mutex.RUnlock()
	return qw.err
}

func (qw *QemuIOManager) QemuProcOutput() string {
	return string(qw.processStderrBuffer.Bytes())
}

func (qw *QemuIOManager) setupIOMode(mode QemuIOCacheMode) error {
	switch mode {
	case Direct:
		qw.WriteThrough, qw.EnableAIO = true, false
	case DirectWithAio:
		qw.WriteThrough, qw.EnableAIO = true, true
	case Writeback:
		qw.WriteThrough, qw.EnableAIO = false, false
	case WritebackWithAio:
		qw.WriteThrough, qw.EnableAIO = false, true
	default:
		return errors.Errorf("unsupported io-mode: %v", mode)
	}
	return nil
}

func (qw *QemuIOManager) open() {
	var err error

	defer qw.markExitedProcess()

	defer func() {
		qw.setError(err)
	}()

	cmdline := qw.generateProcessCmd()

	qw.processHandle = exec.CommandContext(qw.ctx, "sh", "-c", cmdline)
	qw.processStdin, err = qw.processHandle.StdinPipe()
	if err != nil {
		err = errors.Wrapf(err, "StdinPipe")
		return
	}
	qw.processStdout, err = qw.processHandle.StdoutPipe()
	if err != nil {
		err = errors.Wrapf(err, "StdoutPipe")
		return
	}
	qw.processStderr, err = qw.processHandle.StderrPipe()
	if err != nil {
		err = errors.Wrapf(err, "StderrPipe")
		return
	}
	if err = qw.processHandle.Start(); err != nil {
		err = errors.Wrapf(err, "Start")
		return
	}

	qw.markStartedProcess()
	logger.Debugf("sub-process(`%s`) start...", cmdline)

	go qw.copyProcessStderr()

	if err = qw.processHandle.Wait(); err != nil {
		err = errors.Wrapf(err, "Wait")
	}
	logger.Debugf("sub-process(`%s`) stderr: \n%s", cmdline, string(qw.processStderrBuffer.Bytes()))
}

func (qw *QemuIOManager) checkErrorForRW() error {
	if qw.isCancelled() {
		return errors.New("context cancelled")
	}
	if qw.isErrorOccurred() {
		return qw.Error()
	}
	return nil
}

func (qw *QemuIOManager) copyProcessStderr() {
	defer func() {
		_ = recover()
	}()
	_, _ = io.Copy(&qw.processStderrBuffer, qw.processStderr)
	_ = qw.processStderr.Close()
}

func (qw *QemuIOManager) generateProcessCmd() string {
	cmdline := fmt.Sprintf("%s -f %s -F %s", _QemuIoWExecPath, qw.Image, qw.ImageType)
	if qw.EnableAIO {
		cmdline += " -a"
	}
	if qw.WriteThrough {
		cmdline += " -n"
	}
	return cmdline
}

func (qw *QemuIOManager) closePipe(pipe io.Closer) error {
	if !isNil(pipe) && !isNil(qw.processHandle) {
		err := pipe.Close()
		if err != nil {
			if strings.Contains(err.Error(), "file already closed") {
				return nil
			}
		}
	}
	return nil
}

func (qw *QemuIOManager) markStartedProcess() {
	qw.startedProcess <- struct{}{}
}

func (qw *QemuIOManager) waitUntilStartedProcess() {
	<-qw.startedProcess
}

func (qw *QemuIOManager) markExitedProcess() {
	qw.exitedProcess <- struct{}{}
}

func (qw *QemuIOManager) waitUntilExitedProcess() {
	<-qw.exitedProcess
}

func (qw *QemuIOManager) wait() {
	for {
		select {
		case <-qw.ctx.Done():
			qw.release()
			return
		}
	}
}

func (qw *QemuIOManager) release() {
	qw.releaseOnce.Do(func() {
		logger.Debugf("%s.release called", qw.String())
	})
}

func (qw *QemuIOManager) isErrorOccurred() bool {
	qw.mutex.RLock()
	defer qw.mutex.RUnlock()
	return qw.err != nil
}

func (qw *QemuIOManager) isCancelled() bool {
	return isCancelled(qw.ctx)
}

func (qw *QemuIOManager) setError(err error) {
	if err == nil || err == io.EOF {
		return
	}
	qw.mutex.Lock()
	defer qw.mutex.Unlock()
	if qw.err != nil {
		return
	}
	qw.err = err
	logger.Errorf("%s.setError err=%v", qw.String(), err)
}

func (qw *QemuIOManager) resetError() {
	qw.mutex.Lock()
	defer qw.mutex.Unlock()
	qw.err = nil
}
