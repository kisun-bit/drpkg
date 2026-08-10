package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

const usage = `drcli - 驱动库管理工具

用法:
  drcli <command> [options]

命令:
  list                 列举驱动资源
  delete               删除指定驱动
  match-win-virt       匹配 Windows 最优虚拟化驱动
  match-win-normal     匹配 Windows 最优普通硬件驱动
  match-linux-virt     匹配 Linux 最优虚拟化驱动
  match-linux-normal   匹配 Linux 最优普通硬件驱动
  add-win-virt         添加 Windows 虚拟化驱动
  add-win-normal       添加 Windows 普通硬件驱动
  add-linux-virt       添加 Linux 虚拟化驱动
  add-linux-normal     添加 Linux 普通硬件驱动
  help                 显示帮助信息

通用选项:
  --lib <dir>          驱动库目录 (必须)

使用 drcli <command> --help 查看具体命令的选项。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := parseArgs(os.Args[2:])

	if _, ok := args["help"]; ok || cmd == "help" || cmd == "-h" || cmd == "--help" {
		if cmdHelp := getHelp(cmd); cmdHelp != "" && cmd != "help" {
			fmt.Print(cmdHelp)
			return
		}
		fmt.Print(usage)
		return
	}

	var err error
	switch cmd {
	case "list":
		err = cmdList(args)
	case "delete":
		err = cmdDelete(args)
	case "match-win-virt":
		err = cmdMatchWinVirt(args)
	case "match-win-normal":
		err = cmdMatchWinNormal(args)
	case "match-linux-virt":
		err = cmdMatchLinuxVirt(args)
	case "match-linux-normal":
		err = cmdMatchLinuxNormal(args)
	case "add-win-virt":
		err = cmdAddWinVirt(args)
	case "add-win-normal":
		err = cmdAddWinNormal(args)
	case "add-linux-virt":
		err = cmdAddLinuxVirt(args)
	case "add-linux-normal":
		err = cmdAddLinuxNormal(args)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

// ============================================================
// 参数解析
// ============================================================

func parseArgs(raw []string) map[string]string {
	m := make(map[string]string)
	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if i+1 >= len(raw) || strings.HasPrefix(raw[i+1], "--") {
				m[key] = "true"
			} else {
				i++
				m[key] = raw[i]
			}
		} else if strings.HasPrefix(arg, "-") {
			key := strings.TrimPrefix(arg, "-")
			if i+1 >= len(raw) || strings.HasPrefix(raw[i+1], "-") {
				m[key] = "true"
			} else {
				i++
				m[key] = raw[i]
			}
		}
	}
	return m
}

func require(args map[string]string, keys ...string) {
	var missing []string
	for _, k := range keys {
		if v, ok := args[k]; !ok || v == "" {
			missing = append(missing, "--"+k)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "缺少必要参数: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}
}

func openLib(args map[string]string, readonly bool) *x2xlib.X2XLib {
	require(args, "lib")
	lib, err := x2xlib.NewX2XLib(args["lib"], readonly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开驱动库失败: %v\n", err)
		os.Exit(1)
	}
	return lib
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseSignatures 从命令行参数解析签名列表
// 格式: --signer <s1,s2,...> --hash <h1,h2,...>
// signer 和 hash 按位置一一对应，数量必须相同
func parseSignatures(args map[string]string) ([]x2xlib.Signature, error) {
	signerStr := args["signer"]
	hashStr := args["hash"]

	// 两个都没传，返回空
	if signerStr == "" && hashStr == "" {
		return nil, nil
	}

	signers := splitList(signerStr)
	hashes := splitList(hashStr)

	if len(signers) != len(hashes) {
		return nil, fmt.Errorf("--signer 和 --hash 的数量不一致 (signer=%d, hash=%d)", len(signers), len(hashes))
	}

	sigs := make([]x2xlib.Signature, len(signers))
	for i := range signers {
		sigs[i] = x2xlib.Signature{
			Signer: define.Signer(signers[i]),
			Hash:   define.Hash(hashes[i]),
		}
	}
	return sigs, nil
}

// parseSignature 从命令行参数解析单个签名
// 格式: --signer <s> --hash <h>
func parseSignature(args map[string]string) (x2xlib.Signature, error) {
	signerStr := args["signer"]
	hashStr := args["hash"]

	if signerStr == "" && hashStr == "" {
		return x2xlib.Signature{}, nil
	}

	if signerStr == "" || hashStr == "" {
		return x2xlib.Signature{}, fmt.Errorf("--signer 和 --hash 必须同时提供")
	}

	return x2xlib.Signature{
		Signer: define.Signer(signerStr),
		Hash:   define.Hash(hashStr),
	}, nil
}

// ============================================================
// 命令: list
// ============================================================

const helpList = `drcli list - 列举驱动资源

用法:
  drcli list --lib <dir> [--id <driver_id>]

选项:
  --lib <dir>          驱动库目录 (必须)
  --id <driver_id>     指定驱动ID (可选，为空则列出全部)
`

func cmdList(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpList)
		return nil
	}

	lib := openLib(args, true)
	defer lib.Close()

	drivers, err := lib.ListDriver(args["id"])
	if err != nil {
		return err
	}

	if len(drivers) == 0 {
		fmt.Println("没有找到驱动。")
		return nil
	}

	fmt.Printf("共 %d 个驱动:\n", len(drivers))
	fmt.Println(strings.Repeat("-", 80))
	for _, d := range drivers {
		fmt.Printf("  ID:           %s\n", d.Id)
		fmt.Printf("  名称:         %s\n", d.FriendlyName)
		fmt.Printf("  模块:         %s\n", strings.Join(d.Modules, ", "))
		fmt.Printf("  目录:         %s\n", d.Dir)
		fmt.Println(strings.Repeat("-", 80))
	}
	return nil
}

// ============================================================
// 命令: delete
// ============================================================

const helpDelete = `drcli delete - 删除指定驱动

用法:
  drcli delete --lib <dir> --id <driver_id>

选项:
  --lib <dir>          驱动库目录 (必须)
  --id <driver_id>     要删除的驱动ID (必须)
`

func cmdDelete(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpDelete)
		return nil
	}

	require(args, "id")
	lib := openLib(args, false)
	defer lib.Close()

	if err := lib.DeleteDriver(args["id"]); err != nil {
		return err
	}

	fmt.Printf("驱动 [%s] 已成功删除。\n", args["id"])
	return nil
}

// ============================================================
// 命令: match-win-virt
// ============================================================

const helpMatchWinVirt = `drcli match-win-virt - 匹配 Windows 最优虚拟化驱动

用法:
  drcli match-win-virt --lib <dir> --virt <type> --arch <arch> --win-ver <version> [--ignore-sig]

选项:
  --lib <dir>          驱动库目录 (必须)
  --virt <type>        虚拟化类型，如 kvm, vmware, hyperv (必须)
  --arch <arch>        架构，如 amd64, 386 (必须)
  --win-ver <version>  Windows 版本，如 win7, win10, win2k19 (必须)
  --ignore-sig         忽略签名检查 (可选)
`

func cmdMatchWinVirt(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpMatchWinVirt)
		return nil
	}

	require(args, "virt", "arch", "win-ver")
	lib := openLib(args, true)
	defer lib.Close()

	dr, err := lib.SelectWindowsBestVirtualDriver(
		define.HPVirtType(args["virt"]),
		args["arch"],
		define.WindowsVersion(args["win-ver"]),
		args["ignore-sig"] == "true",
	)
	if err != nil {
		return err
	}

	printDriverResource(dr)
	return nil
}

// ============================================================
// 命令: match-win-normal
// ============================================================

const helpMatchWinNormal = `drcli match-win-normal - 匹配 Windows 最优普通硬件驱动

用法:
  drcli match-win-normal --lib <dir> --arch <arch> --win-ver <version> --unipci <unipci> [--ignore-sig]

选项:
  --lib <dir>          驱动库目录 (必须)
  --arch <arch>        架构，如 amd64, 386 (必须)
  --win-ver <version>  Windows 版本，如 win7, win10, win2k19 (必须)
  --unipci <unipci>    统一 PCI 标识 (必须)
  --ignore-sig         忽略签名检查 (可选)
`

func cmdMatchWinNormal(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpMatchWinNormal)
		return nil
	}

	require(args, "arch", "win-ver", "unipci")
	lib := openLib(args, true)
	defer lib.Close()

	dr, err := lib.SelectWindowsBestNormalDriver(
		args["arch"],
		define.WindowsVersion(args["win-ver"]),
		args["unipci"],
		args["ignore-sig"] == "true",
	)
	if err != nil {
		return err
	}

	printDriverResource(dr)
	return nil
}

// ============================================================
// 命令: match-linux-virt
// ============================================================

const helpMatchLinuxVirt = `drcli match-linux-virt - 匹配 Linux 最优虚拟化驱动

用法:
  drcli match-linux-virt --lib <dir> --virt <type> --arch <arch> --family <family> --kernel <kernel> [--vendor <vendor>]

选项:
  --lib <dir>          驱动库目录 (必须)
  --virt <type>        虚拟化类型，如 kvm, vmware (必须)
  --arch <arch>        架构，如 amd64, arm64 (必须)
  --family <family>    发行版系列，如 debian, rhel (必须)
  --kernel <kernel>    内核版本，如 5.15.0 (必须)
  --vendor <vendor>    供应商 (可选)
`

func cmdMatchLinuxVirt(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpMatchLinuxVirt)
		return nil
	}

	require(args, "virt", "arch", "family", "kernel")
	lib := openLib(args, true)
	defer lib.Close()

	dr, err := lib.SelectLinuxBestVirtualDriver(
		define.HPVirtType(args["virt"]),
		args["arch"],
		args["family"],
		args["kernel"],
		args["vendor"],
	)
	if err != nil {
		return err
	}

	printDriverResource(dr)
	return nil
}

// ============================================================
// 命令: match-linux-normal
// ============================================================

const helpMatchLinuxNormal = `drcli match-linux-normal - 匹配 Linux 最优普通硬件驱动

用法:
  drcli match-linux-normal --lib <dir> --arch <arch> --family <family> --kernel <kernel> --unipci <unipci>

选项:
  --lib <dir>          驱动库目录 (必须)
  --arch <arch>        架构，如 amd64, arm64 (必须)
  --family <family>    发行版系列，如 debian, rhel (必须)
  --kernel <kernel>    内核版本，如 5.15.0 (必须)
  --unipci <unipci>    统一 PCI 标识 (必须)
`

func cmdMatchLinuxNormal(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpMatchLinuxNormal)
		return nil
	}

	require(args, "arch", "family", "kernel", "unipci")
	lib := openLib(args, true)
	defer lib.Close()

	dr, err := lib.SelectLinuxBestNormalDriver(
		args["arch"],
		args["family"],
		args["kernel"],
		args["unipci"],
	)
	if err != nil {
		return err
	}

	printDriverResource(dr)
	return nil
}

// ============================================================
// 命令: add-win-virt
// ============================================================

const helpAddWinVirt = `drcli add-win-virt - 添加 Windows 虚拟化驱动

用法:
  drcli add-win-virt --lib <dir> --name <name> --version <ver> --virt <type> --arch <arch> \
      --source <dir> --vendor <vendor> --remark <remark> \
      --modules <m1,m2,...> --win-vers <v1,v2,...> \
      [--signer <s1,s2,...> --hash <h1,h2,...>]

签名选项:
  --signer 和 --hash 按位置一一对应，数量必须相同。
  signer 可选值: sign-private, sign-vendor, sign-distro, sign-microsoft, sign-whql
  hash   可选值: sha1, sha224, sha256, sha384, sha512, unknown

示例:
  --signer vendor,microsoft --hash sha1,sha256

其他选项:
  --lib <dir>              驱动库目录 (必须)
  --name <name>            驱动名称 (必须)
  --version <ver>          驱动版本 (必须)
  --virt <type>            虚拟化类型 (必须)
  --arch <arch>            架构 (必须)
  --source <dir>           驱动源文件目录 (必须)
  --vendor <vendor>        供应商 (必须)
  --remark <remark>        备注 (可选)
  --modules <m1,m2,...>    模块列表，逗号分隔 (必须)
  --win-vers <v1,v2,...>   兼容 Windows 版本列表，逗号分隔 (必须)
`

func cmdAddWinVirt(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpAddWinVirt)
		return nil
	}

	require(args, "name", "version", "virt", "arch", "source", "vendor", "modules", "win-vers")
	lib := openLib(args, false)
	defer lib.Close()

	sigs, err := parseSignatures(args)
	if err != nil {
		return err
	}

	modules := splitList(args["modules"])
	winVersStr := splitList(args["win-vers"])
	winVers := make([]define.WindowsVersion, len(winVersStr))
	for i, v := range winVersStr {
		winVers[i] = define.WindowsVersion(v)
	}

	driverID, driverDir, err := lib.AddWindowsVirtualDriver(
		args["name"],
		args["version"],
		define.HPVirtType(args["virt"]),
		args["arch"],
		args["source"],
		args["vendor"],
		args["remark"],
		sigs,
		modules,
		winVers,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Windows 虚拟化驱动添加成功:\n")
	fmt.Printf("  ID:   %s\n", driverID)
	fmt.Printf("  目录: %s\n", driverDir)
	return nil
}

// ============================================================
// 命令: add-win-normal
// ============================================================

const helpAddWinNormal = `drcli add-win-normal - 添加 Windows 普通硬件驱动

用法:
  drcli add-win-normal --lib <dir> --name <name> --version <ver> --arch <arch> \
      --source <dir> --vendor <vendor> --remark <remark> \
      --module <module> --win-vers <v1,v2,...> --hw-ids <id1,id2,...> \
      [--signer <s1,s2,...> --hash <h1,h2,...>]

签名选项:
  --signer 和 --hash 按位置一一对应，数量必须相同。
  signer 可选值: sign-private, sign-vendor, sign-distro, sign-microsoft, sign-whql
  hash   可选值: sha1, sha224, sha256, sha384, sha512, unknown

示例:
  --signer vendor --hash sha1

其他选项:
  --lib <dir>              驱动库目录 (必须)
  --name <name>            驱动名称 (必须)
  --version <ver>          驱动版本 (必须)
  --arch <arch>            架构 (必须)
  --source <dir>           驱动源文件目录 (必须)
  --vendor <vendor>        供应商 (必须)
  --remark <remark>        备注 (可选)
  --module <module>        驱动模块文件名 (必须)
  --win-vers <v1,v2,...>   兼容 Windows 版本列表，逗号分隔 (必须)
  --hw-ids <id1,id2,...>   硬件 ID 列表，逗号分隔 (必须)
`

func cmdAddWinNormal(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpAddWinNormal)
		return nil
	}

	require(args, "name", "version", "arch", "source", "vendor", "module", "win-vers", "hw-ids")
	lib := openLib(args, false)
	defer lib.Close()

	sigs, err := parseSignatures(args)
	if err != nil {
		return err
	}

	winVersStr := splitList(args["win-vers"])
	winVers := make([]define.WindowsVersion, len(winVersStr))
	for i, v := range winVersStr {
		winVers[i] = define.WindowsVersion(v)
	}

	hwIds := splitList(args["hw-ids"])

	driverID, driverDir, err := lib.AddWindowsNormalDriver(
		args["name"],
		args["version"],
		args["arch"],
		args["source"],
		args["vendor"],
		args["remark"],
		sigs,
		args["module"],
		winVers,
		hwIds,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Windows 普通硬件驱动添加成功:\n")
	fmt.Printf("  ID:   %s\n", driverID)
	fmt.Printf("  目录: %s\n", driverDir)
	return nil
}

// ============================================================
// 命令: add-linux-virt
// ============================================================

const helpAddLinuxVirt = `drcli add-linux-virt - 添加 Linux 虚拟化驱动

用法:
  drcli add-linux-virt --lib <dir> --name <name> --version <ver> --virt <type> --arch <arch> \
      --source <dir> --vendor <vendor> --remark <remark> --family <family> \
      --modules <m1,m2,...> --kernels <k1,k2,...> \
      [--signer <s> --hash <h>]

签名选项:
  --signer 和 --hash 必须同时提供或同时省略。
  signer 可选值: sign-private, sign-vendor, sign-distro, sign-microsoft, sign-whql
  hash   可选值: sha1, sha224, sha256, sha384, sha512, unknown

示例:
  --signer distro --hash unknown

其他选项:
  --lib <dir>              驱动库目录 (必须)
  --name <name>            驱动名称 (必须)
  --version <ver>          驱动版本 (必须)
  --virt <type>            虚拟化类型 (必须)
  --arch <arch>            架构 (必须)
  --source <dir>           驱动源文件目录 (必须)
  --vendor <vendor>        供应商 (必须)
  --remark <remark>        备注 (可选)
  --family <family>        发行版系列 (必须)
  --modules <m1,m2,...>    模块列表，逗号分隔 (必须)
  --kernels <k1,k2,...>    兼容内核版本列表，逗号分隔 (必须)
`

func cmdAddLinuxVirt(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpAddLinuxVirt)
		return nil
	}

	require(args, "name", "version", "virt", "arch", "source", "vendor", "family", "modules", "kernels")
	lib := openLib(args, false)
	defer lib.Close()

	sig, err := parseSignature(args)
	if err != nil {
		return err
	}

	modules := splitList(args["modules"])
	kernels := splitList(args["kernels"])

	driverID, driverDir, err := lib.AddLinuxVirtualDriver(
		args["name"],
		args["version"],
		define.HPVirtType(args["virt"]),
		args["arch"],
		args["source"],
		args["vendor"],
		args["remark"],
		args["family"],
		sig,
		modules,
		kernels,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Linux 虚拟化驱动添加成功:\n")
	fmt.Printf("  ID:   %s\n", driverID)
	fmt.Printf("  目录: %s\n", driverDir)
	return nil
}

// ============================================================
// 命令: add-linux-normal
// ============================================================

const helpAddLinuxNormal = `drcli add-linux-normal - 添加 Linux 普通硬件驱动

用法:
  drcli add-linux-normal --lib <dir> --name <name> --version <ver> --arch <arch> \
      --source <dir> --vendor <vendor> --remark <remark> --family <family> \
      --modules <m1,m2,...> --kernels <k1,k2,...> --aliases <a1,a2,...> \
      [--signer <s> --hash <h>]

签名选项:
  --signer 和 --hash 必须同时提供或同时省略。
  signer 可选值: sign-private, sign-vendor, sign-distro, sign-microsoft, sign-whql
  hash   可选值: sha1, sha224, sha256, sha384, sha512, unknown

示例:
  --signer distro --hash unknown

其他选项:
  --lib <dir>              驱动库目录 (必须)
  --name <name>            驱动名称 (必须)
  --version <ver>          驱动版本 (必须)
  --arch <arch>            架构 (必须)
  --source <dir>           驱动源文件目录 (必须)
  --vendor <vendor>        供应商 (必须)
  --remark <remark>        备注 (可选)
  --family <family>        发行版系列 (必须)
  --modules <m1,m2,...>    模块列表，逗号分隔 (必须)
  --kernels <k1,k2,...>    兼容内核版本列表，逗号分隔 (必须)
  --aliases <a1,a2,...>    兼容硬件别名列表，逗号分隔 (必须)
`

func cmdAddLinuxNormal(args map[string]string) error {
	if _, ok := args["help"]; ok {
		fmt.Print(helpAddLinuxNormal)
		return nil
	}

	require(args, "name", "version", "arch", "source", "vendor", "family", "modules", "kernels", "aliases")
	lib := openLib(args, false)
	defer lib.Close()

	sig, err := parseSignature(args)
	if err != nil {
		return err
	}

	modules := splitList(args["modules"])
	kernels := splitList(args["kernels"])
	aliases := splitList(args["aliases"])

	driverID, driverDir, err := lib.AddLinuxNormalDriver(
		args["name"],
		args["version"],
		args["arch"],
		args["source"],
		args["vendor"],
		args["remark"],
		args["family"],
		sig,
		modules,
		kernels,
		aliases,
	)
	if err != nil {
		return err
	}

	fmt.Printf("Linux 普通硬件驱动添加成功:\n")
	fmt.Printf("  ID:   %s\n", driverID)
	fmt.Printf("  目录: %s\n", driverDir)
	return nil
}

// ============================================================
// 辅助函数
// ============================================================

func printDriverResource(dr *x2xlib.DriverResource) {
	fmt.Println("匹配到最优驱动:")
	fmt.Printf("  ID:     %s\n", dr.Id)
	fmt.Printf("  名称:   %s\n", dr.FriendlyName)
	fmt.Printf("  模块:   %s\n", strings.Join(dr.Modules, ", "))
	fmt.Printf("  目录:   %s\n", dr.Dir)
}

func getHelp(cmd string) string {
	switch cmd {
	case "list":
		return helpList
	case "delete":
		return helpDelete
	case "match-win-virt":
		return helpMatchWinVirt
	case "match-win-normal":
		return helpMatchWinNormal
	case "match-linux-virt":
		return helpMatchLinuxVirt
	case "match-linux-normal":
		return helpMatchLinuxNormal
	case "add-win-virt":
		return helpAddWinVirt
	case "add-win-normal":
		return helpAddWinNormal
	case "add-linux-virt":
		return helpAddLinuxVirt
	case "add-linux-normal":
		return helpAddLinuxNormal
	default:
		return ""
	}
}
