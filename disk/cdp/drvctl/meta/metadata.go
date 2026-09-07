package meta

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"

	"github.com/lunixbochs/struc"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
)

type Metadata interface {
	Path() string
	Header() Header
	Extents() []FileDiskExtentSegment
	Destroy() error
}

type metadata struct {
	path    string
	header  Header
	extents []FileDiskExtentSegment
}

func Load(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h, err := loadHeader(f)
	if err != nil {
		return nil, err
	}

	_m := new(metadata)
	_m.path = path
	_m.header = h

	_m.extents, err = FileDiskExtents(path)
	if err != nil {
		return nil, err
	}
	//fmt.Println(DumpExtents(_m.extents))

	if err = _m.check(); err != nil {
		return nil, err
	}

	return _m, nil
}

func CreateDefault(path string) (Metadata, error) {
	return Create(path, defaultMaxDiskCount, defaultMaxDiskSize, false)
}

func CreateLinear(
	path string,
	start uint64,
	maxDiskCount uint32,
	maxDiskSize uint64,
) (Metadata, error) {

	const (
		HeaderSize   = 1 << 20  // 1MB
		WriteChunkSz = 64 << 10 // 64KB
	)

	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := new(metadata)
	m.path = path
	metaStart := start

	// 1. 生成 Header
	m.header, err = GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return nil, err
	}

	// 2. 序列化 Header（只写 struct，不污染 buffer）
	headerBuf := new(bytes.Buffer)
	if err := struc.Pack(headerBuf, &m.header); err != nil {
		return nil, err
	}

	headerData := headerBuf.Bytes()
	if len(headerData) > HeaderSize {
		return nil, fmt.Errorf("header too large: %d > %d", len(headerData), HeaderSize)
	}

	// 3. 写 Header 区（struct + padding）
	headerBlock := make([]byte, HeaderSize)
	copy(headerBlock, headerData)

	for off := 0; off < HeaderSize; off += WriteChunkSz {
		end := off + WriteChunkSz
		if end > HeaderSize {
			end = HeaderSize
		}
		_, err := f.WriteAt(headerBlock[off:end], int64(start))
		if err != nil {
			return nil, err
		}
		start += uint64(end - off)
	}

	// 4. 写 Bitmap 区
	bytesPerBitmap := m.header.BitsPerDiskBitmap / 8
	totalBitmapBytes := uint64(bytesPerBitmap) * uint64(m.header.MaxDiskCount)

	zeroChunk := make([]byte, WriteChunkSz)

	written := uint64(0)
	for written < totalBitmapBytes {
		remain := totalBitmapBytes - written
		writeLen := uint64(len(zeroChunk))
		if remain < writeLen {
			writeLen = remain
		}

		_, err := f.WriteAt(zeroChunk[:writeLen], int64(start))
		if err != nil {
			return nil, err
		}

		start += writeLen
		written += writeLen
	}

	// 5. 记录 extent（真实占用大小）
	m.extents = []FileDiskExtentSegment{
		{
			path,
			int64(metaStart),
			int64(HeaderSize + totalBitmapBytes),
		},
	}

	return m, nil
}

// Create 创建CDP元数据缓存文件
// 特别说明：switchPreStageWorkMode 为true时会通知Linux Cdp驱动切换为兼容模式（预处理阶段拷贝数据）
func Create(path string, maxDiskCount uint32, maxDiskSize uint64, switchPreStageCopyWorkMode bool) (m Metadata, err error) {
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	_m := new(metadata)
	_m.path = path

	_m.header, err = GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return nil, err
	}

	if switchPreStageCopyWorkMode {
		_m.header.LinuxWorkMode = 1
	}

	defer func() {
		if err == nil {
			ec := _m.check()
			if ec == nil {
				return
			}
			err = ec
		}
		_ = os.Remove(path)
	}()

	f, err := os.OpenFile(path, os.O_CREATE|W_DSYNC_MODE, defaultPerm)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if err = struc.Pack(f, &_m.header); err != nil {
		return nil, err
	}
	hs, err := struc.Sizeof(_m.header)
	if err != nil {
		return nil, err
	}

	padSize := DefaultFirstDiskBitmapStart - hs
	if padSize < 0 {
		return nil, errors.Errorf("invalid header size: %d", hs)
	}
	if _, err = f.Write(make([]byte, padSize)); err != nil {
		return nil, err
	}

	bytesPerBitmap := _m.header.BitsPerDiskBitmap / 8
	bytesAllBitmap := uint64(bytesPerBitmap) * uint64(_m.header.MaxDiskCount)

	chunkBuf := make([]byte, 1<<20)
	chunkCnt := bytesAllBitmap / uint64(len(chunkBuf))
	for i := uint64(0); i < chunkCnt; i++ {
		if _, err = f.Write(chunkBuf); err != nil {
			return nil, err
		}
	}

	if err = f.Sync(); err != nil {
		return nil, err
	}

	_m.extents, err = FileDiskExtents(path)
	if err != nil {
		return nil, err
	}

	if err = customFileAttr(path); err != nil {
		return nil, err
	}

	return _m, nil
}

func (m *metadata) Path() string {
	return m.path
}

func (m *metadata) Header() Header {
	return m.header
}

func (m *metadata) Extents() []FileDiskExtentSegment {
	return m.extents
}

func (m *metadata) Destroy() error {
	return os.Remove(m.path)
}

func (m *metadata) check() error {
	f, err := os.Open(m.path)
	if err != nil {
		return err
	}
	defer f.Close()

	fileMd5, err := Md5sum(f)
	if err != nil {
		return err
	}

	dmd5Hasher := md5.New()
	if _, err = CopyFileByExtents(m.path, dmd5Hasher); err != nil {
		return err
	}
	diskMd5 := hex.EncodeToString(dmd5Hasher.Sum(nil))

	if fileMd5 != diskMd5 {
		return errors.New("file md5 does not match disk md5 hash")
	}
	return nil
}

func DumpExtents(es []FileDiskExtentSegment) string {
	var buf bytes.Buffer
	t := tablewriter.NewWriter(&buf)
	t.SetHeader([]string{"Index", "Disk", "Start", "Length"})
	for i, ex := range es {
		t.Append([]string{
			strconv.Itoa(i),
			ex.Disk,
			strconv.FormatUint(uint64(ex.Start), 10),
			strconv.FormatUint(uint64(ex.Size), 10),
		})
	}
	t.Render()
	return buf.String()
}
