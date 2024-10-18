package ioctl

import (
	"encoding/binary"
	"net"
)

// ########################################## Linux平台相关 ##########################################

type NetworkCfgManager interface {
	GetIPv4BootProto() string
	GetIPv4Gateway() string
	GetIPv4DNS() []string
	RemoveAllIPv4()
	AddIP(ip *net.IPNet)
	SetGateway(ip *net.IP)
	SaveTo(destFile string) error
	SaveToSelf() error
	IfCfgPath() string
}

// ResourcesStorage 本地存储环境信息.
type ResourcesStorage struct {
	// Disks 磁盘列表.
	Disks []ResourcesStorageDisk `json:"disks"`

	// Total 设备数量(包含磁盘和分区).
	Total uint64 `json:"total"`
}

type ResourcesStorageDisk struct {
	// Name 硬盘ID.
	// 示例: nvme0n1
	Name string `json:"name"`

	Path string `json:"path"`

	// DeviceNumber number.
	// 示例: 259:0
	DeviceNumber string `json:"device"`

	// Model Disk model.
	// 示例: INTEL SSDPEKKW256G7
	Model string `json:"model,omitempty"`

	// Type 存储类型.
	// 示例: nvme
	Type string `json:"type,omitempty"`

	// ReadOnly 是否只读.
	// 示例: false
	ReadOnly bool `json:"read_only"`

	// Mounted 挂载时为true.
	// 示例: true
	Mounted bool `json:"mounted"`

	// Size 设备大小.
	// 示例: 256060514304
	Size uint64 `json:"size"`

	// Removable 是否可移除(hot-plug).
	// 示例: false
	Removable bool `json:"removable"`

	// WWN identifier
	// 示例: eui.0000000001000000e4d25cafae2e4c00
	WWN string `json:"wwn,omitempty"`

	// NUMANode 磁盘所属的 NUMA 节点.
	// 示例: 0
	NUMANode uint64 `json:"numa_node"`

	// DeviceNumber by-path identifier
	// 示例: pci-0000:05:00.0-nvme-1
	DevicePCIPath string `json:"device_path,omitempty"`

	// BlockSize 块大小(一个sector大小).
	// 示例: 512
	BlockSize uint64 `json:"block_size"`

	// FirmwareVersion 当前固件版本.
	// 示例: PSF121C
	FirmwareVersion string `json:"firmware_version,omitempty"`

	// RtationRateRPM 旋转速度.
	// 示例: 0
	RtationRateRPM uint64 `json:"rpm"`

	// Serial 序列号.
	// 示例: BTPY63440ARH256D
	Serial string `json:"serial,omitempty"`

	// DeviceNumber by-id identifier
	// 示例: nvme-eui.0000000001000000e4d25cafae2e4c00
	DeviceID string `json:"device_id"`

	// PartUUID 分区UUID.
	PartUUID string `json:"part_uuid"`

	// UUID 卷UUID.
	UUID string `json:"uuid"`

	// Filesystem 文件系统.
	Filesystem string `json:"filesystem"`

	// MountPath 挂载点.
	MountPath string `json:"mountPath"`

	// Partitions 分区集合.
	Partitions []ResourcesStorageDiskPartition `json:"partitions"`

	// PCIAddress PCI 地址信息.
	// 示例: 0000:05:00.0
	PCIAddress string `json:"pci_address,omitempty"`

	// USBAddress 地址信息.
	// 示例: 3:5
	USBAddress string `json:"usb_address,omitempty"`
}

// ResourcesStorageDiskPartition 分区.
type ResourcesStorageDiskPartition struct {
	// Name 分区设备名.
	// 示例: nvme0n1p1
	Name string `json:"name"`

	// DeviceNumber number.
	// 示例: 259:1
	DeviceNumber string `json:"device_number"`

	// DeviceNumber by-id identifier
	// 示例: nvme-eui.0000000001000000e4d25cafae2e4c00
	DeviceID string `json:"device_id"`

	// PartUUID 分区UUID.
	PartUUID string `json:"part_uuid"`

	// UUID 卷UUID.
	UUID string `json:"uuid"`

	// Filesystem 文件系统.
	Filesystem string `json:"filesystem"`

	// MountPath 挂载点.
	MountPath string `json:"mountPath"`

	// ReadOnly 分区设备是否只读.
	// 示例: false
	ReadOnly bool `json:"read_only"`

	// Size 分区大小.
	// 示例: 254933278208
	Size uint64 `json:"size"`

	// PartitionIndex number.
	// 示例: 1
	PartitionIndex uint64 `json:"partition_index"`

	// Mounted 是否挂载.
	// 示例: true
	Mounted bool `json:"mounted"`
}

type deviceMountInfo struct {
	Mount      bool
	MountPath  string
	ReadOnly   bool
	Filesystem string
}

// ########################################## Windows平台相关 ##########################################

type GET_LENGTH_INFORMATION struct {
	Length int64
}

type WIN_STORAGE_BUS_TYPE byte

type GET_DISK_ATTRIBUTES struct {
	Version    uint32
	Reserved1  uint32
	Attributes uint64
}

type PARTITION_INFORMATION_EX struct {
	PartitionStyle  uint32 `struc:"uint64,little"`
	StartingOffset  int64  `struc:"little"`
	PartitionLength int64  `struc:"little"`
	PartitionNumber uint32 `struc:"uint64,little"`
}

type VolumeDiskExtents []byte

type DiskExtent struct {
	DiskNumber     uint32
	StartingOffset uint64
	ExtentLength   uint64
}

type STORAGE_PROPERTY_QUERY_WITH_DUMMY struct {
	// PropertyId 对应winioctl.h中的STORAGE_PROPERTY_ID.
	PropertyId uint32
	// QueryType 对应winioctl.h中的STORAGE_QUERY_TYPE. 各个枚举值见 https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ne-winioctl-storage_property_id
	QueryType            uint32
	AdditionalParameters [1]byte
}

type LogicalDrive struct {
	DriveName      string
	UNCPath        string
	Label          string
	UniqueID       string
	FileSystem     string
	GUIDMountPath  string
	DriveMountPath string
	DiskNumber     int
}

type STORAGE_DEVICE_DESCRIPTOR struct {
	Version               uint32
	Size                  uint32
	DeviceType            byte
	DeviceTypeModifier    byte
	RemovableMedia        bool
	CommandQueueing       bool
	VendorIdOffset        uint32
	ProductIdOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               WIN_STORAGE_BUS_TYPE
	RawPropertiesLength   uint32
}

// Win32_NetworkAdapter WMI class
type Win32_NetworkAdapter struct {
	Name            string
	NetConnectionID string
	PhysicalAdapter bool
	Description     string
}

type StorageDeviceDescription struct {
	DeviceType         byte
	DeviceTypeModifier byte
	RemovableMedia     bool
	VendorId           string
	ProductId          string
	ProductRevision    string
	SerialNumber       string
	BusType            WIN_STORAGE_BUS_TYPE
}

type WindowsVersion struct {
	Major    int
	Minor    int
	Build    int
	Revision int
}

func (v *VolumeDiskExtents) Len() uint {
	return uint(binary.LittleEndian.Uint32(*v))
}

func (v *VolumeDiskExtents) Extent(n uint) DiskExtent {
	ba := []byte(*v)
	offset := 8 + 24*n
	return DiskExtent{
		DiskNumber:     binary.LittleEndian.Uint32(ba[offset:]),
		StartingOffset: binary.LittleEndian.Uint64(ba[offset+8:]),
		ExtentLength:   binary.LittleEndian.Uint64(ba[offset+16:]),
	}
}

type EthernetExtraInfo struct {
	Physical                                                   bool
	IPv4bootProto, IPv6bootProto                               string
	IPv4gatewayList, IPv6gatewayList, IPv4dnsList, IPv6dnsList []string
	IfCfgPath                                                  string
}
