package storage

import "github.com/kisun-bit/drpkg/disk/table"

type WindowsSharedDiskAttrs struct {
	BriefDesc        string `json:"brief"`
	DiskPath         string `json:"disk_path"`
	Boot             bool   `json:"boot"` // 是否含有bootloader.
	ReadOnly         bool   `json:"read_only"`
	Offline          bool   `json:"offline"`
	Ineffective      bool   `json:"ineffective"`
	BusType          string `json:"bus_type"`
	Dynamic          bool   `json:"dynamic"`
	Vendor           string `json:"vendor_id"`
	Product          string `json:"product_id"`
	SerialNumber     string `json:"serial_number"`
	Revision         string `json:"product_revision"`
	EffectiveForBoot bool   `json:"effective_for_boot"`
}

type WindowsSharedPartAttrs struct {
	BriefDesc            string `json:"brief"`
	IsBitlockerVolume    bool   `json:"is_bitlocker_volume"`
	DiskPath             string `json:"disk_path"`
	PartActualID         int    `json:"part_actual_id"`
	VolumePath           string `json:"volume_path"`
	VolumeFilesystem     string `json:"volume_filesystem"`
	VolumeLabel          string `json:"volume_label"`
	VolumeDriveLetter    string `json:"volume_drive_letter"`
	VolumeTotalBytes     int64  `json:"volume_total_bytes"`
	VolumeAvailableBytes int64  `json:"volume_available_bytes"`
	VolumeUsedBytes      int64  `json:"volume_used_bytes"`
	EffectiveForBoot     bool   `json:"effective_for_boot"`
}

type WindowsHardDiskMBR struct {
	table.MBRJson
	WindowsSharedDiskAttrs
	Parts []WindowsHardDiskMBRPartition `json:"parts"`
	UniqueID
}

type WindowsHardDiskMBRPartition struct {
	table.MBRPartJson
	WindowsSharedPartAttrs
	UniqueID
}

type WindowsHardDiskGPT struct {
	table.GPTJson
	WindowsSharedDiskAttrs
	Parts []WindowsHardDiskGPTPartition `json:"parts"`
	UniqueID
}

type WindowsHardDiskGPTPartition struct {
	table.GPTPartJson
	WindowsSharedPartAttrs
	UniqueID
}

type WindowsHardDiskRAW struct {
	DiskLabelType  string `json:"disk_label_type"`
	DiskIdentifier string `json:"disk_identifier"`
	Size           int64  `json:"size"`
	WindowsSharedDiskAttrs
	UniqueID
}

type LinuxSharedDiskAttrs struct {
	DiskPath     string `json:"disk_path"`
	ReadOnly     bool   `json:"read_only"`
	BusType      string `json:"bus_type"`
	Boot         bool   `json:"boot"` // 是否含有bootloader.
	Model        string `json:"model"`
	SerialNumber string `json:"serial_number"`
	Ineffective  bool   `json:"ineffective"`
	// 磁盘有可能直接被格式化, 那么他就有可能具有卷的属性
	LinuxSharedPartAttrs
}

type LinuxSharedPartAttrs struct {
	BriefDesc            string `json:"brief"`
	IsPV                 bool   `json:"is_pv"`
	IsPart               bool   `json:"is_part"`
	IsDisk               bool   `json:"is_disk"`
	DeviceID             string `json:"device_id"`
	PartUUID             string `json:"part_uuid"`
	VolumeUUID           string `json:"volume_uuid"`
	VolumePath           string `json:"volume_path"`
	VolumeFilesystem     string `json:"volume_filesystem"`
	VolumeTotalBytes     int64  `json:"volume_total_bytes"`
	VolumeAvailableBytes int64  `json:"volume_available_bytes"`
	VolumeUsedBytes      int64  `json:"volume_used_bytes"`
	VolumeMountPath      string `json:"volume_mount_path"`
	EffectiveForBoot     bool   `json:"effective_for_boot"`
}

type LinuxHardDiskMBR struct {
	table.MBRJson
	LinuxSharedDiskAttrs
	Parts []LinuxHardDiskMBRPartition `json:"parts"`
	UniqueID
}

type LinuxHardDiskMBRPartition struct {
	table.MBRPartJson
	LinuxSharedPartAttrs
	UniqueID
}

type LinuxHardDiskGPT struct {
	table.GPTJson
	LinuxSharedDiskAttrs
	Parts []LinuxHardDiskGPTPartition `json:"parts"`
	UniqueID
}

type LinuxHardDiskGPTPartition struct {
	table.GPTPartJson
	LinuxSharedPartAttrs
	UniqueID
}

type LinuxHardDiskNoPartitionTable struct {
	DiskLabelType string `json:"disk_label_type"`
	SectorSize    int    `json:"sector_size"`
	Sectors       int64  `json:"sectors"`
	Size          int64  `json:"size"`
	LinuxSharedDiskAttrs
	UniqueID
}

type LinuxLVM struct {
	SupportLVM   bool `json:"support_lvm"`
	MajorVersion int  `json:"major_version"`
	PVList       []PV `json:"pv_list"`
	VGList       []VG `json:"vg_list"`
	LVList       []LV `json:"lv_list"`
}

type PV struct {
	BriefDesc        string `json:"brief"`
	Name             string `json:"name"`
	UUID             string `json:"uuid"`
	VGName           string `json:"vg_name"`
	Size             int64  `json:"size"`
	Free             int64  `json:"free"`
	Attr             string `json:"attr"`
	EffectiveForBoot bool   `json:"effective_for_boot"`
	UniqueID
}

type VG struct {
	BriefDesc        string   `json:"brief"`
	Name             string   `json:"name"`
	UUID             string   `json:"uuid"`
	PVNames          []string `json:"pv_names"`
	LVNames          []string `json:"lv_names"`
	ExtentSize       int      `json:"extent_size"`
	Size             int64    `json:"size"`
	Free             int64    `json:"free"`
	Attr             string   `json:"attr"`
	EffectiveForBoot bool     `json:"effective_for_boot"`
	UniqueID
}

type LV struct {
	BriefDesc            string `json:"brief"`
	Name                 string `json:"name"`
	DmName               string `json:"dm_name"`
	UUID                 string `json:"uuid"`
	VGName               string `json:"vg_name"`
	Pool                 string `json:"pool"`
	Origin               string `json:"origin"`
	Size                 int64  `json:"size"`
	Attr                 string `json:"attr"`
	IsVolume             bool   `json:"is_volume"`
	PartUUID             string `json:"part_uuid"`
	VolumeUUID           string `json:"volume_uuid"`
	VolumePath           string `json:"volume_path"`
	VolumeFilesystem     string `json:"volume_filesystem"`
	VolumeTotalBytes     int64  `json:"volume_total_bytes"`
	VolumeAvailableBytes int64  `json:"volume_available_bytes"`
	VolumeUsedBytes      int64  `json:"volume_used_bytes"`
	VolumeMountPath      string `json:"volume_mount_path"`
	EffectiveForBoot     bool   `json:"effective_for_boot"`
	UniqueID
}

type Swap struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Used     int64  `json:"used"`
	Priority int    `json:"priority"`
	Brief    string `json:"brief"`
	UUID     string `json:"uuid"`
	Label    string `json:"label"`
}

type UniqueID struct {
	UniqueID    string `json:"unique_id"`
	UniqueIDMd5 string `json:"unique_id_md5"`
}
