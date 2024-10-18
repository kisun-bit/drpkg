package ntfs

import (
	"fmt"
	"io"
)

// Fragment contains an absolute Offset in bytes from the start of a volume and a Length of the fragment, also in bytes.
type Fragment struct {
	Offset int64
	Length int64
}

// A fragment Reader will read data from the fragments in order. When one fragment is depleted, it will seek to the
// position of the next fragment and continue reading from there, until all fragments have been exhausted. When the last
// fragment has been exhaused, each subsequent Read() will return io.EOF.
type Reader struct {
	src       io.ReadSeeker
	fragments []Fragment
	idx       int
	remaining int64
}

// NewReader initializes a new Reader from the io.ReaderSeeker and fragments and returns a pointer to. Note that
// fragments may not be sequential in order, so the io.ReadSeeker should support seeking backwards (or rather, from the
// start).
func NewReader(src io.ReadSeeker, fragments []Fragment) *Reader {
	return &Reader{src: src, fragments: fragments, idx: -1, remaining: 0}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.idx >= len(r.fragments) {
		return 0, io.EOF
	}

	if len(p) == 0 {
		return 0, nil
	}

	if r.remaining == 0 {
		r.idx++
		if r.idx >= len(r.fragments) {
			return 0, io.EOF
		}
		next := r.fragments[r.idx]
		r.remaining = next.Length
		seeked, err := r.src.Seek(next.Offset, io.SeekStart)
		if err != nil {
			return 0, fmt.Errorf("unable to seek to next offset %d: %v", next.Offset, err)
		}
		if seeked != next.Offset {
			return 0, fmt.Errorf("wanted to seek to %d but reached %d", next.Offset, seeked)
		}
	}

	target := p
	if int64(len(p)) > r.remaining {
		target = p[:r.remaining]
	}

	n, err = io.ReadFull(r.src, target)
	r.remaining -= int64(n)
	return n, err
}
