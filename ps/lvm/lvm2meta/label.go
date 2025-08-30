package lvm2meta

import "fmt"

func (h LabelHeader) String() string {
	return fmt.Sprintf(
		"LabelHeader[ID:%s Sector:%d CRC:%d Offset:%d Typename:%s]",
		string(h.ID[:]), h.Sector, h.CRC, h.Offset, string(h.Typename[:]),
	)
}

func (h LabelHeader) CheckCRC32(sector []byte) (uint32, bool) {
	crc32checksum := Calc(InitialCRC, sector[20:SectorSize])
	return crc32checksum, crc32checksum == h.CRC
}
