package bitmap

import (
	"fmt"
	"io"
	"os"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

type fsRegionReader struct {
	path   string
	offset int64
	size   int64

	file *os.File
	r    *io.SectionReader
}

func newFsRegionReader(path string, offset, size int64) (*fsRegionReader, error) {
	if offset < 0 || size <= 0 {
		return nil, errors.Errorf("invalid offset/size: offset=%d size=%d", offset, size)
	}

	deviceSize, err := extend.FileSize(path)
	if err != nil {
		return nil, errors.Errorf("get device size failed: %w", err)
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
		return nil, errors.Errorf("open device failed: %w", err)
	}

	r := io.NewSectionReader(f, offset, size)

	return &fsRegionReader{
		path:   path,
		offset: offset,
		size:   size,
		file:   f,
		r:      r,
	}, nil
}

func (r *fsRegionReader) String() string {
	return fmt.Sprintf("fsregionreader(dev=%s,off=%d,size=%d)", r.path, r.offset, r.size)
}

func (r *fsRegionReader) Close() error {
	return r.file.Close()
}
