package info

import (
	"encoding/hex"
	"fmt"
	"runtime"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

// Disk 磁盘信息
type Disk struct {
	// Name 磁盘名
	Name string `json:"name"`
	// Device 设备路径
	Device string `json:"device"`
	// GUID 全局唯一ID
	GUID string `json:"guid"`
	// PathId 路径唯一Id
	// Windows: 取磁盘Id，如0、1、2
	// Linux：  取/dev/disk/by-path/xxxxx，如：pci-0000:03:00.0-scsi-0:0:0:0
	PathId string `json:"pathId"`
	// Sectors 逻辑扇区个数（单位：扇区）
	Sectors int64 `json:"sectors"`
	// LogicalSectorSize 逻辑扇区大小（单位：字节）
	LogicalSectorSize int `json:"logicalSectorSize"`
	// PhysicalSectorSize 物理扇区大小（单位：字节）
	PhysicalSectorSize int `json:"physicalSectorSize"`
	// Size 磁盘大小（单位：字节）
	Size int64 `json:"size"`
	// Vendor 制造商
	Vendor string `json:"vendor"`
	// Model 产品型号
	Model string `json:"model"`
	// SerialNumber 硬件序列号（注意：可能为空）
	SerialNumber string `json:"serialNumber"`
	// Bus 总线类型
	// 注意Windows请参考：WIN_STORAGE_BUS_TYPE
	Bus string `json:"bus"`
	// Table 分区表信息
	Table DiskTable `json:"table"`
	// IsOnline 是否已联机
	IsOnline bool `json:"isOnline"`
	// IsMsDynamic 是否为Windows动态磁盘
	IsMsDynamic bool `json:"isMsDynamic"`
	// IsReadOnly 是否只读
	IsReadOnly bool `json:"isReadOnly"`
}

// DiskTable 磁盘分区表信息
type DiskTable struct {
	// Device 设备路径
	Device string `json:"device"`
	// Type 分区表类型
	Type table.TableType `json:"type"`
	// Damaged 是否损坏，仅在Type等于GPT或MBR时才有意义
	Damaged bool `json:"damaged"`
	// Identifier 分区表唯一ID
	Identifier string `json:"identifier"`
	// Partitions 分区表项集合
	Partitions []DiskPartitionTable `json:"partitions"`
}

// DiskPartitionTable 分区表项信息
type DiskPartitionTable struct {
	// Device 设备路径
	Device string `json:"device"`
	// Type 分区表项的类型
	Type string `json:"type"`
	// TypeBrief 分区表项的类型描述
	TypeBrief string `json:"typeBrief"`
	// Start 起始字节
	Start int64 `json:"start"`
	// Size 总大小
	Size int64 `json:"size"`
}

func GetDiskTable(disk string) (dt DiskTable, err error) {
	t, err := table.GetDiskType(disk)
	if err != nil {
		return dt, err
	}

	dt.Device = disk
	dt.Type = t

	defer func() {
		if err != nil {
			dt.Damaged = true
			err = nil
		}
	}()

	switch t {
	case table.TableTypeGPT:
		return readGPTTable(disk, dt)
	case table.TableTypeMBR:
		return readMBRTable(disk, dt)
	default:
		// 未识别的分区类型
		return dt, nil
	}
}

func readGPTTable(disk string, dt DiskTable) (DiskTable, error) {
	gpt, err := table.NewGPT(disk, 0)
	if err != nil {
		return dt, err
	}
	defer gpt.Close()

	dt.Identifier = gpt.Identifier()
	for i, gp := range gpt.PartitionEntries {
		if gp.Type() == table.GPT_UNUSED_ENTRY {
			continue
		}
		dt.Partitions = append(dt.Partitions, DiskPartitionTable{
			Device:    getPartitionDevice(disk, i+1),
			Type:      gp.Type(),
			TypeBrief: gp.Description(),
			Start:     gp.FirstLBAIndex * int64(gpt.SectorSize),
			Size:      (gp.LastLBAIndex - gp.FirstLBAIndex + 1) * int64(gpt.SectorSize),
		})
	}
	return dt, nil
}

func readMBRTable(disk string, dt DiskTable) (DiskTable, error) {
	mbr, err := table.NewMBR(disk, 0, false)
	if err != nil {
		return dt, err
	}
	defer mbr.Close()

	dt.Identifier = hex.EncodeToString(mbr.Signature)

	appendMBRPartitions := func(entries []table.MBRPartition, startIndex int) {
		for i, mp := range entries {
			if mp.Type() == table.MBR_EMPTY_PARTITION {
				continue
			}
			dt.Partitions = append(dt.Partitions, DiskPartitionTable{
				Device:    getPartitionDevice(disk, startIndex+i),
				Type:      mp.Type(),
				TypeBrief: mp.Description(),
				Start:     mp.StartingLBA * int64(mbr.SectorSize),
				Size:      mp.TotalSectors * int64(mbr.SectorSize),
			})
		}
	}

	// 主分区
	appendMBRPartitions(mbr.MainPartitionEntries[:], 1)

	// 逻辑分区
	lps, err := mbr.LogicalPartitionEntries()
	if err != nil {
		return dt, err
	}
	appendMBRPartitions(lps, 5)

	return dt, nil
}

func getPartitionDevice(disk string, partEntryIndex int) string {
	switch runtime.GOOS {
	case "windows":
		// FIXME 作者没有搞清楚Windows平台下Partition对象的生成方式，貌似并不是以分区表项的索引去生成的Partition对象，
		//  暂时屏蔽获取此类对象路径
		return ""
	default:
		if extend.StringEndWithDigit(disk) {
			return fmt.Sprintf("%sp%d", disk, partEntryIndex)
		}
		return fmt.Sprintf("%s%d", disk, partEntryIndex)
	}
}

func extendDiskGUID(d *Disk) error {
	if d == nil {
		return errors.New("nil Device")
	}

	switch runtime.GOOS {
	case "windows":
		d.GUID = d.Table.Identifier
	case "linux":
		d.GUID = d.Table.Identifier
		if d.GUID == "" {
			// 尝试以PV UUID作为磁盘ID
			pl, existed, err := extend.ScanPVLabelFromDisk(d.Device)
			if err != nil {
				return errors.Wrapf(err, "ScanPVLabelFromDisk for %s", d.Device)
			}
			if existed {
				d.GUID = string(pl.UUID[:])
			}
		}
	}

	return nil
}
