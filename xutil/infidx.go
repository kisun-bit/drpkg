// Package xutil - INF 驱动索引
//
// 将 Windows INF 驱动文件预解析为紧凑的内存索引，支持：
//   - 从 INF 目录构建索引（BuildInfIndex）
//   - 按硬件 ID 进行 O(1) 查找（FindByHwid）
//
// 设计参考 SDIO（Snappy Driver Installer Origin）的方案：
// 字符串池 + 偏移引用 + HWID 哈希表，避免运行时逐个解析 INF。
package xutil

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// String pool（字符串池，类似 SDIO 的 Txt 类）
// ---------------------------------------------------------------------------

// stringPool 维护全局字符串表。相同字符串只存一份，通过 uint32 偏移引用。
type stringPool struct {
	data    []byte // 拼接后的字节缓存
	offsets sync.Map
}

func newStringPool(capHint int) *stringPool {
	return &stringPool{data: make([]byte, 0, capHint)}
}

// intern 将 s 写入池，返回其偏移（零终止字节偏移）。相同字符串返回同一偏移。
func (p *stringPool) intern(s string) uint32 {
	if v, ok := p.offsets.Load(s); ok {
		return v.(uint32)
	}
	off := uint32(len(p.data))
	p.data = append(p.data, []byte(s)...)
	p.data = append(p.data, 0)
	p.offsets.Store(s, off)
	return off
}

func (p *stringPool) get(off uint32) string {
	if int(off) >= len(p.data) {
		return ""
	}
	end := off
	for end < uint32(len(p.data)) && p.data[end] != 0 {
		end++
	}
	return string(p.data[off:end])
}

// ---------------------------------------------------------------------------
// INF 解析器（内嵌版本）
// ---------------------------------------------------------------------------

// rawInf 是 INF 文件的中间解析结果。
type rawInf struct {
	sections             map[string][]string
	strings              map[string]string
	manufacturerSections map[string]bool
}

// parseInfFile 解析单个 INF 文件。
func parseInfFile(path string) (*rawInf, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	inf := &rawInf{
		sections:             make(map[string][]string),
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
		if i := strings.Index(line, ";"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if sec != "" {
			inf.sections[sec] = append(inf.sections[sec], line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// 解析 [Strings] 段
	for _, line := range inf.sections["strings"] {
		k, v, ok := _splitKeyValue(line)
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		inf.strings[k] = v
	}

	// 解析 [Manufacturer] 段引用的设备段名。
	//
	// INF 格式：%strkey% = models-section [, TargetOSVersion ...]
	// 实际设备段名 = models-section.TargetOSVersion1.TargetOSVersion2...
	// 因此需要将逗号分隔的字段用 "." 拼接，生成所有可能的前缀形式。
	for _, line := range inf.sections["manufacturer"] {
		_, v, ok := _splitKeyValue(line)
		if !ok {
			continue
		}
		fields := strings.Split(v, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		// 生成所有前缀拼接：第一个字段、前两个字段用 "." 拼接 ...
		for n := 1; n <= len(fields); n++ {
			name := strings.ToLower(strings.Join(fields[:n], "."))
			if name != "" {
				inf.manufacturerSections[name] = true
			}
		}
	}

	return inf, nil
}

func (inf *rawInf) expandStrings(s string) string {
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

func (inf *rawInf) classGuid() string {
	for _, line := range inf.sections["version"] {
		k, v, ok := _splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "ClassGUID") {
			continue
		}
		if g := strings.Trim(strings.TrimSpace(v), `"`); g != "" {
			return g
		}
	}
	return ""
}

func (inf *rawInf) class() string {
	for _, line := range inf.sections["version"] {
		k, v, ok := _splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "Class") {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"`)
	}
	return ""
}

func (inf *rawInf) provider() string {
	for _, line := range inf.sections["version"] {
		k, v, ok := _splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "Provider") {
			continue
		}
		return inf.expandStrings(strings.Trim(v, `"`))
	}
	return ""
}

func (inf *rawInf) driverVer() string {
	for _, line := range inf.sections["version"] {
		k, v, ok := _splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "DriverVer") {
			continue
		}
		return strings.TrimSpace(v)
	}
	return ""
}

func (inf *rawInf) catalogFile() string {
	for _, line := range inf.sections["version"] {
		k, v, ok := _splitKeyValue(line)
		if !ok || !strings.EqualFold(k, "CatalogFile") {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"`)
	}
	return ""
}

// hardwareIds 提取 INF 中所有硬件/兼容 ID（已小写）。
func (inf *rawInf) hardwareIds() []string {
	// 非设备段黑名单
	nonDevice := map[string]bool{
		"version": true, "manufacturer": true, "destinationdirs": true,
		"layoutfiles": true, "strings": true, "defaultinstall": true,
		"classinstall": true, "classinstall32": true, "controlflags": true,
	}

	var ids []string
	for sec, lines := range inf.sections {
		if nonDevice[sec] || strings.HasPrefix(sec, "sourcedisks") ||
			strings.HasPrefix(sec, "classinstall") {
			continue
		}
		if len(inf.manufacturerSections) > 0 && !inf.manufacturerSections[sec] {
			continue
		}
		for _, line := range lines {
			lower := strings.ToLower(inf.expandStrings(line))
			if !strings.Contains(lower, `\`) || !strings.Contains(lower, "=") {
				continue
			}
			fields := _splitComma(lower)
			for _, f := range fields[1:] {
				f = strings.TrimSpace(f)
				f = strings.Trim(f, `"`)
				if f == "" || strings.Contains(f, " ") || !strings.Contains(f, `\`) {
					continue
				}
				if strings.HasPrefix(f, "@") {
					continue
				}
				ids = append(ids, f)
			}
		}
	}
	return ids
}

// serviceNames 返回 INF AddService 声明的服务名列表（去重）。
func (inf *rawInf) serviceNames() []string {
	var svcs []string
	seen := make(map[string]bool)
	for sec, lines := range inf.sections {
		if !strings.HasSuffix(sec, ".services") {
			continue
		}
		for _, line := range lines {
			k, v, ok := _splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "AddService") {
				continue
			}
			fields := _splitComma(v)
			if len(fields) < 3 {
				continue
			}
			svc := strings.Trim(strings.TrimSpace(fields[0]), `"`)
			installSec := strings.ToLower(strings.Trim(strings.TrimSpace(fields[2]), `"`))
			if svc == "" || installSec == "" {
				continue
			}
			for _, l := range inf.sections[installSec] {
				if k2, _, ok2 := _splitKeyValue(l); ok2 && strings.EqualFold(k2, "ServiceBinary") {
					if !seen[strings.ToLower(svc)] {
						seen[strings.ToLower(svc)] = true
						svcs = append(svcs, svc)
					}
					break
				}
			}
		}
	}
	return svcs
}

// sysFile 返回指定服务对应的 .sys 文件名（不含路径）。
func (inf *rawInf) sysFile(svcName string) string {
	for sec, lines := range inf.sections {
		if !strings.HasSuffix(sec, ".services") {
			continue
		}
		for _, line := range lines {
			k, v, ok := _splitKeyValue(line)
			if !ok || !strings.EqualFold(k, "AddService") {
				continue
			}
			fields := _splitComma(v)
			if len(fields) < 3 {
				continue
			}
			svc := strings.Trim(strings.TrimSpace(fields[0]), `"`)
			if !strings.EqualFold(svc, svcName) {
				continue
			}
			installSec := strings.ToLower(strings.Trim(strings.TrimSpace(fields[2]), `"`))
			for _, l := range inf.sections[installSec] {
				k2, v2, ok2 := _splitKeyValue(l)
				if !ok2 || !strings.EqualFold(k2, "ServiceBinary") {
					continue
				}
				binFields := _splitComma(v2)
				if len(binFields) == 0 {
					continue
				}
				p := strings.Trim(strings.TrimSpace(binFields[0]), `"`)
				if i := strings.LastIndexAny(p, `\/`); i >= 0 {
					p = p[i+1:]
				}
				if strings.HasSuffix(strings.ToLower(p), ".sys") {
					return strings.ToLower(p)
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// INF 索引核心数据结构
// ---------------------------------------------------------------------------

// DriverInfo 是一次 HWID 查询返回的驱动信息。
type DriverInfo struct {
	InfPath   string   // INF 文件路径
	InfName   string   // INF 文件名
	ClassGUID string   // 设备类 GUID
	Class     string   // 设备类名
	Provider  string   // 提供商
	DriverVer string   // 驱动版本
	CatFile   string   // 编录文件
	SvcNames  []string // 驱动服务名列表
	SysFiles  []string // .sys 文件名列表
	Hwid      string   // 匹配到的硬件 ID
}

// infIdxEntry 是索引内部条目。字符串字段存 stringPool 偏移。
type infIdxEntry struct {
	infPath   uint32
	infName   uint32
	classGUID uint32
	class     uint32
	provider  uint32
	driverVer uint32
	catFile   uint32
	hwid      uint32
	svcNames  []string
	sysFiles  []string
}

// InfIndex 是预编译的 INF 索引，支持按硬件 ID 快速查找驱动。
type InfIndex struct {
	textPool *stringPool
	entries  []infIdxEntry
	hwidMap  map[string][]int // HWID → entries 索引列表
}

// BuildInfIndex 扫描 dir 下所有 .inf 文件并构建索引。
// 仅收录存储类（SCSIAdapter）和网络类（Net）驱动。
func BuildInfIndex(dir string) (*InfIndex, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read inf dir %s: %w", dir, err)
	}

	idx := &InfIndex{
		textPool: newStringPool(64 * 1024),
		hwidMap:  make(map[string][]int, 256),
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".inf") {
			continue
		}

		infPath := filepath.Join(dir, name)
		inf, err := parseInfFile(infPath)
		if err != nil {
			continue
		}

		cg := inf.classGuid()
		// 仅收录存储类（SCSIAdapter/HDC）和网络类（Net）
		if !_isStorageClass(cg) && !_isNetClass(cg) {
			continue
		}

		svcs := inf.serviceNames()
		hwids := inf.hardwareIds()
		if len(hwids) == 0 {
			continue
		}

		var sysFiles []string
		for _, svc := range svcs {
			if sf := inf.sysFile(svc); sf != "" {
				sysFiles = append(sysFiles, sf)
			}
		}

		for _, hwid := range hwids {
			entry := infIdxEntry{
				infPath:   idx.textPool.intern(infPath),
				infName:   idx.textPool.intern(name),
				classGUID: idx.textPool.intern(cg),
				class:     idx.textPool.intern(inf.class()),
				provider:  idx.textPool.intern(inf.provider()),
				driverVer: idx.textPool.intern(inf.driverVer()),
				catFile:   idx.textPool.intern(inf.catalogFile()),
				hwid:      idx.textPool.intern(hwid),
				svcNames:  svcs,
				sysFiles:  sysFiles,
			}
			idx.entries = append(idx.entries, entry)
			idx.hwidMap[hwid] = append(idx.hwidMap[hwid], len(idx.entries)-1)
		}
	}

	return idx, nil
}

// BuildInfIndexRecursive 递归扫描 root 下所有子目录中的 .inf 文件并构建索引。
//
// 仅收录存储类（SCSIAdapter、HDC）和网络类（Net）驱动。
// 多个子目录可能包含同名 INF（如不同 OS 版本的 viostor.inf），
// 索引会为每个 INF 文件独立建条目（按完整路径区分）。
func BuildInfIndexRecursive(root string) (*InfIndex, error) {
	idx := &InfIndex{
		textPool: newStringPool(256 * 1024),
		hwidMap:  make(map[string][]int, 1024),
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读的目录
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".inf") {
			return nil
		}

		inf, parseErr := parseInfFile(path)
		if parseErr != nil {
			return nil
		}

		cg := inf.classGuid()
		if !_isStorageClass(cg) && !_isNetClass(cg) {
			return nil
		}

		svcs := inf.serviceNames()
		hwids := inf.hardwareIds()
		if len(hwids) == 0 {
			return nil
		}

		var sysFiles []string
		for _, svc := range svcs {
			if sf := inf.sysFile(svc); sf != "" {
				sysFiles = append(sysFiles, sf)
			}
		}

		name := d.Name()
		for _, hwid := range hwids {
			entry := infIdxEntry{
				infPath:   idx.textPool.intern(path),
				infName:   idx.textPool.intern(name),
				classGUID: idx.textPool.intern(cg),
				class:     idx.textPool.intern(inf.class()),
				provider:  idx.textPool.intern(inf.provider()),
				driverVer: idx.textPool.intern(inf.driverVer()),
				catFile:   idx.textPool.intern(inf.catalogFile()),
				hwid:      idx.textPool.intern(hwid),
				svcNames:  svcs,
				sysFiles:  sysFiles,
			}
			idx.entries = append(idx.entries, entry)
			idx.hwidMap[hwid] = append(idx.hwidMap[hwid], len(idx.entries)-1)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk dir %s: %w", root, err)
	}
	return idx, nil
}

// FindByHwid 按硬件 ID 查找匹配的驱动列表。
// hwid 大小写不敏感（内部做小写归一化）。
// 返回去重后的 DriverInfo 切片；无匹配时返回 nil。
func (idx *InfIndex) FindByHwid(hwid string) []*DriverInfo {
	hwid = strings.ToLower(strings.TrimSpace(hwid))
	indices := idx.hwidMap[hwid]
	if len(indices) == 0 {
		return nil
	}

	result := make([]*DriverInfo, 0, len(indices))
	seen := make(map[string]bool, len(indices))
	for _, i := range indices {
		e := &idx.entries[i]
		key := idx.textPool.get(e.infPath) + "|" + idx.textPool.get(e.hwid)
		if seen[key] {
			continue
		}
		seen[key] = true

		result = append(result, &DriverInfo{
			InfPath:   idx.textPool.get(e.infPath),
			InfName:   idx.textPool.get(e.infName),
			ClassGUID: idx.textPool.get(e.classGUID),
			Class:     idx.textPool.get(e.class),
			Provider:  idx.textPool.get(e.provider),
			DriverVer: idx.textPool.get(e.driverVer),
			CatFile:   idx.textPool.get(e.catFile),
			SvcNames:  e.svcNames,
			SysFiles:  e.sysFiles,
			Hwid:      idx.textPool.get(e.hwid),
		})
	}
	return result
}

// Size 返回索引中条目（HWID 行）总数。
func (idx *InfIndex) Size() int { return len(idx.entries) }

// NumInfs 返回索引涉及的 INF 文件数量（去重）。
func (idx *InfIndex) NumInfs() int {
	set := make(map[string]bool, len(idx.entries))
	for i := range idx.entries {
		set[idx.textPool.get(idx.entries[i].infPath)] = true
	}
	return len(set)
}

// AllHwids 返回索引中所有去重排序后的 HWID。
func (idx *InfIndex) AllHwids() []string {
	keys := make([]string, 0, len(idx.hwidMap))
	for k := range idx.hwidMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AllClassGUIDs 返回索引中所有去重排序后的 ClassGUID。
func (idx *InfIndex) AllClassGUIDs() []string {
	set := make(map[string]bool, len(idx.entries))
	for i := range idx.entries {
		s := idx.textPool.get(idx.entries[i].classGUID)
		if s != "" {
			set[s] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AllProviders 返回索引中所有去重排序后的 Provider。
func (idx *InfIndex) AllProviders() []string {
	set := make(map[string]bool, len(idx.entries))
	for i := range idx.entries {
		s := idx.textPool.get(idx.entries[i].provider)
		if s != "" {
			set[s] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// _isStorageClass 判断 ClassGUID 是否属于存储类。
func _isStorageClass(guid string) bool {
	g := strings.ToLower(guid)
	return g == "{4d36e97b-e325-11ce-bfc1-08002be10318}" || // SCSIAdapter
		g == "{4d36e96a-e325-11ce-bfc1-08002be10318}" // HDC
}

// _isNetClass 判断 ClassGUID 是否属于网络类。
func _isNetClass(guid string) bool {
	return strings.EqualFold(guid, "{4D36E972-E325-11CE-BFC1-08002BE10318}")
}

func _splitKeyValue(line string) (string, string, bool) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// _splitComma 按逗号切分，正确处理引号内的逗号。
func _splitComma(s string) []string {
	var result []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			depth ^= 1
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(s[start:]))
	return result
}