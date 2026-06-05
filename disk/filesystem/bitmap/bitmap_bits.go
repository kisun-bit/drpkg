package bitmap

import (
	"math"

	"github.com/pkg/errors"
)

const rawSectorSize = 512

func bitmapBytes(bits int64) ([]byte, error) {
	if bits < 0 {
		return nil, errors.Errorf("invalid bitmap bit count: %d", bits)
	}
	if uint64(bits) > math.MaxInt64-7 {
		return nil, errors.Errorf("bitmap bit count overflows byte size: %d", bits)
	}
	return make([]byte, (bits+7)/8), nil
}

func fillBitmap(dst []byte, value byte) {
	for i := range dst {
		dst[i] = value
	}
}

func setBit(dst []byte, totalBits int64, nr uint64) error {
	if nr >= uint64(totalBits) {
		return errors.Errorf("set block %d out of boundary(%d)", nr, totalBits)
	}
	dst[nr/8] |= 1 << (nr & 7)
	return nil
}

func clearBit(dst []byte, totalBits int64, nr uint64) error {
	if nr >= uint64(totalBits) {
		return errors.Errorf("clear block %d out of boundary(%d)", nr, totalBits)
	}
	dst[nr/8] &^= 1 << (nr & 7)
	return nil
}

func testBit(src []byte, nr uint64) bool {
	return src[nr/8]&(1<<(nr&7)) != 0
}

func countBits(src []byte, totalBits int64) int64 {
	var count int64
	for i := int64(0); i < totalBits; i++ {
		if testBit(src, uint64(i)) {
			count++
		}
	}
	return count
}
