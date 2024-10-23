package fossick

import (
	"context"
	"github.com/panjf2000/ants/v2"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"io"
	"os"
	"sync"
)

type readAtCloser interface {
	io.ReaderAt
	io.Closer
}

// GetFilesystemType 获取指定文件系统(支持有效数据提取)的类型.
func GetFilesystemType(filesystemPath string) (fsType Filesystem, err error) {
	fp, eOpen := os.Open(filesystemPath)
	if eOpen != nil {
		return "", eOpen
	}
	defer func(fp *os.File) {
		err = fp.Close()
	}(fp)
	return GetFilesystemTypeByStream(fp)
}

func GetFilesystemTypeByStream(stream readAtCloser) (fsType Filesystem, err error) {
	compatibleHeader := make([]byte, 2<<10)
	_, err = stream.ReadAt(compatibleHeader, 0)
	if err != nil {
		return "", err
	}
	extSB := compatibleHeader[1024:]
	if string(extSB[56:56+len(EXTMagic)]) == EXTMagic {
		return EXT, nil
	}
	if string(compatibleHeader[0x20:0x20+len(OracleDiskMagic)]) == OracleDiskMagic {
		return OracleASM, nil
	}
	if string(compatibleHeader[80:80+len(FAT32Magic)]) == FAT32Magic {
		return FAT, nil
	}
	if string(compatibleHeader[:len(XFSMagic)]) == XFSMagic {
		return XFS, nil
	}
	if string(compatibleHeader[:len(NTFSMagic)]) == NTFSMagic {
		return NTFS, nil
	}
	if string(compatibleHeader[:len(BTRFSMagic)]) == BTRFSMagic {
		return BTRFS, nil
	}
	if string(compatibleHeader[:len(JFSMagic)]) == JFSMagic {
		return JFS, nil
	}
	if string(compatibleHeader[:len(APFSMagic)]) == APFSMagic {
		return APFS, nil
	}
	if string(compatibleHeader[:len(ZFSMagic)]) == ZFSMagic {
		return ZFS, nil
	}
	return "", errors.Errorf("can not detect filesystem")
}

// GetFilesystemClusterSize 获取指定文件系统的块大小(一个inode、簇、块等).
func GetFilesystemClusterSize(filesystemPath string) (clusterSize int, err error) {
	return
}

// CopyFilesystem 复制文件系统.
func CopyFilesystem(source, dest string, sourceBitmap []byte, blockSize int) (int64, error) {
	sourceFile, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destFile, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = destFile.Close()
	}()

	var bytesCopied int64
	for i, bit := range sourceBitmap {
		for j := 0; j < 8; j++ {
			if bit&(1<<uint(7-j)) != 0 {
				buffer := make([]byte, blockSize)
				addr := int64(i*8+j) * int64(blockSize)
				rn, er := sourceFile.ReadAt(buffer, addr)
				if er != nil {
					if er == io.EOF {
						break
					}
					return 0, er
				}
				_, ew := destFile.WriteAt(buffer[:rn], addr)
				if ew != nil {
					return 0, ew
				}
				bytesCopied += int64(rn)
			}
		}
	}

	return bytesCopied, nil
}

func CopyFileSystemWithHashAndBlockSize(ctx context.Context, logger *zap.SugaredLogger,
	source, dest, referHashFile, destHashFile string, blockSize, readcores, writecores int) (n int64, err error) {
	var (
		referHashFileHandle, destHashFileHandle, destDevice *os.File
		writePoolWg                                         sync.WaitGroup
	)
	if logger == nil {
		logger = logger
	}
	if referHashFile != "" {
		referHashFileHandle, err = os.Open(referHashFile)
		if err != nil {
			return 0, err
		}
		defer referHashFileHandle.Close()
	}
	if destHashFile != "" {
		destHashFileHandle, err = os.OpenFile(destHashFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
		if err != nil {
			return 0, err
		}
		defer destHashFileHandle.Close()
	}
	destDevice, err = os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return 0, err
	}
	writeDestDeviceFunc := func(i interface{}) {
		defer writePoolWg.Done()
		ed := i.(EffectiveData)
		_, err = destDevice.WriteAt(ed.Bytes, ed.Offset)
		if err != nil {
			logger.Errorf("CopyFileSystemWithHashAndBlockSize failed to write at %v", ed.Offset)
		}
	}
	writePool, err := ants.NewPoolWithFunc(writecores, writeDestDeviceFunc)
	if err != nil {
		logger.Errorf("CopyFileSystemWithHashAndBlockSize can not init write pool")
		return 0, err
	}
	edr, err := NewEffectiveDataReader(
		ctx,
		"mirror_fs",
		logger,
		source,
		referHashFileHandle,
		destHashFileHandle,
		blockSize,
		readcores)
	if err != nil {
		logger.Errorf("CopyFileSystemWithHashAndBlockSize NewEffectiveDataReader err=%v", err)
		return 0, err
	}
	defer edr.Release()

	for ed := range edr.DataChannel() {
		if err != nil {
			logger.Errorf("CopyFileSystemWithHashAndBlockSize range DataChannel err=%v", err)
			return 0, err
		}
		n += int64(ed.Length)
		writePoolWg.Add(1)
		err = writePool.Invoke(ed)
		if err != nil {
			writePoolWg.Done()
			logger.Errorf("CopyFileSystemWithHashAndBlockSize write invoke err=%v", err)
			return 0, err
		}
	}
	writePoolWg.Wait()
	if edr.Error() != nil {
		err = edr.Error()
		logger.Errorf("CopyFileSystemWithHashAndBlockSize EffectiveDataReader err=%v", err)
	} else {
		logger.Debugf("Hash signature is %v", edr.BitmapIter.GetFsHashSignature())
		logger.Debugf("EffetiveBlocks is %v, IncrEffectBlocks is %v", edr.EffectBlockCount(), edr.IncrEffectBlockCount())
	}
	return n, err
}
