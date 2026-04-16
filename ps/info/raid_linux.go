package info

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// QueryRaidDevices 获取所有 md 设备
func QueryRaidDevices() ([]RaidDevice, error) {
	mdPath := "/sys/block"
	files, err := os.ReadDir(mdPath)
	if err != nil {
		return nil, err
	}

	var raids []RaidDevice
	for _, f := range files {
		name := f.Name()
		if !strings.HasPrefix(name, "md") {
			continue
		}

		levelFile := filepath.Join(mdPath, name, "md", "level")
		sizeFile := filepath.Join(mdPath, name, "size")
		devicesPath := filepath.Join(mdPath, name, "slaves")

		level := -1
		size := uint64(0)
		var subDevices []string

		if data, err := os.ReadFile(levelFile); err == nil {
			// 读取 RAID 级别，可能是 "raid1"、"raid5" 等
			levelStr := strings.TrimSpace(string(data))
			switch levelStr {
			case "raid0":
				level = 0
			case "raid1":
				level = 1
			case "raid4":
				level = 4
			case "raid5":
				level = 5
			case "raid6":
				level = 6
			case "raid10":
				level = 10
			default:
				level = -1
			}
		}

		if data, err := os.ReadFile(sizeFile); err == nil {
			size64, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			if err == nil {
				// size 单位是 512 字节块
				size = size64 * 512
			}
		}

		// 获取子设备
		if slaves, err := os.ReadDir(devicesPath); err == nil {
			for _, s := range slaves {
				subDevices = append(subDevices, filepath.Join("/dev", s.Name()))
			}
		}

		if len(subDevices) == 0 {
			continue
		}

		raids = append(raids, RaidDevice{
			Name:   name,
			Level:  level,
			Device: filepath.Join("/dev", name),
			Size:   int64(size),
			Slaves: subDevices,
		})
	}

	return raids, nil
}
