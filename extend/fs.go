package extend

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
)

type FsRegionReader struct {
	path   string
	offset int64
	size   int64

	fileSize int64
	file     *os.File
	r        *io.SectionReader
}

func NewFsRegionReader(path string, offset, size int64) (*FsRegionReader, error) {
	if offset < 0 || size <= 0 {
		return nil, errors.Errorf("invalid offset/size: offset=%d size=%d", offset, size)
	}

	deviceSize, err := FileSize(path)
	if err != nil {
		return nil, errors.Errorf("get device size failed: %v", err)
	}
	if deviceSize <= 0 {
		return nil, errors.Errorf("invalid device size: %d", deviceSize)
	}
	if uint64(offset)+uint64(size) > deviceSize {
		return nil, errors.Errorf(
			"region out of range: offset=%d size=%d deviceSize=%d",
			offset, size, deviceSize,
		)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Errorf("open device failed: %v", err)
	}

	r := io.NewSectionReader(f, offset, size)

	return &FsRegionReader{
		path:     path,
		offset:   offset,
		size:     size,
		fileSize: int64(deviceSize),
		file:     f,
		r:        r,
	}, nil
}

func (r *FsRegionReader) String() string {
	return fmt.Sprintf("fsregionreader(dev=%s,off=%d,size=%d)", r.path, r.offset, r.size)
}

func (r *FsRegionReader) Close() error {
	return r.file.Close()
}

func (r *FsRegionReader) ReadAt(p []byte, off int64) (int, error) {
	return r.r.ReadAt(p, off)
}

func (r *FsRegionReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r *FsRegionReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		offset += r.offset
	}
	if whence == io.SeekEnd {
		offset += (r.fileSize - r.size)
	}
	return r.r.Seek(offset, whence)
}

func (r *FsRegionReader) Size() int64 {
	return r.size
}
