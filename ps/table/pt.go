package table

import (
	"os"

	"github.com/pkg/errors"
)

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

func DetectBootType(diskPath string) (string, error) {
	dt, err := GetDiskType(diskPath)
	if err != nil {
		return "", err
	}
	// FIXME 未判定是否存在启动分区.
	if dt == DTypeMBR {
		return "bios", nil
	}
	gpt, err := NewGPT(diskPath, 0)
	if err != nil {
		return "", err
	}
	for i := 0; i < 128; i++ {
		if gpt.PartitionEntries[i].PartTypeGUIDInMixedEndian() == BIOSBootPartition {
			return "bios", nil
		}
	}
	return "uefi", nil
}

func DetectOSType(diskPath string) (string, error) {
	dt, err := GetDiskType(diskPath)
	if err != nil {
		return "", err
	}
	if dt == DTypeMBR {
		mbr, e := NewMBR(diskPath, 0, false)
		if e != nil {
			return "", e
		}
		for i := 0; i < 4; i++ {
			switch mbr.FullMainPartitionEntries[i].PartitionType {
			case NTFS, WindowsRecoveryEnv:
				return "windows", nil
			case LinuxSwap, Linux, LinuxExtend, LinuxPlaintext, LinuxLVM, HiddenLinux, LinuxRAID, LinuxUnifiedKeySetup:
				return "linux", nil
				// FIXME 其他系统分区标识.
			}
		}
	} else {
		gpt, e := NewGPT(diskPath, 0)
		if e != nil {
			return "", e
		}
		for i := 0; i < 128; i++ {
			switch gpt.PartitionEntries[i].PartTypeGUIDInMixedEndian() {
			case MicroMRE, BasicDataPartition, LDMMetaDataPartition, LDMDataPartition, MicroMSR, IBMGPFSPartition:
				return "windows", nil
			case LinuxFSData, RAIDPartition, SwapPartition, LVMPartition, HomePartition, SrvPartition, PlainDmCryptPartition, LUKSPartition, Reserved:
				return "linux", nil
				// FIXME 其他系统分区标识.
			}
		}
	}
	return "", errors.New("failed to detect os type")
}

func IsDiskBootable(diskPath string) bool {
	t, _ := GetDiskType(diskPath)
	switch t {
	case DTypeGPT:
		gpt, e := NewGPT(diskPath, 0)
		if e != nil {
			return false
		}
		defer gpt.Close()
		for _, p := range gpt.PartitionEntries {
			if p.IsBootable() {
				return true
			}
		}
	case DTypeMBR:
		mbr, e := NewMBR(diskPath, 0, false)
		if e != nil {
			return false
		}
		defer mbr.Close()
		for _, p := range mbr.FullMainPartitionEntries {
			if p.IsBootable() {
				return true
			}
		}
	}
	return false
}
