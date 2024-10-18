package table

import "os"

type DiskType string

const (
	DTypeGPT DiskType = "GPT"
	DTypeMBR DiskType = "MBR"
	DTypeRAW DiskType = "RAW"
)

// GUIDToString 将原始GUID转换为字符串.
// 注意: byteGuid 的长度只能等于16, 否则将返回空串.
func GUIDToString(byteGuid []byte) string {
	byteToChars := func(b byte) (res []byte) {
		res = make([]byte, 0, 2)
		for i := 1; i >= 0; i-- {
			switch b >> uint(4*i) & 0x0F {
			case 0:
				res = append(res, '0')
			case 1:
				res = append(res, '1')
			case 2:
				res = append(res, '2')
			case 3:
				res = append(res, '3')
			case 4:
				res = append(res, '4')
			case 5:
				res = append(res, '5')
			case 6:
				res = append(res, '6')
			case 7:
				res = append(res, '7')
			case 8:
				res = append(res, '8')
			case 9:
				res = append(res, '9')
			case 10:
				res = append(res, 'A')
			case 11:
				res = append(res, 'B')
			case 12:
				res = append(res, 'C')
			case 13:
				res = append(res, 'D')
			case 14:
				res = append(res, 'E')
			case 15:
				res = append(res, 'F')
			}
		}
		return
	}
	if len(byteGuid) != 16 {
		return ""
	}
	s := make([]byte, 0, 36)
	byteOrder := [...]int{3, 2, 1, 0, -1, 5, 4, -1, 7, 6, -1, 8, 9, -1, 10, 11, 12, 13, 14, 15}
	for _, i := range byteOrder {
		if i == -1 {
			s = append(s, '-')
		} else {
			s = append(s, byteToChars(byteGuid[i])...)
		}
	}
	return string(s)
}

// GetDiskType 获取磁盘类型.
func GetDiskType(diskPath string) (DiskType, error) {
	// 如何判定为GPT磁盘？
	// 有保护性MBR分区, 存在GPT类型的签名
	mbr_, err := NewMBR(diskPath, 0, false)
	if err != nil && os.IsNotExist(err) {
		return DTypeRAW, err
	}
	if err != nil {
		return DTypeRAW, nil
	}
	existedProtectivePart := false
	for _, p := range mbr_.FullMainPartitionEntries {
		if p.IsProtectiveMBR() {
			existedProtectivePart = true
			break
		}
	}
	if !existedProtectivePart {
		return DTypeMBR, nil
	}
	return DTypeGPT, nil
}
