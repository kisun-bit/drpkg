package recovery

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

type DistroInfo struct {
	ID      string // centos, rhel, ubuntu, debian...
	Version string // 7.9.2009
	Major   int
	Pretty  string // 原始字符串
	Source  string // 来源文件
	Family  string // 来源：RHEL、ALT、SUSE、DEBIAN
}

func GetFamilyByDistroId(distroId string) string {
	switch distroId {
	case "fedora",
		"rhel",
		"centos",
		"circle",
		"scientificlinux",
		"redhat-based",
		"oraclelinux",
		"rocky",
		"kylin",
		"neokylin",
		"anolis",
		"openeuler":
		return LinuxFamilyRHEL
	case "altlinux":
		return LinuxFamilyALT
	case "sles", "suse-based", "opensuse":
		return LinuxFamilySUSE
	case "debian", "ubuntu", "linuxmint", "kalilinux":
		return LinuxFamilyDebian
	}
	return ""
}

func DetectDistro(root string) (*DistroInfo, error) {
	try := []func(string) (*DistroInfo, error){
		parseOSReleaseEtc,
		parseOSReleaseUsr,
		parseLSBRelease,
		parseRedhatRelease,
		parseDebianVersion,
		parseSuseRelease,
		parseIssueFallback,
	}

	for _, fn := range try {
		if info, err := fn(root); err == nil && info != nil {
			info.Family = GetFamilyByDistroId(info.ID)
			return info, nil
		}
	}

	return nil, errors.New("unknown distro")
}

//
// -------------------- os-release --------------------
//

func parseOSReleaseEtc(root string) (*DistroInfo, error) {
	return parseOSRelease(filepath.Join(root, "etc/os-release"))
}

func parseOSReleaseUsr(root string) (*DistroInfo, error) {
	return parseOSRelease(filepath.Join(root, "usr/lib/os-release"))
}

func parseOSRelease(path string) (*DistroInfo, error) {
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	m, err := parseKeyValueFile(path)
	if err != nil {
		return nil, err
	}

	info := &DistroInfo{
		ID:      strings.ToLower(m["ID"]),
		Version: trimQuote(m["VERSION_ID"]),
		Pretty:  trimQuote(m["PRETTY_NAME"]),
		Source:  path,
	}

	if info.Pretty == "" {
		info.Pretty = trimQuote(m["NAME"])
	}

	info.Major = extractMajor(info.Version)
	return info, nil
}

//
// -------------------- lsb-release --------------------
//

func parseLSBRelease(root string) (*DistroInfo, error) {
	path := filepath.Join(root, "etc/lsb-release")
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	m, err := parseKeyValueFile(path)
	if err != nil {
		return nil, err
	}

	info := &DistroInfo{
		ID:      strings.ToLower(m["DISTRIB_ID"]),
		Version: m["DISTRIB_RELEASE"],
		Pretty:  m["DISTRIB_DESCRIPTION"],
		Source:  path,
	}

	if info.ID == "" {
		return nil, errors.New("invalid lsb-release")
	}

	info.Major = extractMajor(info.Version)
	return info, nil
}

//
// -------------------- redhat-release（关键） --------------------
//

func parseRedhatRelease(root string) (*DistroInfo, error) {
	path := filepath.Join(root, "etc/redhat-release")
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	s := strings.TrimSpace(string(data))
	lower := strings.ToLower(s)

	info := &DistroInfo{
		Pretty: s,
		Source: path,
	}

	switch {
	case strings.Contains(lower, "centos"):
		info.ID = "centos"
	case strings.Contains(lower, "red hat"):
		info.ID = "rhel"
	case strings.Contains(lower, "oracle"):
		info.ID = "oracle"
	case strings.Contains(lower, "rocky"):
		info.ID = "rocky"
	case strings.Contains(lower, "alma"):
		info.ID = "almalinux"
	default:
		info.ID = "unknown"
	}

	re := regexp.MustCompile(`release\s+([\d\.]+)`)
	m := re.FindStringSubmatch(lower)
	if len(m) > 1 {
		info.Version = m[1]
		info.Major = extractMajor(info.Version)
	}

	return info, nil
}

//
// -------------------- debian --------------------
//

func parseDebianVersion(root string) (*DistroInfo, error) {
	path := filepath.Join(root, "etc/debian_version")
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	v := strings.TrimSpace(string(data))

	return &DistroInfo{
		ID:      "debian",
		Version: v,
		Major:   extractMajor(v),
		Pretty:  "Debian " + v,
		Source:  path,
	}, nil
}

//
// -------------------- suse --------------------
//

func parseSuseRelease(root string) (*DistroInfo, error) {
	path := filepath.Join(root, "etc/SuSE-release")
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	info := &DistroInfo{
		ID:     "suse-based",
		Pretty: strings.TrimSpace(s),
		Source: path,
	}

	if strings.Contains(strings.ToLower(string(data)), "suse linux enterprise server") {
		info.ID = "sles"
	} else if strings.Contains(strings.ToLower(string(data)), "opensuse") {
		info.ID = "opensuse"
	}

	re := regexp.MustCompile(`VERSION\s*=\s*(\d+)`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		info.Version = m[1]
		info.Major = extractMajor(info.Version)
	}

	return info, nil
}

//
// -------------------- fallback: /etc/issue --------------------
//

func parseIssueFallback(root string) (*DistroInfo, error) {
	path := filepath.Join(root, "etc/issue")
	if !extend.IsExisted(path) {
		return nil, errors.New("not exist")
	}

	data, _ := os.ReadFile(path)
	line := strings.Split(string(data), "\n")[0]
	lower := strings.ToLower(line)

	info := &DistroInfo{
		Pretty: strings.TrimSpace(line),
		Source: path,
	}

	switch {
	case strings.Contains(lower, "centos"):
		info.ID = "centos"
	case strings.Contains(lower, "ubuntu"):
		info.ID = "ubuntu"
	case strings.Contains(lower, "debian"):
		info.ID = "debian"
	case strings.Contains(lower, "red hat"):
		info.ID = "rhel"
	default:
		info.ID = "unknown"
	}

	re := regexp.MustCompile(`(\d+(\.\d+)*)`)
	if m := re.FindStringSubmatch(lower); len(m) > 0 {
		info.Version = m[1]
		info.Major = extractMajor(info.Version)
	}

	return info, nil
}

//
// -------------------- utils --------------------
//

func parseKeyValueFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	m := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		m[k] = trimQuote(v)
	}

	return m, nil
}

func trimQuote(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	s = strings.Trim(s, `'`)
	return s
}

func extractMajor(v string) int {
	if v == "" {
		return 0
	}
	// 麒麟v10
	v = strings.TrimLeft(strings.ToLower(v), "v")
	parts := strings.Split(v, ".")
	n, _ := strconv.Atoi(parts[0])
	return n
}

func suseVersion(distro DistroInfo) string {
	pretty := strings.ToLower(strings.TrimSpace(distro.Pretty))
	prettyItems := strings.Fields(pretty)
	if sPStr := prettyItems[len(prettyItems)-1]; strings.HasPrefix(sPStr, "sp") {
		return fmt.Sprintf("%d%s", distro.Major, sPStr)
	}
	return fmt.Sprintf("%d", distro.Major)
}
