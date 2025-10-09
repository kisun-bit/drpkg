package table

import (
	"bytes"
	"io"
	"os"

	"github.com/pkg/errors"
)

type TableType string

const (
	// TableTypeUnknown 未知分区表
	// 一般指磁盘第一个LBA中为非全0数据
	TableTypeUnknown TableType = "unknown"
	// TableTypeMBR MBR分区表
	TableTypeMBR TableType = "mbr"
	// TableTypeGPT GPT分区表
	TableTypeGPT TableType = "gpt"
	// TableTypeRaw 未初始化磁盘
	// 一般指磁盘第一个LBA中为全0数据
	TableTypeRaw TableType = "raw"
)

func GetDiskType(disk string) (TableType, error) {
	// 以MBR进行探测
	mbr_, err := NewMBR(disk, 0, false)
	if err != nil {
		// 磁盘不存在
		if os.IsNotExist(err) {
			return TableTypeUnknown, err
		}

		// 磁盘存在则继续探测磁盘头部签名数据是否是全0
		signatureBuf := make([]byte, 4<<10)
		f, e := os.Open(disk)
		if e != nil {
			return TableTypeUnknown, e
		}
		defer f.Close()
		nr, er := f.Read(signatureBuf)
		if er != nil && er != io.EOF {
			return TableTypeUnknown, er
		}
		if nr == 0 {
			return TableTypeUnknown, errors.Errorf("failed to read %s", disk)
		}
		// 头部签名数据全为0
		if bytes.Equal(signatureBuf[:nr], make([]byte, nr)) {
			return TableTypeRaw, nil
		}

		return TableTypeUnknown, err
	}

	defer mbr_.Close()

	// 存在保护性MBR
	if mbr_.ContainsProtectiveMBR() {
		return TableTypeGPT, nil
	}

	// 不存在保护性MBR
	return TableTypeMBR, nil
}

func IsDiskBootable(disk string) bool {
	t, err := GetDiskType(disk)
	if err != nil {
		return false
	}

	switch t {
	case TableTypeGPT:
		gpt, err := NewGPT(disk, 0)
		if err != nil {
			return false
		}
		defer gpt.Close()
		return gpt.ContainsBootFlag()
	case TableTypeMBR:
		mbr, err := NewMBR(disk, 0, false)
		if err != nil {
			return false
		}
		defer mbr.Close()
		return mbr.ContainsBootFlag()
	}
	return false
}
