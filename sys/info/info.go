package info

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/sys/info/network"
	"github.com/kisun-bit/drpkg/sys/info/storage"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/basic"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type updateJsonFunc func(key string, value interface{}) error

func baseInfoJson() (string, error) {
	json_ := "{}"

	var updateJSON updateJsonFunc = func(key string, value interface{}) error {
		var err error
		json_, err = sjson.Set(json_, key, value)
		if err != nil {
			return errors.Errorf("failed to set %s info to json, %v", key, err)
		}
		return nil
	}

	// 获取CPU信息
	if err := retrieveCPUInfo(updateJSON); err != nil {
		return "", err
	}

	// 获取内存信息
	if err := retrieveMemoryInfo(updateJSON); err != nil {
		return "", err
	}

	// 获取网络信息
	if err := fetchNetworkInfo(updateJSON); err != nil {
		return "", err
	}

	// 获取路由信息
	if err := fetchRoutingInfo(updateJSON); err != nil {
		return "", err
	}

	// 主机名、操作系统、架构、引导模式
	if err := updateJSON("hostname", fetchHostname()); err != nil {
		return "", err
	}

	if err := updateJSON("goos", runtime.GOOS); err != nil {
		return "", err
	}

	if err := updateJSON("goarch", runtime.GOARCH); err != nil {
		return "", err
	}

	// 是否为LiveCD/PE等内存操作系统.
	if err := updateJSON("is_live_os", ioctl.IsLiveCDEnv()); err != nil {
		return "", err
	}

	// 主板生产商
	if err := updateJSON("system_manufacturer", ioctl.SystemManufacturer()); err != nil {
		return "", err
	}

	// 是否是虚拟机
	if err := updateJSON("is_virtual", ioctl.IsVirtualGuest()); err != nil {
		return "", err
	}

	bootMode := detectBootMode()
	if err := updateJSON("boot_mode", bootMode); err != nil {
		return "", err
	}

	return json_, nil
}

func retrieveCPUInfo(updateJSON updateJsonFunc) error {
	cpus, err := cpu.Info()
	if err != nil {
		return err
	}
	for i := 0; i < len(cpus); i++ {
		cpus[i].Flags = nil
	}
	return updateJSON("cpu", cpus)
}

func retrieveMemoryInfo(updateJSON updateJsonFunc) error {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	return updateJSON("memory", vmem)
}

func fetchNetworkInfo(updateJSON updateJsonFunc) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return errors.Errorf("failed to query network info, %v", err)
	}
	eths := make([]network.Ethernet, 0)
	for _, i := range interfaces {
		e := network.Ethernet{
			IPv4BootProto:      ioctl.BootProtoNone,
			IPv6BootProto:      ioctl.BootProtoDHCP,
			IPv4GatewayList:    make([]string, 0),
			IPv6GatewayList:    make([]string, 0),
			IPv4DnsList:        make([]string, 0),
			IPv6DnsList:        make([]string, 0),
			FixedInterfaceStat: network.FixedInterfaceStat(i),
		}
		extraInfo, ok := ioctl.QueryExtraInfoForEth(i.Name)
		if ok {
			e.Physical = extraInfo.Physical
			e.IPv4BootProto = extraInfo.IPv4bootProto
			e.IPv6BootProto = extraInfo.IPv6bootProto
			e.IfCfgPath = extraInfo.IfCfgPath
			e.IPv4GatewayList = append(e.IPv4GatewayList, extraInfo.IPv4gatewayList...)
			e.IPv6GatewayList = append(e.IPv6GatewayList, extraInfo.IPv6gatewayList...)
			e.IPv4DnsList = append(e.IPv4DnsList, extraInfo.IPv4dnsList...)
			e.IPv6DnsList = append(e.IPv6DnsList, extraInfo.IPv6dnsList...)
		}
		eths = append(eths, e)
	}
	return updateJSON("nic", eths)
}

func fetchRoutingInfo(updateJSON updateJsonFunc) error {
	rs, err := ioctl.GetRoutingTable(context.Background())
	if err != nil {
		return nil // 如果获取路由表失败，不影响其他信息
	}
	rgs := make([]ioctl.RouteGeneral, 0)
	for _, route_ := range rs {
		if !route_.Default && route_.RoutedNet.String() == "<nil>" {
			continue
		}
		rgs = append(rgs, route_.Convert2RouteGeneral())
	}
	return updateJSON("route_list", rgs)
}

func fetchHostname() string {
	hn, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hn
}

func detectBootMode() string {
	if ioctl.IsBootByUEFI() {
		return "UEFI"
	}
	return "BIOS"
}

func NewSystemDebugInfo(json_ string) (debug string) {
	formatKeyValue := func(key, value string) string {
		return fmt.Sprintf("%-15s: %s", key, value)
	}

	generateLVMObjects := func(list gjson.Result) []string {
		var contents []string
		for i, item := range list.Array() {
			debugContent := item.Get("brief").String()
			if item.Get("effective_for_boot").Bool() {
				debugContent += " *"
			}
			if i != 0 {
				debugContent = fmt.Sprintf("%17s", "") + debugContent
			}
			contents = append(contents, debugContent)
		}
		if len(contents) == 0 {
			contents = append(contents, "--")
		}
		return contents
	}

	debugContents := []string{
		formatKeyValue("Manufacturer", gjson.Get(json_, "system_manufacturer").String()),
		formatKeyValue("IsVirtual", ifElse(gjson.Get(json_, "is_virtual").Bool(), "Yes", "No")),
		formatKeyValue("IsLiveOS", ifElse(gjson.Get(json_, "is_live_os").Bool(), "Yes", "No")),
		formatKeyValue("Hostname", gjson.Get(json_, "hostname").String()),
		formatKeyValue("OS Name", gjson.Get(json_, "os_name").String()),
		formatKeyValue("OS Version", gjson.Get(json_, "os_version").String()),
		formatKeyValue("GOOS", gjson.Get(json_, "goos").String()),
		formatKeyValue("GOARCH", gjson.Get(json_, "goarch").String()),
		formatKeyValue("Boot Mode", gjson.Get(json_, "boot_mode").String()),
	}

	if grubver := gjson.Get(json_, "grub_version"); grubver.Exists() {
		debugContents = append(debugContents,
			formatKeyValue("GRUB Version", grubver.String()),
			formatKeyValue("GRUB Target", gjson.Get(json_, "grub_target").String()),
		)
	}

	cpuDetails := gjson.Get(json_, "cpu").Array()
	cpuModel, cpuCores := getCPUInfo(cpuDetails)
	debugContents = append(debugContents,
		formatKeyValue("CPU Number", fmt.Sprintf("%d", len(cpuDetails))),
		formatKeyValue("CPU Model", cpuModel),
		formatKeyValue("CPU Cores", fmt.Sprintf("%d", cpuCores)),
	)

	mem_ := gjson.Get(json_, "memory")
	debugContents = append(debugContents,
		formatKeyValue("Memory Size", basic.TrimAllSpace(humanize.IBytes(uint64(mem_.Get("total").Int())))),
		formatKeyValue("Memory Used", basic.TrimAllSpace(humanize.IBytes(uint64(mem_.Get("used").Int())))),
		formatKeyValue("Memory Avail", basic.TrimAllSpace(humanize.IBytes(uint64(mem_.Get("available").Int())))),
	)

	if kernel := gjson.Get(json_, "default_kernel"); kernel.Exists() {
		kernelContents := getKernelInfo(gjson.Get(json_, "kernels"))
		debugContents = append(debugContents,
			formatKeyValue("Kernel(default)", kernel.String()),
			formatKeyValue("Kernel Image", gjson.Get(json_, "kernel_img").String()),
			formatKeyValue("Initrd Image", gjson.Get(json_, "initrd").String()),
			strings.Join(kernelContents, "\n"),
		)
	}

	nicContents := getNICInfo(gjson.Get(json_, "nic"))
	debugContents = append(debugContents, formatKeyValue("Networks", strings.Join(nicContents, "\n")))

	routeContents := getRouteInfo(gjson.Get(json_, "route_list"))
	debugContents = append(debugContents, formatKeyValue("Route(4&6)", strings.Join(routeContents, "\n")))

	storageContents := getStorageInfo(gjson.Get(json_, "storage"))
	debugContents = append(debugContents, formatKeyValue("Disks", strings.Join(storageContents, "\n")))

	if gjson.Get(json_, "lvm").Exists() {
		supportLVM := gjson.Get(json_, "lvm.support_lvm").Bool()
		debugContents = append(debugContents, formatKeyValue("Support LVM", ifElse(supportLVM, "Yes", "No")))
		if supportLVM && gjson.Get(json_, "lvm.major_version").Exists() {
			debugContents = append(debugContents,
				formatKeyValue("LVM Version", gjson.Get(json_, "lvm.major_version").String()),
			)
		}
	}

	lvmTypes := map[string]string{
		"PV":    "lvm.pv_list",
		"VG":    "lvm.vg_list",
		"LV":    "lvm.lv_list",
		"Swaps": "swap",
	}

	for key, path := range lvmTypes {
		if list := gjson.Get(json_, path); list.Exists() {
			debugContents = append(debugContents, formatKeyValue(key, strings.Join(generateLVMObjects(list), "\n")))
		}
	}

	return strings.Join(debugContents, "\n")
}

func ifElse(cond bool, trueVal, falseVal string) string {
	if cond {
		return trueVal
	}
	return falseVal
}

func getCPUInfo(cpuDetails []gjson.Result) (model string, cores int) {
	if len(cpuDetails) == 0 {
		return "--", 0
	}
	model = cpuDetails[0].Get("modelName").String()
	for _, r := range cpuDetails {
		cores += int(r.Get("cores").Int())
	}
	return model, cores
}

func getKernelInfo(kernels gjson.Result) []string {
	var kernelContents []string
	for i, k := range kernels.Array() {
		line := fmt.Sprintf("%-15s: %s", "Kernels", k.String())
		if i != 0 {
			line = fmt.Sprintf("%17s", "") + k.String()
		}
		kernelContents = append(kernelContents, line)
	}
	if len(kernelContents) == 0 {
		kernelContents = append(kernelContents, "--")
	}
	return kernelContents
}

func getNICInfo(nics gjson.Result) []string {
	var nicContents []string
	for i, nic := range nics.Array() {
		name := nic.Get("name").String()
		if name == "" {
			name = "--"
		}
		mac := nic.Get("hardware_addr").String()
		if mac == "" {
			mac = "--"
		}
		var addrsContent []string
		for _, addr := range nic.Get("addrs").Array() {
			addrsContent = append(addrsContent, addr.Get("addr").String())
		}
		if len(addrsContent) == 0 {
			addrsContent = append(addrsContent, "--")
		}
		nicContent := fmt.Sprintf("%s (MAC:%s) [%s]", name, mac, strings.Join(addrsContent, ", "))
		if i != 0 {
			nicContent = fmt.Sprintf("%17s", "") + nicContent
		}
		nicContents = append(nicContents, nicContent)
	}
	if len(nicContents) == 0 {
		nicContents = append(nicContents, "--")
	}
	return nicContents
}

func getRouteInfo(routes gjson.Result) []string {
	var routeContents []string
	for i, route_ := range routes.Array() {
		routeObj := ioctl.RouteGeneral{}
		_ = json.Unmarshal([]byte(route_.Raw), &routeObj)
		if routeObj.Gateway == "0.0.0.0" || routeObj.Gateway == "::" {
			continue
		}
		routeContent := routeObj.String()
		if i != 0 {
			routeContent = fmt.Sprintf("%17s", "") + routeContent
		}
		routeContents = append(routeContents, routeContent)
	}
	if len(routeContents) == 0 {
		routeContents = append(routeContents, "--")
	}
	return routeContents
}

func getStorageInfo(storage gjson.Result) []string {
	var storageContents []string
	for i, s := range storage.Array() {
		diskDebugContent := s.Get("brief").String()
		if s.Get("effective_for_boot").Bool() {
			diskDebugContent += " *"
		}
		if i != 0 {
			diskDebugContent = fmt.Sprintf("%17s", "") + diskDebugContent
		}
		var partContents []string
		for _, p := range s.Get("parts").Array() {
			partContent := p.Get("brief").String()
			if p.Get("effective_for_boot").Bool() {
				partContent += " *"
			}
			partContents = append(partContents, fmt.Sprintf("%26s%s", "", partContent))
		}
		if len(partContents) > 0 {
			diskDebugContent = diskDebugContent + "\n" + strings.Join(partContents, "\n")
		}
		storageContents = append(storageContents, diskDebugContent)
	}
	if len(storageContents) == 0 {
		storageContents = append(storageContents, "--")
	}
	return storageContents
}

func FindDiskJsonObjectByUniqueID(info string, uniqueID string) (result gjson.Result, ok bool) {
	for _, sd := range gjson.Get(info, "storage").Array() {
		if sd.Get("unique_id_md5").String() == uniqueID {
			return sd, true
		}
	}
	return result, false
}

func LvList(info string) (lvs []storage.LV) {
	lvListJson := gjson.Get(info, "lvm.lv_list")
	if len(lvListJson.Array()) == 0 {
		return lvs
	}
	_ = json.Unmarshal([]byte(lvListJson.Raw), &lvs)
	return lvs
}

func VgList(info string) (vgs []storage.VG) {
	vgListJson := gjson.Get(info, "lvm.vg_list")
	if len(vgListJson.Array()) == 0 {
		return vgs
	}
	_ = json.Unmarshal([]byte(vgListJson.Raw), &vgs)
	return vgs
}

func PvList(info string) (pvs []storage.PV) {
	pvListJson := gjson.Get(info, "lvm.pv_list")
	if len(pvListJson.Array()) == 0 {
		return pvs
	}
	_ = json.Unmarshal([]byte(pvListJson.Raw), &pvs)
	return pvs
}

func MatchDiskForPvOrDevice(pvOrDevName, info string) (diskUniqueID, diskPath string, err error) {
	for _, sd := range gjson.Get(info, "storage").Array() {
		diskUniqueID, diskPath = sd.Get("unique_id_md5").String(), sd.Get("disk_path").String()
		if diskPath == pvOrDevName {
			return diskUniqueID, diskPath, nil
		}
		for _, p := range sd.Get("parts").Array() {
			if p.Get("volume_path").String() == pvOrDevName {
				return diskUniqueID, diskPath, nil
			}
		}
	}
	return "", "", errors.Errorf("can not find disk info for PV(or Device) named `%s`", pvOrDevName)
}

func ConvertLocalPartToTargetPart(localPartName, localInfo, targetDiskUniqueID, targetInfo string) (targetPartName string, err error) {
	_, localDiskPath, err := MatchDiskForPvOrDevice(localPartName, localInfo)
	if err != nil {
		return "", err
	}
	targetDiskJson, ok := FindDiskJsonObjectByUniqueID(targetInfo, targetDiskUniqueID)
	if !ok {
		return "", errors.Errorf("can not find disk info in target by unique-id is `%s`", targetDiskUniqueID)
	}
	targetDiskPath := targetDiskJson.Get("disk_path").String()
	return generateTargetDevicePath(localPartName, localDiskPath, targetDiskPath), nil
}

func ConvertLocalPvToTargetPV(localPvName, localInfo, targetDiskUniqueID, targetInfo string) (targetPvName string, err error) {
	return ConvertLocalPartToTargetPart(localPvName, localInfo, targetDiskUniqueID, targetInfo)
}

func ConvertLocalVgPvsToTargetVgPvs(localVgPvs []string, localInfo string, localDisk2TargetDiskMap map[string]string,
	targetInfo string) (targetVgPvs []string, err error) {
	for _, curLocalPvName := range localVgPvs {
		curLocalDiskUniqueID, _, err := MatchDiskForPvOrDevice(curLocalPvName, localInfo)
		if err != nil {
			return targetVgPvs, err
		}
		curTargetPvName, err := ConvertLocalPvToTargetPV(
			curLocalPvName,
			localInfo,
			localDisk2TargetDiskMap[curLocalDiskUniqueID],
			targetInfo)
		if err != nil {
			return targetVgPvs, err
		}
		targetVgPvs = append(targetVgPvs, curTargetPvName)
	}
	return targetVgPvs, nil
}

func generateTargetDevicePath(localDevicePath, localDiskPath, targetDiskPath string) string {
	// 第一大类情况, 若localDevicePath==localDiskPath, 即设备为磁盘：
	//   直接进行字符替换即可
	// 第二大类情况，设备为分区，需以磁盘结尾情况, 分情况处理：：
	// 数字  数字：
	// 数字  字符：
	// 字符  字符：
	// 字符  数字：
	if localDevicePath == localDiskPath {
		return targetDiskPath
	}
	if (basic.IsLastCharDigit(localDiskPath) && basic.IsLastCharDigit(targetDiskPath)) ||
		(!basic.IsLastCharDigit(localDiskPath) && !basic.IsLastCharDigit(targetDiskPath)) {
		return strings.Replace(localDevicePath, localDiskPath, targetDiskPath, 1)
	} else if basic.IsLastCharDigit(localDiskPath) && !basic.IsLastCharDigit(targetDiskPath) {
		return strings.Replace(localDevicePath, localDiskPath+"p", targetDiskPath, 1)
	} else {
		return strings.Replace(localDevicePath, localDiskPath, targetDiskPath+"p", 1)
	}
}

func MatchTargetSwapDevice(localSwap string, localInfo string, localDisk2TargetDiskMap map[string]string,
	targetInfo string) (targetSwap string, err error) {
	for _, lv := range LvList(localInfo) {
		if lv.DmName == filepath.Base(localSwap) {
			return lv.VolumePath, nil
		}
	}
	for _, diskJson := range gjson.Get(localInfo, "storage").Array() {
		localDiskPath := diskJson.Get("disk_path").String()
		localDiskUniqueID := diskJson.Get("unique_id_md5").String()
		tDiskJson, ok := FindDiskJsonObjectByUniqueID(targetInfo, localDisk2TargetDiskMap[localDiskUniqueID])
		targetDiskPath := tDiskJson.Get("disk_path").String()
		if !ok {
			continue
		}
		if localSwap == localDiskPath {
			return tDiskJson.Get("disk_path").String(), nil
		}
		for _, partJson := range diskJson.Get("parts").Array() {
			localPartPath := partJson.Get("volume_path").String()
			if localPartPath != localSwap {
				continue
			}
			return generateTargetDevicePath(localSwap, localDiskPath, targetDiskPath), nil
		}
	}
	return "", errors.Errorf("device of %s not found", localSwap)
}
