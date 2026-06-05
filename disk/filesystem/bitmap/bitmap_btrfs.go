package bitmap

import (
	"fmt"

	"github.com/pkg/errors"
)

type btrfsBitmapParser struct {
	fr *fsRegionReader
}

func NewBtrfsBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &btrfsBitmapParser{fr: fr}, nil
}

func (p *btrfsBitmapParser) String() string {
	return fmt.Sprintf("btrfsBitmapParser(%s)", p.fr)
}

func (p *btrfsBitmapParser) Dump() (*FsBitmap, error) {
	return nil, errors.New("btrfs bitmap parser is not implemented yet")
}
