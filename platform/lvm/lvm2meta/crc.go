package lvm2meta

import (
	"bytes"
	"encoding/binary"
	"io"
)

func Calc(init uint32, buffer []byte) uint32 {
	var word uint32
	crc := init

	reader := bytes.NewReader(buffer)
	err := binary.Read(reader, binary.LittleEndian, &word)
	for err != io.EOF {
		crc = crc ^ word
		crc = crcTable[crc&0xff] ^ crc>>8
		crc = crcTable[crc&0xff] ^ crc>>8
		crc = crcTable[crc&0xff] ^ crc>>8
		crc = crcTable[crc&0xff] ^ crc>>8
		err = binary.Read(reader, binary.LittleEndian, &word)
	}

	return crc
}
