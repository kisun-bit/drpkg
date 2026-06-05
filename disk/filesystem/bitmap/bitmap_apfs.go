package bitmap

import (
	"fmt"

	"github.com/pkg/errors"
)

type apfsBitmapParser struct {
	fr *fsRegionReader
}

func NewApfsBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &apfsBitmapParser{fr: fr}, nil
}

func (p *apfsBitmapParser) String() string {
	return fmt.Sprintf("apfsBitmapParser(%s)", p.fr)
}

func (p *apfsBitmapParser) Dump() (*FsBitmap, error) {
	return nil, errors.New("apfs bitmap parser is not implemented yet")
}
