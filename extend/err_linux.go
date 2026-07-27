package extend

import (
	"io"

	"github.com/pkg/errors"
)

func IsDataCrcError(err error) bool {
	_ = err
	return false
}

func IsEOF(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	return false
}
