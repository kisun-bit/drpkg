package bitmap

import (
	"fmt"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
)

type rawBitmapParser struct {
	fr *fsRegionReader
}

func NewUnknownBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	return NewRawBitmapParser(device, fsStart, fsSize)
}

func NewRawBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &rawBitmapParser{fr: fr}, nil
}

func (p *rawBitmapParser) String() string {
	return fmt.Sprintf("rawBitmapParser(%s)", p.fr)
}

func (p *rawBitmapParser) Dump() (*FsBitmap, error) {
	bits := p.fr.Size() / rawSectorSize
	bm, err := bitmapBytes(bits)
	if err != nil {
		return nil, err
	}
	fillBitmap(bm, 0xff)
	return &FsBitmap{
		Type:       types.FsTypeUnknown,
		BitmapKind: BitmapFromFS,
		Bitmap:     bm,
		Bits:       bits,
		BlockSize:  rawSectorSize,
	}, nil
}
