package universal

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	upPrefix                = "PCI"
	upSeparator             = "\\"
	upVendorShort           = "V"
	upDeviceShort           = "D"
	upSubsystemVendorShort  = "SV"
	upSubsystemDeviceShort  = "SD"
	upBaseClassShort        = "BC"
	upSubClassShort         = "SC"
	upProgramInterfaceShort = "I"
	upRevisionShort         = "REV"
)

// UniPci 跨平台PCI硬件描述符
// 此结构，对所有平台的PCI设备的硬件号的表现形式进行了统一
type UniPci struct {
	// vendorId 厂商Id
	vendorId uint32

	// deviceId 设备Id
	deviceId uint32

	// subsystemVendorId 子系统厂商Id
	subsystemVendorId uint32

	// subsystemDeviceId 子系统设备Id
	subsystemDeviceId uint32

	// baseClass 主类别
	baseClass uint32

	// subClass 子类别
	subClass uint32

	// programInterface 编程接口
	programInterface uint32

	// revision 修订号
	revision uint32

	_upStr      string
	_upHumanStr string
}

func ListUniPci() ([]*UniPci, error) {
	return listUniPci()
}

func UniPciFromString(s string) (*UniPci, error) {
	if !strings.HasPrefix(strings.ToUpper(s), upPrefix) {
		return nil, errors.Errorf("invalid prefix")
	}

	up := &UniPci{}
	s = strings.ToUpper(strings.TrimPrefix(s, upPrefix))
	parts := strings.Split(s, upSeparator)

	set := func(prefix string, assign func(uint32)) {
		for _, p := range parts {
			if strings.HasPrefix(p, prefix) {
				valStr := p[len(prefix):]
				if v, err := uint32FromString(valStr); err == nil {
					assign(v)
				}
			}
		}
	}

	set(upVendorShort, func(v uint32) { up.vendorId = v })
	set(upDeviceShort, func(v uint32) { up.deviceId = v })
	set(upSubsystemVendorShort, func(v uint32) { up.subsystemVendorId = v })
	set(upSubsystemDeviceShort, func(v uint32) { up.subsystemDeviceId = v })
	set(upBaseClassShort, func(v uint32) { up.baseClass = v })
	set(upSubClassShort, func(v uint32) { up.subClass = v })
	set(upProgramInterfaceShort, func(v uint32) { up.programInterface = v })
	set(upRevisionShort, func(v uint32) { up.revision = v })

	return up, nil
}

// String PCI在业务层的统一表现形式
func (up *UniPci) String() string {
	if up._upStr != "" {
		return up._upStr
	}

	strGroups := make([]string, 0)
	strGroups = append(strGroups,
		upPrefix,
		fmt.Sprintf("%s%04x", upVendorShort, up.vendorId),
		fmt.Sprintf("%s%04x", upDeviceShort, up.deviceId),
		fmt.Sprintf("%s%04x", upSubsystemVendorShort, up.subsystemVendorId),
		fmt.Sprintf("%s%04x", upSubsystemDeviceShort, up.subsystemDeviceId),
		fmt.Sprintf("%s%02x", upBaseClassShort, up.baseClass),
		fmt.Sprintf("%s%02x", upSubClassShort, up.subClass),
		fmt.Sprintf("%s%02x", upProgramInterfaceShort, up.programInterface),
		fmt.Sprintf("%s%02x", upRevisionShort, up.revision),
	)

	up._upStr = strings.Join(strGroups, upSeparator)
	return up._upStr
}

// Human 获取此PCI的人类可读字符串
func (up *UniPci) Human() string {
	if up._upHumanStr != "" {
		return up._upHumanStr
	}

	baseClassStr, vendorStr, deviceStr := Lookup(
		uint16(up.baseClass),
		uint16(up.vendorId),
		uint16(up.deviceId))

	humanStr := fmt.Sprintf(`"%s", "%s", "%s"`, baseClassStr, vendorStr, deviceStr)
	up._upHumanStr = humanStr

	return up._upHumanStr
}

// VendorId 硬件厂商Id
func (up *UniPci) VendorId() uint32 {
	return up.vendorId
}

// DeviceId 硬件设备Id
func (up *UniPci) DeviceId() uint32 {
	return up.deviceId
}

// BaseClassId 硬件基础类别Id
func (up *UniPci) BaseClassId() uint32 {
	return up.baseClass
}

// SubClassId 硬件子类别Id
func (up *UniPci) SubClassId() uint32 {
	return up.subClass
}

func (up *UniPci) Equals(other *UniPci) bool {
	return up != nil && other != nil && up.String() == other.String()
}

// Modalias 获取此PCI在Linux系统中的硬件标识
func (up *UniPci) Modalias() string {
	return fmt.Sprintf("pci:v%08Xd%08Xsv%08xsd%08Xbc%02Xsc%02Xi%02X",
		up.vendorId, up.deviceId, up.subsystemVendorId, up.subsystemDeviceId, up.baseClass, up.subClass, up.programInterface)
}

// VirtioModalias 获取此PCI在virtio总线上的硬件标识
func (up *UniPci) VirtioModalias() (modAlias string, ok bool) {
	if up.vendorId != 0x1af4 {
		return "", false
	}
	return fmt.Sprintf("virtio:d%08Xv%08X", up.subsystemDeviceId, up.subsystemVendorId), true
}

// MsHardwareId 获取此PCI在Windows系统中的硬件ID集合
func (up *UniPci) MsHardwareId() []string {

	//
	// 硬件ID的枚举形式：
	// * VEN + DEV + SUBSYS + REV
	// * VEN + DEV + SUBSYS
	// * VEN + DEV + CC(6)
	// * VEN + DEV + CC(4)
	//

	return []string{
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X\\SUBSYS_%04X%04X\\REV_%02X",
			up.vendorId, up.deviceId, up.subsystemVendorId, up.subsystemDeviceId, up.revision),
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X\\SUBSYS_%04X%04X",
			up.vendorId, up.deviceId, up.subsystemVendorId, up.subsystemDeviceId),
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X\\CC_%02X%02X%02X",
			up.vendorId, up.deviceId, up.baseClass, up.subClass, up.programInterface),
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X\\CC_%02X%02X",
			up.vendorId, up.deviceId, up.baseClass, up.subClass),
	}
}

// MsCompatibleId 获取此PCI在Windows系统中的兼容ID集合
func (up *UniPci) MsCompatibleId() []string {

	//
	// 兼容ID的枚举形式：
	// * VEN + DEV + REV
	// * VEN + DEV
	// * VEN + CC(6)
	// * VEN + CC(4)
	// * VEN
	// * CC(6)
	// * CC(4)
	//

	return []string{
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X\\REV_%02X",
			up.vendorId, up.deviceId, up.revision),
		fmt.Sprintf("PCI\\VEN_%04X\\DEV_%04X",
			up.vendorId, up.deviceId),
		fmt.Sprintf("PCI\\VEN_%04X\\CC_%02X%02X%02X",
			up.vendorId, up.baseClass, up.subClass, up.programInterface),
		fmt.Sprintf("PCI\\VEN_%04X\\CC_%02X%02X",
			up.vendorId, up.baseClass, up.subClass),
		fmt.Sprintf("PCI\\VEN_%04X",
			up.vendorId),
		fmt.Sprintf("PCI\\CC_%02X%02X%02X",
			up.baseClass, up.subClass, up.programInterface),
		fmt.Sprintf("PCI\\CC_%02X%02X",
			up.baseClass, up.subClass),
	}
}
