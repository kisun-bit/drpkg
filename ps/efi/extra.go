package efi

import (
	"fmt"

	"github.com/pkg/errors"
)

func ParseBootCurrent(raw []byte) (int, error) {
	raw = raw[4:]
	// 数据是一个小端序的uint16类型
	l := len(raw)
	if l != 2 {
		return -1, errors.Errorf("failed to read BootCurrent data: data length (%d) is not 2", l)
	}
	bootCurrent, _, err := GetEFIUint(raw, 16)
	return int(bootCurrent), err
}

func GetEFIUint(data []byte, bits int) (uint64, int, error) {
	if bits%8 != 0 {
		return 0, 0, errors.Errorf("failed to get EFI uint: bits (%d) are not multiple of 8", bits)
	}
	nb := bits / 8
	if nb == 0 {
		return 0, 0, nil
	} else if len(data) < nb {
		return 0, 0, errors.Errorf("failed to get EFI uint%d: insufficient data (%d bytes), expected %d", bits, len(data), nb)
	}
	var val = uint64(data[nb-1])
	for i := nb - 2; i >= 0; i-- {
		val = val*256 + uint64(data[i])
	}
	return val, nb, nil
}

func BootEntryName(bootNumber int) string {
	return fmt.Sprintf("Boot%04X", bootNumber)
}

//// UnmarshalLoadOption decodes a binary EFI_LOAD_OPTION into a LoadOption.
//func UnmarshalLoadOption(data []byte) (*LoadOption, error) {
//	if len(data) < 6 {
//		return nil, fmt.Errorf("invalid load option: minimum 6 bytes are required, got %d", len(data))
//	}
//	lenPath := binary.LittleEndian.Uint16(data[4:6])
//	// Search for UTF-16 null code
//	nullIdx := bytes.Index(data[6:], []byte{0x00, 0x00})
//	if nullIdx == -1 {
//		return nil, errors.New("no null code point marking end of Description found")
//	}
//	descriptionEnd := 6 + nullIdx + 1
//	descriptionRaw := data[6:descriptionEnd]
//	description, err := Encoding.NewDecoder().Bytes(descriptionRaw)
//	if err != nil {
//		return nil, fmt.Errorf("error decoding UTF-16 in Description: %w", err)
//	}
//	descriptionEnd += 2 // 2 null bytes terminating UTF-16 string
//	_ = description
//	if descriptionEnd+int(lenPath) > len(data) {
//		return nil, fmt.Errorf("declared length of FilePath (%d) overruns available data (%d)", lenPath, len(data)-descriptionEnd)
//	}
//	filePathData := data[descriptionEnd : descriptionEnd+int(lenPath)]
//	opt.FilePath, filePathData, err = UnmarshalDevicePath(filePathData)
//	if err != nil {
//		return nil, fmt.Errorf("failed unmarshaling FilePath: %w", err)
//	}
//	for len(filePathData) > 0 {
//		var extraPath DevicePath
//		extraPath, filePathData, err = UnmarshalDevicePath(filePathData)
//		if err != nil {
//			return nil, fmt.Errorf("failed unmarshaling ExtraPath: %w", err)
//		}
//		opt.ExtraPaths = append(opt.ExtraPaths, extraPath)
//	}
//
//	if descriptionEnd+int(lenPath) < len(data) {
//		opt.OptionalData = data[descriptionEnd+int(lenPath):]
//	}
//	return &opt, nil
//}
