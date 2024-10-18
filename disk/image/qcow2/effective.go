//go:build linux

package qcow2

import (
	"bufio"
	"context"
	"fmt"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/panjf2000/ants/v2"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"unicode"
)

// ImageEffectiveReader QCow2磁盘镜像文件的有效数据读取器.
type ImageEffectiveReader struct {
	Image     string
	ImageType string

	extCfg effectReaderExtCfg

	ctx    context.Context
	cancel context.CancelFunc

	rch chan EffectBlock

	proc             *exec.Cmd
	procOutputBuffer io.ReadCloser
	procStarted      chan struct{}
	procExited       chan struct{}

	fileHandleCache sync.Map

	readGoPool   *ants.PoolWithFunc
	readGoPoolWG sync.WaitGroup

	closed       bool
	rchCloseOnce sync.Once

	errMutex sync.RWMutex
	err      error
}

// WithCheck 运行qemu-img check命令检测镜像文件的合法性.
func WithCheck() EffectReaderOption {
	return func(cfg *effectReaderExtCfg) {
		cfg.check = true
	}
}

// WithReadQueueSize 设置读块的管道的缓存大小(默认为512,上限为 _MaxQueueSizeForEffectReader)
func WithReadQueueSize(size int) EffectReaderOption {
	if size <= 0 {
		size = _DefaultQueueSizeForEffectReader
	}
	if size > _MaxQueueSizeForEffectReader {
		size = _MaxQueueSizeForEffectReader
	}
	return func(cfg *effectReaderExtCfg) {
		cfg.rChSize = size
	}
}

// WithBlockSize 设置读块的大小(默认为1MiB, 上限为 _MaxBlockSizeForEffectReader)
func WithBlockSize(bs int) EffectReaderOption {
	if bs <= 0 {
		bs = _DefaultBlockSizeForEffectReader
	}
	if bs > _MaxBlockSizeForEffectReader {
		bs = _MaxBlockSizeForEffectReader
	}
	return func(cfg *effectReaderExtCfg) {
		cfg.bs = bs
	}
}

// WithReadCores 设置读并发数(默认为 runtime.NumCPU(), 上限为 runtime.NumCPU()).
func WithReadCores(cores int) EffectReaderOption {
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	if cores > runtime.NumCPU() {
		cores = runtime.NumCPU()
	}
	return func(cfg *effectReaderExtCfg) {
		cfg.readCores = runtime.NumCPU()
	}
}

func NewImageEffectiveReader(ctx context.Context, image, imageType string, options ...EffectReaderOption) (
	er *ImageEffectiveReader, err error) {

	if _, err = os.Stat(image); os.IsNotExist(err) {
		return nil, err
	}

	er = &ImageEffectiveReader{
		Image:     image,
		ImageType: imageType,
	}
	er.ctx, er.cancel = context.WithCancel(ctx)

	er.defaultExtCfgSetup()
	for _, opt := range options {
		opt(&er.extCfg)
	}
	if er.extCfg.check {
		if err = CheckImage(er.Image, er.ImageType); err != nil {
			return nil, errors.Wrap(err, "check image")
		}
	}
	er.rch = make(chan EffectBlock, er.extCfg.rChSize)
	er.procExited = make(chan struct{}, 1)
	er.procStarted = make(chan struct{}, 1)

	if er.readGoPool, err = ants.NewPoolWithFunc(er.extCfg.readCores, er.readFunc); err != nil {
		return nil, err
	}

	go er.runMapProc()
	er.waitUntilMapProcStart()
	return er, nil
}

func (er *ImageEffectiveReader) String() string {
	return fmt.Sprintf("<ImageEffectiveReader(image=%s,type=%s)>", er.Image, er.ImageType)
}

func (er *ImageEffectiveReader) Blocks() chan EffectBlock {
	return er.rch
}

func (er *ImageEffectiveReader) Close() error {
	if er.closed {
		return errors.Errorf("%s has already closed", er.String())
	}
	er.closeRch()

	// 清理管道残余.
	for range er.rch {
	}

	er.closeAllFileHandle()
	er.delAllFileHandle()
	er.waitUntilMapProcExit()
	er.cancel()
	er.wait()

	er.closed = true
	return nil
}

func (er *ImageEffectiveReader) Error() error {
	er.errMutex.RLock()
	defer er.errMutex.RUnlock()
	return er.err
}

func (er *ImageEffectiveReader) defaultExtCfgSetup() {
	er.extCfg.check = false
	er.extCfg.bs = _DefaultBlockSizeForEffectReader
	er.extCfg.rChSize = _DefaultQueueSizeForEffectReader
	er.extCfg.readCores = runtime.NumCPU()
}

func (er *ImageEffectiveReader) runRead() {
	scanner := bufio.NewScanner(er.procOutputBuffer)

	lineCount := int64(0)
	parseFunc := er.parseOneMapInfo
	for scanner.Scan() {
		if er.Error() != nil || isCancelled(er.ctx) {
			break
		}
		mapLine := strings.TrimSpace(scanner.Text())
		if len(mapLine) == 0 {
			continue
		}
		lineCount++
		fields := strings.Fields(mapLine)
		if lineCount == 1 {
			if len(fields) == 5 &&
				fields[0] == "Offset" &&
				fields[1] == "Length" &&
				fields[2] == "Mapped" &&
				fields[3] == "to" &&
				fields[4] == "File" {
				parseFunc = er.parseOneMapInfo
				continue
			} else {
				err := errors.Errorf("unsuported map header `%s`", mapLine)
				er.setError(err)
				return
			}
		}
		info, ok, err := parseFunc(mapLine)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse map line `%s`", mapLine)
			er.setError(err)
			return
		}
		if !ok {
			continue
		}
		er.invoke(info)
	}

	if err := scanner.Err(); err != nil {
		err = errors.Wrap(err, "scanner error")
		er.setError(err)
	}

	er.readGoPoolWG.Wait()
	er.closeRch()
}

func (er *ImageEffectiveReader) runMapProc() {
	var err error
	defer er.setMapProcExited()
	defer func() {
		er.setError(err)
	}()

	cmdLine := er.generateCmdLine()
	er.proc = exec.CommandContext(er.ctx, "sh", "-c", cmdLine)
	//er.proc.Stdout = &er.procOutputBuffer
	if er.procOutputBuffer, err = er.proc.StdoutPipe(); err != nil {
		logger.Errorf("%s.runMapProc StdoutPipe: %v", er.String(), err)
		return
	}
	if err = er.proc.Start(); err != nil {
		logger.Errorf("%s.runMapProc Start: %v", er.String(), err)
		return
	}
	er.setMapProcStarted()
	er.runRead()
	if err = er.proc.Wait(); err != nil {
		logger.Errorf("%s.runMapProc Wait: %v", er.String(), err)
		return
	}
}

func (er *ImageEffectiveReader) waitUntilMapProcExit() {
	<-er.procExited
}

func (er *ImageEffectiveReader) setMapProcExited() {
	er.procExited <- struct{}{}
}

func (er *ImageEffectiveReader) waitUntilMapProcStart() {
	<-er.procStarted
}

func (er *ImageEffectiveReader) setMapProcStarted() {
	er.procStarted <- struct{}{}
}

func (er *ImageEffectiveReader) generateCmdLine() string {
	return fmt.Sprintf("%s map %s -f %s", _QemuImgExecPath, er.Image, er.ImageType)
}

func (er *ImageEffectiveReader) setError(err error) {
	er.errMutex.Lock()
	defer er.errMutex.Unlock()
	if err == nil || err == io.EOF {
		return
	}
	if er.err != nil {
		return
	}
	er.err = err
	er.closeRch()
	logger.Errorf("%s.setError err=%v", er.String(), err)
}

func (er *ImageEffectiveReader) wait() {
	for {
		select {
		case <-er.ctx.Done():
			er.readGoPoolWG.Wait()
			return
		}
	}
}

func (er *ImageEffectiveReader) closeRch() {
	er.rchCloseOnce.Do(func() {
		close(er.rch)
	})
}

func (er *ImageEffectiveReader) parseOneMapInfo(mapLine string) (info qemuMapBlockInfo, ok bool, err error) {
	//
	// qemu-img map输出示例：
	// Offset          Length          Mapped to       File
	// 0               0x6e00000       0x50000         full.qcow2
	//
	fields := strings.Fields(mapLine)
	if len(mapLine) == 0 || !unicode.IsDigit(rune(mapLine[0])) || len(fields) != 4 {
		return qemuMapBlockInfo{}, false, nil
	}
	if info.DiskOffset, err = strToInt64(fields[0]); err != nil {
		return qemuMapBlockInfo{}, false, err
	}
	if info.Length, err = strToInt64(fields[1]); err != nil {
		return qemuMapBlockInfo{}, false, err
	}
	if info.MappedTo, err = strToInt64(fields[2]); err != nil {
		return qemuMapBlockInfo{}, false, err
	}
	info.File = fields[3]
	return info, true, nil
}

func (er *ImageEffectiveReader) getOrStoreOneFileHandle(file string) (*os.File, error) {
	// 双重load, 避免文件句柄资源泄漏.
	if v, ok := er.fileHandleCache.Load(file); ok {
		return v.(*os.File), nil
	}
	h, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	v, loaded := er.fileHandleCache.LoadOrStore(file, h)
	if loaded {
		_ = h.Close()
	}
	if isNil(v) {
		return nil, errors.Errorf("why an nil value for %s stored in file-handle cache", file)
	}
	return v.(*os.File), nil
}

func (er *ImageEffectiveReader) delOneFileHandle(file string) {
	er.fileHandleCache.Delete(file)
}

func (er *ImageEffectiveReader) getOneFileHandle(file string) (handle *os.File, ok bool) {
	v, ok := er.fileHandleCache.Load(file)
	if ok {
		return v.(*os.File), true
	}
	return nil, false
}

func (er *ImageEffectiveReader) closeAllFileHandle() {
	er.fileHandleCache.Range(func(key, value any) bool {
		handle := value.(*os.File)
		eClose := handle.Close()
		logger.Debugf("%s.closeAllFileHandle closed `%v`, err=%v", er.String(), key, eClose)
		return true
	})
}

func (er *ImageEffectiveReader) delAllFileHandle() {
	handleKeys := make([]string, 0)
	er.fileHandleCache.Range(func(key, value any) bool {
		handleKeys = append(handleKeys, key.(string))
		return true
	})
	for _, key := range handleKeys {
		er.fileHandleCache.Delete(key)
		logger.Debugf("%s.closeAllFileHandle del `%v`", er.String(), key)
	}
}

func (er *ImageEffectiveReader) readFunc(arg interface{}) {
	defer er.readGoPoolWG.Done()
	if er.Error() != nil || isCancelled(er.ctx) {
		return
	}

	var err error
	defer func() {
		er.setError(err)
	}()

	info := arg.(qemuMapBlockInfo)
	handle, e := er.getOrStoreOneFileHandle(info.File)
	if e != nil {
		logger.Errorf("%s.readFunc getOrStoreOneFileHandle: %v", er.String(), e)
		err = e
		return
	}

	blkCnt := info.Length / int64(er.extCfg.bs)
	leftBytes := info.Length % int64(er.extCfg.bs)

	fileOff := info.MappedTo
	DiskOff := info.DiskOffset
	step := er.extCfg.bs
	for i := int64(0); i <= blkCnt; i++ {
		if er.Error() != nil || isCancelled(er.ctx) {
			return
		}
		// 最后一个分块.
		if i == blkCnt {
			step = int(leftBytes)
		}
		if step == 0 {
			return
		}
		block := new(EffectBlock)
		block.Off = DiskOff
		block.Payload = make([]byte, step)
		n, er_ := handle.ReadAt(block.Payload, fileOff)
		if er_ != nil {
			err = er_
			logger.Errorf("%s.readFunc Read: %v", er.String(), err)
			return
		}
		if n != step {
			err = errors.New("insufficient read length")
			logger.Errorf("%s.readFunc Read: %v", er.String(), err)
			return
		}
		fileOff += int64(step)
		DiskOff += int64(step)
		er.rch <- *block
	}
}

func (er *ImageEffectiveReader) invoke(info qemuMapBlockInfo) {
	var err error
	defer func() {
		if err != nil {
			logger.Errorf("%s.invoke error: %v", er.String(), err)
			er.setError(err)
			er.readGoPoolWG.Done()
		}
	}()
	er.readGoPoolWG.Add(1)
	err = er.readGoPool.Invoke(info)
	return
}
