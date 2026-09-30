package info

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
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
		device := filepath.Join("/dev", name)
		if !strings.HasPrefix(name, "md") {
			continue
		}

		levelFile := filepath.Join(mdPath, name, "md", "level")
		sizeFile := filepath.Join(mdPath, name, "size")
		uuidFile := filepath.Join(mdPath, name, "md", "uuid")
		devicesPath := filepath.Join(mdPath, name, "slaves")

		level := -1
		size := uint64(0)
		var uuid string
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

		if level == -1 {
			continue
		}

		table_, err := GetDiskTable(device)
		if err != nil {
			return nil, err
		}

		if data, err := os.ReadFile(sizeFile); err == nil {
			size64, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			if err == nil {
				// size 单位是 512 字节块
				size = size64 * 512
			}
		}

		if data, err := os.ReadFile(uuidFile); err == nil {
			uuid = strings.TrimSpace(string(data))
		}
		if uuid == "" {
			// IMSM/DDF 等 external metadata 的阵列在 sysfs 中没有 uuid 文件，
			// 需回退到 mdadm --detail --export 解析 MD_UUID=。
			uuid = mdRaidUUID(device)
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
			Device: device,
			UUID:   uuid,
			Size:   int64(size),
			Table:  table_,
			Slaves: subDevices,
		})
	}

	return raids, nil
}

// mdRaidUUID 通过 mdadm --detail --export 获取阵列 UUID。
// external metadata（IMSM/DDF）阵列的 sysfs 中没有 uuid 文件，只能由此获取。
func mdRaidUUID(device string) string {
	_, out, err := command.Execute("mdadm --detail --export " + device)
	if err != nil {
		return ""
	}
	return parseMdRaidUUID(out)
}

// parseMdRaidUUID 从 mdadm --detail --export 输出中解析 MD_UUID= 的值。
func parseMdRaidUUID(out string) string {
	const prefix = "MD_UUID="
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return ""
}
