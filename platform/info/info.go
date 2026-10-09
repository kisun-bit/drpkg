package info

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

type BootType string

const (
	BootTypeUnknown BootType = "unknown"
	BootTypeEFI     BootType = "uefi"
	BootTypeBIOS    BootType = "bios"
)

type PsInfo struct {
	// Public 主机公共信息
	Public PublicInfo `json:"public"`
	// Private 主机私有信息
	Private PrivateInfo `json:"private"`
}

type PublicInfo struct {
	Generic
	Dmi DmiInfo `json:"dmi"`

	// HardwareFingerprint 硬件指纹（详情）
	HardwareFingerprint MachineFingerprint `json:"hardwareFingerprint"`
	// IsMemoryOS 是否是内存操作系统
	IsMemoryOS bool `json:"isMemoryOS"`
	// IsVirtualHost 是否是虚拟机
	IsVirtualHost bool `json:"isVirtualHost"`
	// BootType 启动类型(bios、uefi)
	BootType BootType `json:"bootType"`
	// EFIInfo UEFI启动信息
	EFIInfo EFI `json:"efiInfo"`
	// EnableVTX 是否支持虚拟cpu
	EnableVTX bool `json:"enableVTX"`
	// IFList 网卡列表
	IFList []IF `json:"ifList"`
	// Disks 磁盘列表
	Disks []Disk `json:"disks"`
	// Volumes 卷列表
	Volumes []Volume `json:"volumes"`
}

type PrivateInfo struct {
	// Linux 类Linux系统私有信息
	Linux LinuxPrivateInfo `json:"linuxPrivateInfo"`
	// Windows Windows系统私有信息
	Windows WindowsPrivateInfo `json:"windowsPrivateInfo"`
}

type LinuxPrivateInfo struct {
	// Effective 是否有效
	Effective bool `json:"effective"`
	// Kernels 内核信息集合
	Kernels []LinuxKernel `json:"kernels"`
	// Release 版本信息
	Release LinuxRelease `json:"release"`
	// Target 目标平台信息
	Target LinuxTarget `json:"target"`
	// Swaps 交换分区信息
	Swaps []LinuxSwap `json:"swaps"`
	// LVM 逻辑卷信息
	LVM LVM `json:"lvm"`
	// Multipath 多路径设备列表
	Multipath []MultipathDevice `json:"multipath"`
	// Raid RAID设备列表
	Raid []RaidDevice `json:"raid"`
}

type WindowsPrivateInfo struct {
	// Effective 是否有效
	Effective bool `json:"effective"`
	// Release 版本信息
	Release WindowsRelease `json:"release"`
	// Updates 已更新的补丁包
	Updates []Hotfix `json:"updates"`
	// FIXME
}

// RecoveryDevice 表示待恢复的目标设备。
type RecoveryDevice struct {
	HumanName          string `json:"humanName"`          // 可读名称
	Device             string `json:"device"`             // 设备路径
	LogicalSectorSize  int    `json:"logicalSectorSize"`  // 逻辑扇区大小（单位：字节）
	PhysicalSectorSize int    `json:"physicalSectorSize"` // 物理扇区大小（单位：字节）
	Size               uint64 `json:"size"`               // 设备容量，单位：字节
	Safe               bool   `json:"safe"`               // 设备是否安全可写；仅当设备前 64KB 空间的数据全部为 0 时有效
	RaidUUID           string `json:"raidUUID"`           // RAID 阵列 UUID；仅适用于 RAID 设备
	MultipathUUID      string `json:"multipathUUID"`      // Multipath 设备 UUID；仅适用于 Multipath 设备
}

// QueryPsInfo 查询系统信息
func QueryPsInfo() (pi *PsInfo, err error) {
	pi = new(PsInfo)

	if err = pi.fillPublicInfo(); err != nil {
		return nil, err
	}
	if err = pi.fillPrivateInfo(); err != nil {
		return nil, err
	}

	return pi, nil
}

func (p *PsInfo) String() string {
	j, _ := json.Marshal(*p)
	return string(j)
}

func (p *PsInfo) Pretty() string {
	j, _ := json.MarshalIndent(*p, "", "        ")
	return string(j)
}

func (p *PsInfo) OsVersion() string {
	switch {
	case p.Private.Linux.Effective:
		return p.Private.Linux.Release.Distro
	case p.Private.Windows.Effective:
		return p.Private.Windows.Release.OsName
	default:
		return "unknown os version"
	}
}

func (p *PsInfo) KernelVersion() string {
	switch {
	case p.Private.Linux.Effective:
		for _, k := range p.Private.Linux.Kernels {
			if k.Default {
				return k.Name
			}
		}
	case p.Private.Windows.Effective:
		return p.Private.Windows.Release.Version.String()
	}
	return "unknown kernel version"
}

func (p *PsInfo) CpuModel() string {
	if len(p.Public.Cpu.Models) > 0 {
		return p.Public.Cpu.Models[0]
	}
	return "unknown cpu model"
}

func (p *PsInfo) DeviceBootable(device string) bool {
	for _, v := range p.Public.Volumes {
		if !v.IsBootable {
			continue
		}

		if v.Name == device {
			return true
		}

		for _, vs := range v.Segments {
			if vs.Device == device {
				return true
			}
		}
	}

	return false
}

func (p *PsInfo) HardwareFingerprintChanged(other *PsInfo) bool {
	if other == nil {
		return false
	}
	return p.Public.HardwareFingerprint.Equals(&other.Public.HardwareFingerprint)
}

func (p *PsInfo) RecoveryDevices() []RecoveryDevice {
	var rds []RecoveryDevice

	sectorFmt := func(lba, pba int) string {
		switch {
		case lba == 512 && pba == 512:
			return "512n"
		case lba == 512 && pba == 4096:
			return "512e"
		case lba == 4096 && pba == 4096:
			return "4Kn"
		default:
			return "Other"
		}
	}

	appendDevice := func(
		device string,
		size uint64,
		lba, pba int,
		deviceType string,
		raidUUID, multipathUUID string,
	) {
		rds = append(rds, RecoveryDevice{
			HumanName: fmt.Sprintf("%s · %s · %s · %s",
				device,
				xutil.TrimAllSpace(humanize.IBytes(size)),
				sectorFmt(lba, pba),
				deviceType,
			),
			Device:             device,
			Size:               size,
			LogicalSectorSize:  lba,
			PhysicalSectorSize: pba,
			Safe:               isRecoveryDeviceSafe(device),
			RaidUUID:           raidUUID,
			MultipathUUID:      multipathUUID,
		})
	}

	switch runtime.GOOS {
	case "linux":
		slaves := make(map[string]struct{})

		for _, md := range p.Private.Linux.Raid {
			appendDevice(
				md.Device, uint64(md.Size),
				md.LogicalSectorSize, md.PhysicalSectorSize,
				fmt.Sprintf("RAID %d", md.Level), md.UUID, "",
			)
			for _, slave := range md.Slaves {
				slaves[slave] = struct{}{}
			}
		}

		for _, mp := range p.Private.Linux.Multipath {
			appendDevice(
				mp.Device, uint64(mp.Size),
				mp.LogicalSectorSize, mp.PhysicalSectorSize,
				"MULTIPATH", "", mp.UUID,
			)
			for _, slave := range mp.Slaves {
				slaves[slave] = struct{}{}
			}
		}

		for _, d := range p.Public.Disks {
			if _, ok := slaves[d.Device]; ok {
				continue
			}
			appendDevice(
				d.Device, uint64(d.Size),
				d.LogicalSectorSize, d.PhysicalSectorSize,
				"HARDDISK", "", "",
			)
		}

	case "windows":
		for _, d := range p.Public.Disks {
			appendDevice(
				d.Device, uint64(d.Size),
				d.LogicalSectorSize, d.PhysicalSectorSize,
				"HARDDISK", "", "",
			)
		}
	}

	return rds
}

func (p *PsInfo) fillPublicInfo() (err error) {
	if p.Public.Generic, err = QueryGeneric(); err != nil {
		return errors.Wrap(err, "query generic info")
	}
	if p.Public.Dmi, err = QueryDmi(); err != nil {
		return errors.Wrap(err, "query dmi")
	}
	p.Public.IsMemoryOS = IsMemoryOS()
	p.Public.IsVirtualHost = IsVirtualHost(p.Public.Dmi.SystemName)
	p.Public.BootType = QueryBootType()
	if p.Public.BootType == "uefi" {
		if p.Public.EFIInfo, err = QueryEFIInfo(); err != nil {
			return errors.Wrap(err, "query efi")
		}
	}
	p.Public.EnableVTX = SupportCPUVirtual()
	if p.Public.IFList, err = QueryIFList(); err != nil {
		return errors.Wrap(err, "query eth")
	}
	if p.Public.Volumes, err = QueryVolumes(); err != nil {
		return errors.Wrap(err, "query volumes")
	}
	if p.Public.Disks, err = QueryDisks(); err != nil {
		return errors.Wrap(err, "query disks")
	}

	mf, err := MachineFingerprintFromPsInfo(p)
	if err != nil {
		return errors.Wrap(err, "query machine fingerprint")
	}
	p.Public.HardwareFingerprint = *mf
	return nil
}

func (p *PsInfo) fillPrivateInfo() (err error) {
	switch runtime.GOOS {
	case "linux":
		p.Private.Linux.Effective = true
		if p.Private.Linux.Kernels, err = QueryLinuxKernels("/"); err != nil {
			return err
		}
		p.Private.Linux.Release = QueryLinuxRelease("/")
		if p.Private.Linux.Swaps, err = QuerySwapInfo(); err != nil {
			return err
		}
		p.Private.Linux.Target = QueryLinuxTarget()
		if p.Private.Linux.LVM, err = QueryLVMInfo(); err != nil {
			return err
		}
		if p.Private.Linux.Multipath, err = QueryMultipath(); err != nil {
			return err
		}
		if p.Private.Linux.Raid, err = QueryRaidDevices(); err != nil {
			return err
		}
	case "windows":
		p.Private.Windows.Effective = true
		if p.Private.Windows.Release, err = QueryWindowsRelease(""); err != nil {
			return err
		}
		if p.Private.Windows.Updates, err = QueryHotfixList(); err != nil {
			return err
		}
	}
	return nil
}
