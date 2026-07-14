package x2xlib

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

func checkDriverName(name string) error {
	if name == "" || strings.TrimSpace(name) == "" {
		return errors.New("driver name is empty")
	}
	return nil
}

func checkDriverDirectory(driverDir string) error {
	if driverDir == "" {
		return errors.New("driver directory is empty")
	}
	if !extend.IsExisted(driverDir) {
		return errors.Wrapf(os.ErrNotExist, driverDir)
	}
	if !extend.IsDir(driverDir) {
		return errors.Errorf("%s is not a directory", driverDir)
	}
	if extend.IsEmptyDir(driverDir) {
		return errors.Errorf("%s is empty", driverDir)
	}
	return nil
}

func checkFamily(family string) error {
	if family == "" {
		return errors.New("family is required")
	}
	if !funk.InStrings(SupportedFamilyTypes, family) {
		return errors.Errorf("unsupported family(`%s`)", family)
	}
	return nil
}

func checkArchitecture(architecture string) error {
	if architecture == "" {
		return errors.New("architecture is required")
	}
	if !funk.InStrings(SupportedArchTypes, architecture) {
		return errors.Errorf("unsupported architecture(`%s`)", architecture)
	}
	return nil
}

func checkKernels(kernels []string) error {
	if len(kernels) == 0 {
		return errors.New("kernels is empty")
	}
	for _, kernel := range kernels {
		if kernel == "" || strings.TrimSpace(kernel) == "" {
			return errors.New("kernels contains empty version")
		}
	}
	return nil
}

func checkWindowsVersion(windowsVersion define.WindowsVersion) error {
	if _, ok := define.OsNTVersion[windowsVersion]; !ok {
		return errors.Errorf("unsupported windows version `%s`", windowsVersion)
	}
	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func getDriverTypeByVirtType(virt define.HPVirtType) (uint16, error) {
	switch virt {
	case define.HPVTVmware:
		return driverTypeVirtualVmware, nil
	case define.HPVTHyperV:
		return driverTypeVirtualHyperV, nil
	case define.HPVTXen:
		return driverTypeVirtualXen, nil
	case define.HPVTKvm:
		return driverTypeVirtualKvm, nil
	}
	return 0, fmt.Errorf("unsupported VirtType `%s`", virt)
}

// versionWeight
//
// 将最多4段、每段不超过65535的版本号编码为uint64。
// 编码格式：
//
//	Major.Minor.Build.Revision
//
// 对应：
//
//	[16bit][16bit][16bit][16bit]
//
// 例如：
//
//	1            -> 0001.0000.0000.0000
//	1.0          -> 0001.0000.0000.0000
//	1.0.0        -> 0001.0000.0000.0000
//	1.0.0.1      -> 0001.0000.0000.0001
//	6.1.7601.17514
//
// 返回值可直接进行大小比较。
func versionWeight(in string) (uint64, error) {
	if in == "" {
		return 0, fmt.Errorf("empty version")
	}

	parts := strings.Split(in, ".")
	if len(parts) > 4 {
		return 0, fmt.Errorf("too many version parts: %s", in)
	}

	var nums [4]uint64

	for i, p := range parts {
		n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 16)
		if err != nil {
			return 0, fmt.Errorf("invalid version part %q", p)
		}

		if n > 0xffff {
			return 0, fmt.Errorf("version part too large: %d", n)
		}

		nums[i] = n
	}

	return nums[0]<<48 |
		nums[1]<<32 |
		nums[2]<<16 |
		nums[3], nil
}

func compatIdsFromUniPci(upStr string) ([]string, error) {
	up, err := universal.UniPciFromString(upStr)
	if err != nil {
		return nil, err
	}
	msCompatIds := up.MsCompatibleId()
	validMsCompatIds := make([]string, 0)
	for _, id := range msCompatIds {
		if strings.TrimSpace(id) == "" {
			continue
		}
		validMsCompatIds = append(validMsCompatIds, id)
	}
	if len(validMsCompatIds) == 0 {
		return nil, errors.Errorf("no compatible msCompatIds found in %s", upStr)
	}
	return validMsCompatIds, nil
}

type createDriverOption struct {
	Name         string
	Version      string
	Vendor       string
	Architecture string
	SourceDir    string
	Remark       string

	OS      string
	Family  string
	DrvType uint16

	Signature DriverSignature

	Modules []string
}

func buildDriver(opt createDriverOption) (*Driver, error) {

	if err := checkDriverName(opt.Name); err != nil {
		return nil, err
	}

	if err := checkArchitecture(opt.Architecture); err != nil {
		return nil, err
	}

	if err := checkDriverDirectory(opt.SourceDir); err != nil {
		return nil, err
	}

	verWeight, err := versionWeight(opt.Version)
	if err != nil {
		return nil, err
	}
	if len(opt.Modules) == 0 {
		return nil, errors.Errorf("modules is required")
	}
	modulesJson, _ := json.Marshal(opt.Modules)

	return &Driver{
		ID:            strings.ReplaceAll(uuid.New().String(), "-", ""),
		Name:          opt.Name,
		Modules:       string(modulesJson),
		Version:       opt.Version,
		VersionWeight: verWeight,
		Vendor:        opt.Vendor,
		Sign:          opt.Signature.String(),
		SignWeight:    uint64(opt.Signature.Weight()),
		OS:            opt.OS,
		Arch:          opt.Architecture,
		Family:        opt.Family,
		Type:          opt.DrvType,
		Remark:        opt.Remark,
	}, nil
}

func createNTCompat(
	tx *gorm.DB,
	driverID string,
	windowsVersions []define.WindowsVersion,
) error {

	compatList := make([]NTCompat, 0, len(windowsVersions))

	for _, v := range windowsVersions {
		compatList = append(compatList, NTCompat{
			DriverID:       driverID,
			WindowsVersion: v,
		})
	}

	if len(compatList) == 0 {
		return nil
	}

	return tx.Create(&compatList).Error
}

func createKernelCompat(
	tx *gorm.DB,
	driverID string,
	kernels []string,
) error {

	for _, kernel := range kernels {

		if err := tx.Create(&KernelCompat{
			DriverID: driverID,
			Kernel:   strings.TrimSpace(kernel),
		}).Error; err != nil {

			return err
		}
	}

	return nil
}

func createHardwareCompat(
	tx *gorm.DB,
	driverID string,
	compatHwId []string,
) error {
	for _, alias := range compatHwId {
		if err := tx.Create(&HardwareCompat{
			DriverID: driverID,
			CompatID: strings.ToLower(alias),
		}).Error; err != nil {

			return err
		}
	}

	return nil
}

func buildWindowsDriver(
	name string,
	version string,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	signatures []Signature,
	modules []string,
	drvType uint16,
) (
	*Driver,
	error,
) {

	sign, err := NewDriverSignature(
		define.OsWindows,
		signatures,
	)
	if err != nil {
		return nil, err
	}

	return buildDriver(createDriverOption{
		Name:         name,
		Version:      version,
		Vendor:       vendor,
		Architecture: architecture,
		SourceDir:    sourceDir,
		Remark:       remark,

		OS:      define.OsWindows,
		Family:  define.WindowsFamily,
		DrvType: drvType,

		Signature: *sign,

		Modules: modules,
	})
}

func buildLinuxDriver(
	name string,
	version string,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	family string,
	signature Signature,
	modules []string,
	drvType uint16,
) (
	*Driver,
	error,
) {

	sign, err := NewDriverSignature(
		define.OsLinux,
		[]Signature{signature},
	)
	if err != nil {
		return nil, err
	}

	return buildDriver(createDriverOption{
		Name:         name,
		Version:      version,
		Vendor:       vendor,
		Architecture: architecture,
		SourceDir:    sourceDir,
		Remark:       remark,

		OS:      define.OsLinux,
		Family:  family,
		DrvType: drvType,

		Signature: *sign,

		Modules: modules,
	})
}
