package extend

import (
	"io"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func IsDataCrcError(err error) bool {
	return errors.Is(err, windows.ERROR_CRC)
}

func IsEOF(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, windows.ERROR_SECTOR_NOT_FOUND) {
		return true
	}
	return false
}
