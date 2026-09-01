package x2xcore

import (
	"bufio"
	"os"
	"strings"

	"github.com/thoas/go-funk"
)

type INF struct {
	Sections map[string][]string

	// strings 是 [Strings] 段解析出的字符串替换表
	// （键已统一小写）
	strings map[string]string

	// manufacturerSections 是 [Manufacturer] 段引用的设备段名集合
	// （真实硬件 ID 仅存在于这些段中，[Strings] 等段中的
	// "PCI\VEN_xxxx.DeviceDesc" 键名不是设备条目）
	manufacturerSections map[string]bool
}

func ParseINF(path string) (*INF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	inf := &INF{
		Sections:             make(map[string][]string),
		strings:              make(map[string]string),
		manufacturerSections: make(map[string]bool),
	}

	var sec string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// 去掉行尾注释
		if i := strings.Index(line, ";"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}

		if sec != "" {
			inf.Sections[sec] = append(inf.Sections[sec], line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	inf.parseStrings()
	inf.parseManufacturerSections()

	return inf, nil
}

// parseStrings 解析 [Strings] 段，建立 %字符串% 替换表。
// 设备段中的设备描述与制造商名通常以 %xxx% 形式引用，
// 匹配硬件 ID 时需要先展开这些引用。
func (inf *INF) parseStrings() {
	for _, line := range inf.Sections["strings"] {
		k, v, ok := splitKeyValue(line)
		if !ok {
			continue
		}

		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		inf.strings[k] = v
	}
}

// parseManufacturerSections 解析 [Manufacturer] 段，收集其引用的
// 设备段名。只有这些段中的设备条目才代表真实的硬件支持；
// [Strings] 段中的 "PCI\VEN_xxxx&DEV_xxxx.DeviceDesc = ..." 仅是
// 设备描述文本。
func (inf *INF) parseManufacturerSections() {
	for _, line := range inf.Sections["manufacturer"] {
		_, v, ok := splitKeyValue(line)
		if !ok {
			continue
		}

		fields := splitComma(v)
		for _, f := range fields {
			name := strings.ToLower(strings.TrimSpace(f))
			if name != "" {
				inf.manufacturerSections[name] = true
			}
		}
	}
}

// expandStrings 展开文本中的 %name% 字符串引用。未定义的引用保持原样。
func (inf *INF) expandStrings(s string) string {
	i := 0
	for i < len(s) {
		if s[i] != '%' {
			i++
			continue
		}

		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}

		name := strings.ToLower(s[i+1 : i+1+j])
		if v, ok := inf.strings[name]; ok {
			s = s[:i] + v + s[i+2+j:]
			i += len(v)
		} else {
			i += j + 2
		}
	}

	return s
}

// ServiceNames 返回 INF 声明的内核驱动服务名列表。
//
// 仅统计 [xxx.Services] 段中 AddService 指令的关联服务，
// 且该服务必须有对应的 ServiceInstall 段（含 ServiceBinary 声明），
// 以排除 "AddService = , ..."（null service）等非驱动条目。
func (inf *INF) ServiceNames() []string {
	var svcs []string

	for sec, lines := range inf.Sections {
		if !strings.HasSuffix(sec, ".services") {
			continue
		}

		for _, line := range lines {
			k, v, ok := splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "AddService") {
				continue
			}

			fields := splitComma(v)
			if len(fields) < 3 {
				// AddService = <服务名> 后至少要有标志位与
				// ServiceInstall 段引用，否则不是驱动服务
				// （如 null service install）
				continue
			}

			svc := strings.Trim(strings.TrimSpace(fields[0]), `"`)
			installSec := strings.ToLower(
				strings.Trim(strings.TrimSpace(fields[2]), `"`))

			if svc == "" || installSec == "" {
				continue
			}

			// 确认 ServiceInstall 段存在且有 ServiceBinary
			hasBinary := false
			for _, l := range inf.Sections[installSec] {
				if k2, _, ok2 := splitKeyValue(l); ok2 &&
					strings.EqualFold(k2, "ServiceBinary") {
					hasBinary = true
					break
				}
			}
			if !hasBinary {
				continue
			}

			svcs = append(svcs, svc)
		}
	}

	return funk.UniqString(svcs)
}

// infNonDeviceSections 是不包含设备硬件 ID 条目的 INF 段名黑名单
// （段名在解析时已统一小写），解析硬件 ID 时跳过这些段。
var infNonDeviceSections = map[string]bool{
	"version":         true,
	"manufacturer":    true,
	"destinationdirs": true,
	"layoutfiles":     true,
	"strings":         true,
	"defaultinstall":  true,
	"classinstall":    true,
	"classinstall32":  true,
	"controlflags":    true,
}

// isInfNonDeviceSection 判断 INF 段是否属于非设备段
// （含带架构后缀的变体，如 SourceDisksNames.NTamd64）。
func isInfNonDeviceSection(sec string) bool {
	if infNonDeviceSections[sec] {
		return true
	}

	for _, prefix := range []string{"sourcedisks", "classinstall"} {
		if strings.HasPrefix(sec, prefix) {
			return true
		}
	}

	return false
}

// HardwareIds 提取 INF 设备段中声明的所有硬件/兼容 ID
// （形如 PCI\VEN_xxxx&DEV_xxxx、PCI\CC_xxxx、*PNPxxxx 等条目），
// 并做小写归一化。
//
// 设备条目格式：<设备描述> = <安装段>, <硬件ID>[, <硬件ID>...]
//
// 只扫描 [Manufacturer] 段引用的设备段，避免把 [Strings] 段中的
// "PCI\VEN_xxxx.DeviceDesc" 描述键误认为硬件 ID。
func (inf *INF) HardwareIds() []string {
	var ids []string

	for sec, lines := range inf.Sections {
		if isInfNonDeviceSection(sec) {
			continue
		}

		// 仅扫描设备段（[Manufacturer] 引用的段）。
		// 未解析出设备段信息的 INF（无 [Manufacturer] 段，如部分
		// oem*.inf）退化为按行特征扫描。
		if len(inf.manufacturerSections) > 0 && !inf.manufacturerSections[sec] {
			continue
		}

		for _, line := range lines {
			lower := strings.ToLower(inf.expandStrings(line))

			if !strings.Contains(lower, `\`) || !strings.Contains(lower, "=") {
				continue
			}

			fields := splitComma(lower)

			// 设备行按逗号切分后：
			//   fields[0] = "<设备描述> = <安装段名>"
			//   fields[1:] = 硬件 ID / 兼容 ID
			for _, f := range fields[1:] {
				f = strings.TrimSpace(f)
				f = strings.Trim(f, `"`)

				// 硬件 ID 不含空格，且必须带枚举器前缀
				// （pci\、root\、acpi\、isapnp\ 等）
				if f == "" || strings.Contains(f, " ") ||
					!strings.Contains(f, `\`) {
					continue
				}

				// 排除明显不是设备 ID 的条目（安装段名等）
				if strings.HasPrefix(f, "@") {
					continue
				}

				ids = append(ids, f)
			}
		}
	}

	return funk.UniqString(ids)
}

// Class 返回 INF [Version] 段中声明的设备类名（如 SCSIAdapter、Net）。
func (inf *INF) Class() string {
	for _, line := range inf.Sections["version"] {
		k, v, ok := splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "Class") {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"`)
	}

	return ""
}

// ClassGUID 返回 INF [Version] 段中声明的类 GUID。
//
// 取值顺序：
//  1. 直接声明的 ClassGUID；
//  2. ClassInstall/ClassInstall32 引用的 AddReg 段中
//     "HKR,,,," 条目声明的类名，再映射为已知类 GUID；
//  3. Class 类名映射为已知类 GUID。
func (inf *INF) ClassGUID() string {
	for _, line := range inf.Sections["version"] {
		k, v, ok := splitKeyValue(line)
		if !ok {
			continue
		}

		if strings.EqualFold(k, "ClassGUID") {
			if g := strings.Trim(strings.TrimSpace(v), `"`); g != "" {
				return g
			}
		}
	}

	// ClassInstall/ClassInstall32：ClassInstall32.ntx86 等变体一并处理
	for sec := range inf.Sections {
		if !strings.HasPrefix(sec, "classinstall") {
			continue
		}

		for _, line := range inf.Sections[sec] {
			k, v, ok := splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "AddReg") {
				continue
			}

			for _, addRegSec := range splitComma(v) {
				addRegSec = strings.ToLower(
					strings.Trim(strings.TrimSpace(addRegSec), `"`))

				for _, l := range inf.Sections[addRegSec] {
					// 条目格式：HKR,,,,%SystemClassName%
					// 第 4 个逗号后为类显示名（%字符串% 引用）
					if className := classDisplayName(l); className != "" {
						if g := classGuidByName(inf.expandStrings(className)); g != "" {
							return g
						}
					}
				}
			}
		}
	}

	return classGuidByName(inf.Class())
}

// classDisplayName 解析 AddReg 段中 "HKR,,,,<名称>" 形式的类显示名条目。
// 条目字段不足 5 个（HKR、子键、值名、值类型、值）时返回空。
func classDisplayName(line string) string {
	fields := splitComma(line)
	if len(fields) < 5 {
		return ""
	}

	if !strings.EqualFold(strings.TrimSpace(fields[0]), "HKR") {
		return ""
	}

	// 子键与值名必须为空（写入默认值）
	if strings.TrimSpace(fields[1]) != "" || strings.TrimSpace(fields[2]) != "" {
		return ""
	}

	return strings.Trim(strings.TrimSpace(fields[4]), `"`)
}

// classGuidByName 将设备类名映射为类 GUID（大小写不敏感）。
// 覆盖异构恢复引导相关及常见的设备类。
func classGuidByName(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "scsiadapter":
		return classGuidSCSIAdapter
	case "net":
		return classGuidNet
	case "hdc":
		return "{4D36E96A-E325-11CE-BFC1-08002BE10318}"
	case "diskdrive":
		return "{4D36E967-E325-11CE-BFC1-08002BE10318}"
	case "cdrom":
		return "{4D36E965-E325-11CE-BFC1-08002BE10318}"
	case "system":
		return "{4D36E97D-E325-11CE-BFC1-08002BE10318}"
	case "display":
		return "{4D36E968-E325-11CE-BFC1-08002BE10318}"
	case "mouse":
		return "{4D36E96F-E325-11CE-BFC1-08002BE10318}"
	case "keyboard":
		return "{4D36E96B-E325-11CE-BFC1-08002BE10318}"
	case "usb":
		return "{36FC9E60-C465-11CF-8056-444553540000}"
	case "usbclass":
		return "{36FC9E60-C465-11CF-8056-444553540000}"
	case "hidclass":
		return "{745A17A0-74D3-11D0-B6FE-00A0C90F57DA}"
	case "media":
		return "{4D36E96C-E325-11CE-BFC1-08002BE10318}"
	case "multifunction":
		return "{4D36E971-E325-11CE-BFC1-08002BE10318}"
	case "securitydevices":
		return "{D94EE5D8-C189-4A08-964A-2B4BC9E61C82}"
	default:
		return ""
	}
}

// SysFiles 返回 INF 声明部署的 .sys 驱动文件名列表，来源包括：
//  1. CopyFiles 指令引用的文件段中的 .sys 条目（含 @file.sys 简写）；
//  2. ServiceBinary 声明（形如 %12%\xxx.sys）中的文件名。
func (inf *INF) SysFiles() []string {
	var files []string

	// 1) CopyFiles 指令：收集其引用的段名
	var copySections []string

	for sec, lines := range inf.Sections {
		if isInfNonDeviceSection(sec) {
			continue
		}

		for _, line := range lines {
			k, v, ok := splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "CopyFiles") {
				continue
			}

			for _, s := range splitComma(v) {
				s = strings.Trim(strings.TrimSpace(s), `"`)
				if s == "" {
					continue
				}

				// @file.sys 简写：直接就是文件名
				if strings.HasPrefix(s, "@") {
					if strings.HasSuffix(strings.ToLower(s[1:]), ".sys") {
						files = append(files, strings.ToLower(s[1:]))
					}
					continue
				}

				copySections = append(copySections, strings.ToLower(s))
			}
		}
	}

	// 从 CopyFiles 段中提取文件名，只保留 .sys
	for _, sec := range copySections {
		for _, line := range inf.Sections[sec] {
			// 条目格式：<目标文件>[, <源文件>[, <标志>]]
			fields := splitComma(line)
			if len(fields) == 0 {
				continue
			}

			name := strings.Trim(strings.TrimSpace(fields[0]), `"`)
			if name == "" || strings.HasPrefix(name, ";") {
				continue
			}

			if strings.HasSuffix(strings.ToLower(name), ".sys") {
				files = append(files, strings.ToLower(name))
			}
		}
	}

	// 2) ServiceBinary 声明：ServiceBinary = %12%\xxx.sys
	for _, lines := range inf.Sections {
		for _, line := range lines {
			k, v, ok := splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "ServiceBinary") {
				continue
			}

			fields := splitComma(v)
			if len(fields) == 0 {
				continue
			}

			p := strings.Trim(strings.TrimSpace(fields[0]), `"`)

			// 取路径最后一段（文件名）
			if i := strings.LastIndexAny(p, `\/`); i >= 0 {
				p = p[i+1:]
			}

			if strings.HasSuffix(strings.ToLower(p), ".sys") {
				files = append(files, strings.ToLower(p))
			}
		}
	}

	return funk.UniqString(files)
}

func splitKeyValue(line string) (key, value string, ok bool) {
	i := strings.Index(line, "=")
	if i < 0 {
		return "", "", false
	}

	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func splitComma(s string) []string {
	var out []string

	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}
