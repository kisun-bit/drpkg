//go:build linux

package qcow2

import (
	"encoding/binary"
	"github.com/lunixbochs/struc"
	"io"
)

func readBlock(r io.Reader) (block qemuBlock, err error) {
	err = struc.UnpackWithOptions(r, &block, &struc.Options{Order: binary.LittleEndian})
	return
}

func (b *qemuBlock) Write(w io.Writer) error {
	return struc.PackWithOptions(w, b, &struc.Options{Order: binary.LittleEndian})
}

func (rb *qemuRequestBlock) Write(w io.Writer) error {
	return struc.PackWithOptions(w, rb, &struc.Options{Order: binary.LittleEndian})
}
