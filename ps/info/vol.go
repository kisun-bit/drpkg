package info

import (
	"github.com/kisun-bit/drpkg/extend"
)

type Volume struct {
	// Name 卷的显示名称
	// Windows: "数据卷 (D:)"
	// Linux: "/dev/sda1" 或 "/dev/mapper/rl-root"
	Name string `json:"name"`

	// Layout 布局类型，
	// 可取：简单、带区（Windows特有）、RAID-5（Windows特有）、镜像（Windows特有）、跨区（Windows特有）等
	Layout extend.VolumeType `json:"layout"`

	// SegmentLayoutType 卷的数据分布类型，可取：line（默认）、mirror、unknown
	SegmentLayoutType extend.SegmentLayoutType `json:"segmentLayoutType"`

	// Segments 卷的数据分布在哪些磁盘区间
	Segments []extend.Segment `json:"segments"`

	// MountPoint 卷的挂载点
	// Windows: 使用卷的装入点，如："C:"
	// Linux: 使用设备的挂载点"/home"
	MountPoint string `json:"mountpoint"`

	// GUID 设备唯一标识符
	// Windows: 取自卷名（如：\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\）中的GUID
	// Linux：取自 blkid 输出的UUID
	GUID string `json:"guid"`

	// Filesystem 卷的文件系统类型
	Filesystem string `json:"filesystem"`

	// Usage 卷使用情况
	Usage UsageInfo `json:"usage"`

	// Size 大小
	Size uint64 `json:"size"`

	// IsBootable 是否为启动卷
	// true 代表此卷与系统启动相关
	IsBootable bool `json:"isBootable"`
	// EnabledBitlocker 卷是否启用了BitLocker加密
	EnabledBitlocker bool `json:"isBitlocker"`

	// TODO 更多字段
}

// UsageInfo 存储空间使用信息
type UsageInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	AvailBytes uint64 `json:"availBytes"`
}
