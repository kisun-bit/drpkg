package info

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// MachineFingerprint 机器指纹。
//
// 设计目标：
//   - 同一台机器（含虚拟机）重启后指纹保持不变（稳定性）；
//   - 虚拟机克隆、系统异机恢复后，目标机器的指纹与源机器不同（可区分性）；
//   - 不绑定 MAC 地址，避免网卡替换/克隆导致的误判。
//
// 指纹只取“硬件身份”信号：SMBIOS 系统 UUID、主板/整机序列号、CPU 型号，
// 以及启动磁盘的硬件身份标识（物理序列号或稳定路径名）。
// 刻意排除 GPT/MBR 分区标识等“内容”信号——它们会随磁盘镜像一起被克隆/恢复，
// 属于同一个镜像，无法区分源机与目标机。
type MachineFingerprint struct {
	ProductUUID        string   `json:"product_uuid"`
	BoardSerial        string   `json:"board_serial"`
	CpuID              string   `json:"cpu_id"`
	BootableDiskIDList []string `json:"boot_disk_id_list"` // 启动磁盘的硬件身份标识
}

// RawString 返回指纹的规范化原始串。
//
// 固定字段顺序、键值之间以 & 分隔；即使值为空也保留键名，保证每种硬件身份
// 组合对应唯一的串。启动磁盘按字典序输出并编号，与入参顺序无关。
// 形如：productuuid_%s&boardserial_%s&cpuid_%s&bootdisk001_%s&bootdisk002_%s
func (fp *MachineFingerprint) RawString() string {
	if fp == nil {
		return ""
	}

	disks := append([]string(nil), fp.BootableDiskIDList...)
	sort.Strings(disks)

	parts := make([]string, 0, 3+len(disks))
	parts = append(parts,
		"productuuid_"+fp.ProductUUID,
		"boardserial_"+fp.BoardSerial,
		"cpuid_"+fp.CpuID,
	)

	for i, disk := range disks {
		parts = append(parts, fmt.Sprintf("bootdisk%03d_%s", i+1, disk))
	}

	return strings.Join(parts, "&")
}

// String 返回指纹的 sha256 摘要（十六进制），即机器的稳定 ID。
func (fp *MachineFingerprint) String() string {
	if fp == nil {
		return ""
	}

	sum := sha256.Sum256([]byte(fp.RawString()))
	return hex.EncodeToString(sum[:])
}

// Equals 判断两份指纹是否属于同一台机器。
func (fp *MachineFingerprint) Equals(other *MachineFingerprint) bool {
	if fp == nil || other == nil {
		return fp == other
	}

	return fp.String() == other.String()
}

// QueryMachineFingerprint 查询本机信息并计算机器指纹。
func QueryMachineFingerprint() (*MachineFingerprint, error) {
	psInfo, err := QueryPsInfo()
	if err != nil {
		return nil, err
	}

	return MachineFingerprintFromPsInfo(psInfo)
}

// MachineFingerprintFromPsInfo 根据已采集的系统信息计算机器指纹。
// psInfo 为 nil 时自动查询本机信息。
func MachineFingerprintFromPsInfo(psInfo *PsInfo) (*MachineFingerprint, error) {
	if psInfo == nil {
		var err error
		psInfo, err = QueryPsInfo()
		if err != nil {
			return nil, err
		}
	}

	return &MachineFingerprint{
		ProductUUID:        normalizeFingerprintValue(psInfo.Public.Dmi.SystemUUID),
		BoardSerial:        fingerprintBoardSerial(psInfo),
		CpuID:              fingerprintCpuID(psInfo),
		BootableDiskIDList: fingerprintBootDiskIDs(psInfo),
	}, nil
}

// fingerprintBoardSerial 组装可唯一标识硬件的 DMI 序列号字段。
func fingerprintBoardSerial(psInfo *PsInfo) string {
	dmi := psInfo.Public.Dmi

	parts := make([]string, 0, 2)
	if s := normalizeFingerprintValue(dmi.BaseBoardSerialNumber); s != "" {
		parts = append(parts, s)
	}
	if s := normalizeFingerprintValue(dmi.SystemSerial); s != "" {
		parts = append(parts, s)
	}

	return strings.Join(parts, "|")
}

// fingerprintCpuID 取 CPU 型号作为弱标识。同型号 CPU 在多台机器上的值相同，
// 仅作为辅助信号，不承担区分责任。
func fingerprintCpuID(psInfo *PsInfo) string {
	if models := psInfo.Public.Cpu.Models; len(models) > 0 {
		return models[0]
	}

	if vendors := psInfo.Public.Cpu.Vendors; len(vendors) > 0 {
		return vendors[0]
	}

	return ""
}

// fingerprintBootDiskIDs 返回启动磁盘的硬件身份标识，按字典序排序。
func fingerprintBootDiskIDs(psInfo *PsInfo) []string {
	ids := make([]string, 0)
	seen := make(map[string]struct{})

	for _, d := range psInfo.Public.Disks {
		if !psInfo.DeviceBootable(d.Device) {
			continue
		}

		id := fingerprintDiskHardwareID(d)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	sort.Strings(ids)
	return ids
}

// fingerprintDiskHardwareID 返回磁盘的硬件身份标识。
// 优先使用磁盘物理序列号，其次使用稳定路径名（Windows 为 PNPDeviceID，Linux 为 /dev/disk/by-path）。
// 当路径名退化为裸设备名（如 "sda"）时不再具备区分度，予以忽略。
func fingerprintDiskHardwareID(d Disk) string {
	if s := normalizeFingerprintValue(d.SerialNumber); s != "" {
		return s
	}

	if p := normalizeFingerprintValue(d.PathId); p != "" && p != normalizeFingerprintValue(filepath.Base(d.Device)) {
		return p
	}

	return ""
}

// normalizeFingerprintValue 清洗硬件标识：去空白、淘汰无意义的占位值、统一为大写。
func normalizeFingerprintValue(v string) string {
	v = strings.TrimSpace(v)

	if v == "" {
		return ""
	}

	l := strings.ToLower(v)

	switch l {
	case
		"none",
		"unknown",
		"n/a",
		"not available",
		"not applicable",
		"no asset tag",
		"oem",
		"default",
		"default string",
		"to be filled by o.e.m.",
		"to be filled by oem",
		"system serial number",
		"base board serial number",
		"chassis serial number",
		"not specified":
		return ""
	}

	if l == "00000000-0000-0000-0000-000000000000" {
		return ""
	}

	if l == "ffffffff-ffff-ffff-ffff-ffffffffffff" {
		return ""
	}

	return strings.ToUpper(v)
}
