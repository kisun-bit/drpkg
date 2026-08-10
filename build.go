//go:build ignore_build_go
// +build ignore_build_go

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ============================================================
// 编译配置
// ============================================================

// BuildTarget 单个编译目标.
type BuildTarget struct {
	Name string // 程序名 (也用作输出目录名)
	Main string // 主程序路径, 如 ./cmd/drfirstboot
}

// allTargets 所有需要编译的程序列表.
var allTargets = []BuildTarget{
	{Name: "drfirstboot", Main: "./cmd/drfirstboot"},
	{Name: "drfix", Main: "./cmd/drfix"},
	{Name: "drx2xlib", Main: "./cmd/drx2xlib"},
}

// MinGoVersion 支持的最小 Go 编译版本.
var MinGoVersion = GoVersion{Major: 1, Minor: 20, Patch: 0}

// DefaultBuildTags 默认 build tags.
var DefaultBuildTags []string

// TestPackages 需要测试的包路径 (留空则跳过测试).
var TestPackages []string

// ============================================================
// 类型定义
// ============================================================

// GoVersion Go 版本.
type GoVersion struct {
	Major int
	Minor int
	Patch int
}

// Constants 通过 -ldflags -X 注入的常量.
type Constants map[string]string

// ============================================================
// 全局变量
// ============================================================

var (
	verbose   bool
	runTests  bool
	enableCGO bool
	enablePIE bool
	goVersion = ParseGoVersion(runtime.Version())
)

const (
	CGOEnabled     = "CGO_ENABLED=0"
	EnvGOARCH      = "GOARCH"
	EnvGOOS        = "GOOS"
	EnvGO111MODULE = "GO111MODULE"
	EnvGOARM       = "GOARM"
)

// ============================================================
// 工具函数
// ============================================================

func die(message string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, message, args...)
	os.Exit(1)
}

func verbosePrintf(message string, args ...interface{}) {
	if !verbose {
		return
	}
	fmt.Printf("build: "+message, args...)
}

func showUsage(output io.Writer) {
	_, _ = fmt.Fprintf(output, "USAGE: go run build.go OPTIONS\n")
	_, _ = fmt.Fprintf(output, "\n")
	_, _ = fmt.Fprintf(output, "OPTIONS:\n")
	_, _ = fmt.Fprintf(output, "  -v     --verbose       output more messages\n")
	_, _ = fmt.Fprintf(output, "  -t     --tags          specify additional build tags\n")
	_, _ = fmt.Fprintf(output, "  -T     --test          run tests\n")
	_, _ = fmt.Fprintf(output, "  -o     --output        set output file name (only works with --target)\n")
	_, _ = fmt.Fprintf(output, "         --target name   build only the specified program (e.g. drfirstboot)\n")
	_, _ = fmt.Fprintf(output, "         --enable-cgo    use CGO to link against libc\n")
	_, _ = fmt.Fprintf(output, "         --enable-pie    use PIE buildmode\n")
	_, _ = fmt.Fprintf(output, "         --goos value    set GOOS for cross-compilation\n")
	_, _ = fmt.Fprintf(output, "         --goarch value  set GOARCH for cross-compilation\n")
	_, _ = fmt.Fprintf(output, "         --goarm value   set GOARM for cross-compilation\n")
	_, _ = fmt.Fprintf(output, "\n")
	_, _ = fmt.Fprintf(output, "EXAMPLES:\n")
	_, _ = fmt.Fprintf(output, "  go run build.go                              # build all targets\n")
	_, _ = fmt.Fprintf(output, "  go run build.go --target drfix               # build only drfix\n")
	_, _ = fmt.Fprintf(output, "  go run build.go --goos linux --goarch amd64  # cross-compile all for linux/amd64\n")
}

// ============================================================
// Go 版本解析与比较
// ============================================================

// ParseGoVersion 解析 Go 版本字符串 (如 "go1.20.3").
func ParseGoVersion(s string) (v GoVersion) {
	if !strings.HasPrefix(s, "go") {
		return
	}

	s = s[2:]
	data := strings.Split(s, ".")
	if len(data) < 2 || len(data) > 3 {
		return GoVersion{}
	}

	var err error
	v.Major, err = strconv.Atoi(data[0])
	if err != nil {
		return GoVersion{}
	}

	// 尝试解析次要版本，同时删除最终的后缀（如 "rc2"）
	for s := data[1]; s != ""; s = s[:len(s)-1] {
		v.Minor, err = strconv.Atoi(s)
		if err == nil {
			break
		}
	}

	if v.Minor == 0 {
		return GoVersion{}
	}

	if len(data) >= 3 {
		v.Patch, err = strconv.Atoi(data[2])
		if err != nil {
			return GoVersion{}
		}
	}

	return
}

// AtLeast 判断当前版本是否 >= other.
// 修复: 原版在大版本相同时直接比较 Minor 会导致 go1.20 vs go1.19.5 判断错误.
func (v GoVersion) AtLeast(other GoVersion) bool {
	var empty GoVersion
	if v == empty {
		return true // 空版本视为满足所有要求
	}

	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch >= other.Patch
}

func (v GoVersion) String() string {
	return fmt.Sprintf("Go %d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ============================================================
// LDFlags
// ============================================================

func (cs Constants) LDFlags() string {
	l := make([]string, 0, len(cs))
	for k, v := range cs {
		l = append(l, fmt.Sprintf(`-X "%s=%s"`, k, v))
	}
	return strings.Join(l, " ")
}

// ============================================================
// 版本获取
// ============================================================

func getVersionFromFile() string {
	buf, err := os.ReadFile("VERSION")
	if err != nil {
		verbosePrintf("error reading file VERSION: %v\n", err)
		return ""
	}
	return strings.TrimSpace(string(buf))
}

func getVersionFromGit() string {
	cmd := exec.Command("git", "describe", "--long", "--tags", "--dirty", "--always")
	out, err := cmd.Output()
	if err != nil {
		verbosePrintf("git describe returned error: %v\n", err)
		return ""
	}
	version := strings.TrimSpace(string(out))
	verbosePrintf("git version is %s\n", version)
	return version
}

func getVersion() string {
	versionFile := getVersionFromFile()
	versionGit := getVersionFromGit()

	verbosePrintf("version from file 'VERSION' is %q, version from git %q\n",
		versionFile, versionGit)

	switch {
	case versionFile == "":
		return versionGit
	case versionGit == "":
		return versionFile
	}

	return fmt.Sprintf("%s (%s)", versionFile, versionGit)
}

// ============================================================
// 环境 & 参数解析
// ============================================================

func printEnv(env []string) {
	verbosePrintf("environment (GO*):\n")
	for _, v := range env {
		if !strings.HasPrefix(v, "GO") {
			continue
		}
		verbosePrintf("  %s\n", v)
	}
}

func setupEnvironment() map[string]string {
	return map[string]string{
		EnvGO111MODULE: "on",
		EnvGOOS:        runtime.GOOS,
		EnvGOARCH:      runtime.GOARCH,
		EnvGOARM:       "",
	}
}

func parseArguments(params []string, env map[string]string) (
	buildTags []string,
	outputFilename string,
	targetName string,
	err error,
) {
	buildTags = append([]string{}, DefaultBuildTags...)
	skipNext := false

	for i, arg := range params {
		if skipNext {
			skipNext = false
			continue
		}

		switch arg {
		case "-v", "--verbose":
			verbose = true
		case "-t", "-tags", "--tags":
			tags, e := getNextParam(params, i, "tag")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			buildTags = append(buildTags, strings.Split(tags, " ")...)
		case "-o", "--output":
			filename, e := getNextParam(params, i, "output filename")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			outputFilename = filename
		case "--target":
			name, e := getNextParam(params, i, "target name")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			targetName = name
		case "-T", "--test":
			runTests = true
		case "--enable-cgo":
			enableCGO = true
		case "--enable-pie":
			enablePIE = true
		case "--goos":
			goos, e := getNextParam(params, i, "GOOS")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			env[EnvGOOS] = goos
		case "--goarch":
			goarch, e := getNextParam(params, i, "GOARCH")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			env[EnvGOARCH] = goarch
		case "--goarm":
			goarm, e := getNextParam(params, i, "GOARM")
			if e != nil {
				err = e
				return
			}
			skipNext = true
			env[EnvGOARM] = goarm
		case "-h", "--help":
			showUsage(os.Stdout)
			os.Exit(0)
		default:
			err = fmt.Errorf("unknown option %q", arg)
			return
		}
	}

	return
}

func getNextParam(params []string, currentIndex int, paramName string) (string, error) {
	if currentIndex+1 >= len(params) {
		return "", fmt.Errorf("-%s given but no %s specified", paramName, paramName)
	}
	return params[currentIndex+1], nil
}

// ============================================================
// 编译核心
// ============================================================

func build(cwd string, env map[string]string, args ...string) error {
	a := []string{"build", "-trimpath"}

	if enablePIE {
		a = append(a, "-buildmode=pie")
	}

	a = append(a, args...)
	cmd := exec.Command("go", a...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if !enableCGO {
		cmd.Env = append(cmd.Env, CGOEnabled)
	}

	printEnv(cmd.Env)

	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	verbosePrintf("chdir %q\n", cwd)
	verbosePrintf("go %q\n", a)

	return cmd.Run()
}

func test(cwd string, env map[string]string, args ...string) error {
	args = append([]string{"test", "-count", "1"}, args...)
	cmd := exec.Command("go", args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if !enableCGO {
		cmd.Env = append(cmd.Env, CGOEnabled)
	}
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	printEnv(cmd.Env)

	verbosePrintf("chdir %q\n", cwd)
	verbosePrintf("go %q\n", args)

	return cmd.Run()
}

// ============================================================
// Windows 资源植入 (version info / icon)
// ============================================================

func importProgramVersion(cwd string, target BuildTarget, env map[string]string) error {
	if !supportProgramVersion(env) {
		removeProgramVersionResource(cwd, target)
		verbosePrintf("force remove resource.syso for %s\n", target.Name)
		return nil
	}

	verbosePrintf("running go generate for %s\n", target.Name)
	cmd := exec.Command("go", "generate")
	cmd.Env = os.Environ()
	if !enableCGO {
		cmd.Env = append(cmd.Env, CGOEnabled)
	}
	cmd.Dir = filepath.Join(cwd, target.Main)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func supportProgramVersion(env map[string]string) bool {
	return env[EnvGOOS] == "windows" &&
		env[EnvGOOS] == runtime.GOOS &&
		env[EnvGOARCH] == runtime.GOARCH
}

func removeProgramVersionResource(cwd string, target BuildTarget) {
	syso := filepath.Join(cwd, target.Main, "resource.syso")
	_ = os.Remove(syso)
}

// ============================================================
// 输出生成
// ============================================================

func createConstants(version string) Constants {
	c := Constants{}
	if version != "" {
		c["main.version"] = version
	}
	return c
}

func checkPreserveSymbols(buildTags []string) bool {
	for _, tag := range buildTags {
		if tag == "debug" || tag == "profile" {
			return true
		}
	}
	return false
}

// generateOutputFilename 生成输出路径: build/{程序名}/{OS}/{ARCH}/{文件名}
func generateOutputFilename(target BuildTarget, outputFilename string, env map[string]string, root string) string {
	filename := outputFilename
	if filename == "" {
		filename = target.Name
		if env[EnvGOOS] == "windows" {
			filename += ".exe"
		}
	}

	if !filepath.IsAbs(filename) {
		filename = filepath.Join(root, "build", target.Name, env[EnvGOOS], env[EnvGOARCH], filename)
	}

	return filename
}

func generateLdFlags(constants Constants, preserveSymbols bool) string {
	ldflags := constants.LDFlags()
	if !preserveSymbols {
		ldflags = "-s -w " + ldflags
	}
	return ldflags
}

func generateBuildArgs(buildTags []string, ldflags, output, mainPackage string) []string {
	return []string{
		"-tags", strings.Join(buildTags, " "),
		"-ldflags", ldflags,
		"-o", output, mainPackage,
	}
}

func getMainPackage(target BuildTarget) string {
	return filepath.FromSlash(target.Main)
}

// ============================================================
// 单个目标编译流程
// ============================================================

func buildTarget(target BuildTarget, buildTags []string, outputFilename string, env map[string]string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Getwd(): %v", err)
	}

	output := generateOutputFilename(target, outputFilename, env, root)
	verbosePrintf("[%s] output file: %s\n", target.Name, output)

	// 确保输出目录存在
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("MkdirAll: %v", err)
	}

	version := getVersion()
	constants := createConstants(version)
	preserveSymbols := checkPreserveSymbols(buildTags)
	ldflags := generateLdFlags(constants, preserveSymbols)
	verbosePrintf("[%s] ldflags: %s\n", target.Name, ldflags)

	mainPackage := getMainPackage(target)
	buildArgs := generateBuildArgs(buildTags, ldflags, output, mainPackage)

	// Windows 资源植入
	if err := importProgramVersion(root, target, env); err != nil {
		verbosePrintf("[%s] importProgramVersion warning: %v\n", target.Name, err)
	}

	fmt.Printf(">>> Building %s for %s/%s ...\n", target.Name, env[EnvGOOS], env[EnvGOARCH])
	if err := build(root, env, buildArgs...); err != nil {
		return fmt.Errorf("[%s] build failed: %v", target.Name, err)
	}

	fmt.Printf(">>> Built %s => %s\n", target.Name, output)

	// 清理资源文件
	removeProgramVersionResource(root, target)

	return nil
}

// ============================================================
// 主流程
// ============================================================

func panicRecover() {
	if e := recover(); e != nil {
		die("Panic >>> %v\n", e)
	}
}

func checkGoVersion() {
	if !goVersion.AtLeast(MinGoVersion) {
		die("Detected version %s is too old, requires at least %s\n", goVersion, MinGoVersion)
	}
}

// resolveTargets 根据 --target 参数筛选需要编译的目标.
func resolveTargets(targetName string) ([]BuildTarget, error) {
	if targetName == "" {
		return allTargets, nil
	}

	for _, t := range allTargets {
		if t.Name == targetName {
			return []BuildTarget{t}, nil
		}
	}

	names := make([]string, len(allTargets))
	for i, t := range allTargets {
		names[i] = t.Name
	}
	return nil, fmt.Errorf("unknown target %q, available: %s", targetName, strings.Join(names, ", "))
}

func main() {
	defer panicRecover()

	checkGoVersion()

	env := setupEnvironment()
	buildTags, outputFilename, targetName, err := parseArguments(os.Args[1:], env)
	if err != nil {
		die("%v\n", err)
	}

	// 如果编译多个目标, 不允许使用 -o 指定输出文件名 (会产生冲突)
	if targetName == "" && outputFilename != "" {
		die("--output can only be used together with --target when building a single program\n")
	}

	targets, err := resolveTargets(targetName)
	if err != nil {
		die("%v\n", err)
	}

	verbosePrintf("detected Go version %v\n", goVersion)
	verbosePrintf("build tags: %v\n", buildTags)
	verbosePrintf("targets: ")
	for _, t := range targets {
		verbosePrintf("%s ", t.Name)
	}
	verbosePrintf("\n")

	for _, target := range targets {
		if err := buildTarget(target, buildTags, outputFilename, env); err != nil {
			die("build failed: %v\n", err)
		}
	}

	// 运行测试
	if runTests && len(TestPackages) > 0 {
		cwd, _ := os.Getwd()
		fmt.Println(">>> Running tests ...")
		if err := test(cwd, env, TestPackages...); err != nil {
			die("running tests failed: %v\n", err)
		}
	}

	fmt.Println(">>> All builds completed successfully.")
}
