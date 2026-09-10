package meta

import (
	"io"
	"os"

	"github.com/pkg/errors"
)

type volumeSegment struct {
	start int64
	size  int64
}

type FileDiskExtentSegment struct {
	Disk  string
	Start int64
	Size  int64
}

func CopyFileByExtents(file string, dst io.Writer) (int64, error) {
	es, err := FileDiskExtents(file)
	if err != nil {
		return 0, err
	}

	buf := make([]byte, 4<<10)
	size := int64(0)

	for _, de := range es {
		df, eopen := os.OpenFile(de.Disk, R_DSYNC_MODE, defaultPerm)
		if eopen != nil {
			return 0, eopen
		}

		remain := de.Size
		start := de.Start
		for {
			if remain <= 0 {
				_ = df.Close()
				break
			}
			nr, er := df.ReadAt(buf, start)
			if er != nil {
				_ = df.Close()
				return 0, errors.Wrapf(er, "failed to read extent from %s", de.Disk)
			}
			wLen := nr
			if int64(nr) > remain {
				wLen = int(remain)
			}
			nw, ew := dst.Write(buf[:wLen])
			if ew != nil {
				_ = df.Close()
				return 0, errors.Wrap(ew, "failed to write extent to writer")
			}
			size += int64(nw)
			remain -= int64(nr)
			start += int64(nr)
		}
	}

	return size, nil
}
