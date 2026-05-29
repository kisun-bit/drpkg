package x2xlib

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

var (
	extraKeyCompatibleKernels = "compatible_kernels"
	extraKeyMsSignType        = "sign_type"
	extraKeySignedByMs        = "signed_by_ms"
	extraKeyMinNT             = "min_nt"
	extraKeyMaxNT             = "max_nt"
)

// vdl 虚拟化驱动库
// 通常而言，一个驱动库包含多个驱动，用于支撑主机在虚拟化平台的稳定运行
type vdl struct {
	Id           string            `json:"id"`           // 驱动库ID
	Name         string            `json:"name"`         // 驱动库名称
	Version      string            `json:"version"`      // 驱动库版本
	Remark       string            `json:"remark"`       // 备注
	Virtual      define.HPVirtType `json:"virtual"`      // 虚拟化类型
	Vendor       string            `json:"vendor"`       // 虚拟化生产商
	Os           string            `json:"os"`           // 主机系统类型
	Architecture string            `json:"architecture"` // 主机cpu架构
	Distro       string            `json:"distro"`       // 系统发型版
	CTime        string            `json:"ctime"`        // 创建时间
	Extra        map[string]string `json:"extra"`        // 扩展信息
}

func newWindowsVDL(
	name string,
	version string,
	remark string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	signType string,
	signedByMs bool,
	minCompatibleNT string,
	maxCompatibleNT string,
) (
	v *vdl,
	err error,
) {
	v = &vdl{
		Name:         SafeName(name),
		Version:      version,
		Remark:       remark,
		Virtual:      virtual,
		Vendor:       vendor,
		Os:           define.OsWindows,
		Distro:       define.DistroMicrosoft,
		Architecture: architecture,
	}
	if err = v.moreInit(); err != nil {
		return nil, err
	}
	if err = v.check(); err != nil {
		return nil, err
	}
	if err = v.setMinNT(minCompatibleNT); err != nil {
		return nil, err
	}
	if err = v.setMaxNT(maxCompatibleNT); err != nil {
		return nil, err
	}
	if err = v.setMsSignType(signType); err != nil {
		return nil, err
	}
	if signedByMs {
		v.setSignedByMicrosoft()
	}
	return v, nil
}

func newLinuxVDL(
	name string,
	version string,
	remark string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	distro string,
	compatibleKernels []string,
) (
	v *vdl,
	err error,
) {
	v = &vdl{
		Name:         SafeName(name),
		Version:      version,
		Remark:       remark,
		Virtual:      virtual,
		Vendor:       vendor,
		Os:           define.OsLinux,
		Architecture: architecture,
		Distro:       distro,
	}
	if err = v.moreInit(); err != nil {
		return nil, err
	}
	if err = v.check(); err != nil {
		return nil, err
	}
	if err = v.addCompatibleKernels(compatibleKernels...); err != nil {
		return nil, err
	}
	return v, nil
}

func (vd *vdl) String() string {
	vendor := strings.ToUpper(string(vd.Virtual))
	if vd.Vendor != "" {
		vendor = vd.Vendor
	}
	return fmt.Sprintf("%s-%s.%s.%s(%s)(%s)",
		vd.Name,
		vd.Version,
		vd.Distro,
		vd.Architecture,
		vendor,
		vd.Id)
}

func (vd *vdl) json() string {
	jsonb, _ := json.Marshal(vd)
	return string(jsonb)
}

func (vd *vdl) fileRepoDir() string {
	sep := string(os.PathSeparator)
	items := []string{
		DriverStoreVirtualDirName,
		vd.Id,
	}
	return strings.Join(items, sep)
}

func (vd *vdl) moreInit() error {
	if vd.Id == "" {
		vd.Id = strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	vd.Name = SafeName(vd.Name)
	if vd.CTime == "" {
		now := time.Now()
		vd.CTime = now.Format("2006-01-02 15:04:05.000")
	}
	if vd.Extra == nil {
		vd.Extra = make(map[string]string)
	}
	return nil
}

func (vd *vdl) check() error {
	if vd.Id == "" {
		return errors.New("unique id is required")
	}
	if vd.Name == "" {
		return errors.New("name is required")
	}
	if err := checkVirtualType(vd.Virtual); err != nil {
		return err
	}
	if err := checkOsType(vd.Os); err != nil {
		return err
	}
	if err := checkArchitecture(vd.Architecture); err != nil {
		return err
	}
	if err := checkDistro(vd.Distro); err != nil {
		return err
	}
	return nil
}

func (vd *vdl) compatibleKernels() ([]string, error) {
	cks := make([]string, 0)
	val, ok := vd.Extra[extraKeyCompatibleKernels]
	if !ok || val == "" {
		return cks, nil
	}
	if err := json.Unmarshal([]byte(val), &cks); err != nil {
		return cks, err
	}
	return cks, nil
}

func (vd *vdl) msSignType() string {
	val, ok := vd.Extra[extraKeyMsSignType]
	if !ok || val == "" {
		return define.MsSignNone
	}
	return val
}

func (vd *vdl) minNT() extend.Version {
	val, ok := vd.Extra[extraKeyMinNT]
	if !ok || val == "" {
		return extend.Version{}
	}
	return extend.ParseVersion(val)
}

func (vd *vdl) maxNT() extend.Version {
	val, ok := vd.Extra[extraKeyMaxNT]
	if !ok || val == "" {
		return extend.Version{}
	}
	return extend.ParseVersion(val)
}

func (vd *vdl) isLinuxCompatible(virtual define.HPVirtType, architecture string, distro string, kernel string, mustVendor string) bool {
	kernelMatched, _ := vd.isKernelCompatible(kernel)
	return vd.Virtual == virtual &&
		vd.Architecture == architecture &&
		vd.Distro == distro &&
		kernelMatched &&
		(mustVendor == "" || vd.Vendor == mustVendor)
}

func (vd *vdl) isWindowsCompatible(virtual define.HPVirtType, architecture string, ntVersion string, mustSignType string, mustSignedByMs bool) bool {
	if vd.Virtual == virtual &&
		vd.Architecture == architecture &&
		vd.isNTVersionCompatible(ntVersion) &&
		(!mustSignedByMs || vd.isSignedByMicrosoft()) &&
		(mustSignType == define.MsSignNone || mustSignType == vd.msSignType()) {
		return true
	}
	return false
}

func (vd *vdl) isKernelCompatible(kernel string) (bool, error) {
	cks, err := vd.compatibleKernels()
	if err != nil {
		return false, err
	}
	return funk.InStrings(cks, kernel), nil
}

func (vd *vdl) isNTVersionCompatible(version string) bool {
	minv := vd.minNT()
	maxv := vd.maxNT()
	curv := extend.ParseVersion(version)

	return curv.GreaterOrEqual(minv) && curv.LessOrEqual(maxv)
}

func (vd *vdl) isSignedBySha1() bool {
	return vd.msSignType() == define.MsSignSha1
}

func (vd *vdl) isSignedByDual() bool {
	return vd.msSignType() == define.MsSignDual
}

func (vd *vdl) isSignedBySha256() bool {
	return vd.msSignType() == define.MsSignSha256
}

func (vd *vdl) isSignedByMicrosoft() bool {
	val, ok := vd.Extra[extraKeySignedByMs]
	if !ok || val == "" {
		return false
	}
	if val == "true" {
		return true
	}
	return false
}

func (vd *vdl) setSignedByMicrosoft() {
	vd.Extra[extraKeySignedByMs] = "true"
}

func (vd *vdl) setMsSignType(signType string) error {
	if err := checkMsSignAlgo(signType); err != nil {
		return err
	}
	vd.Extra[extraKeyMsSignType] = signType
	return nil
}

func (vd *vdl) setMinNT(version string) error {
	if err := checkNtVersion(version); err != nil {
		return err
	}
	vd.Extra[extraKeyMinNT] = version
	return nil
}

func (vd *vdl) setMaxNT(version string) error {
	if err := checkNtVersion(version); err != nil {
		return err
	}
	vd.Extra[extraKeyMaxNT] = version
	return nil
}

func (vd *vdl) addCompatibleKernels(
	kernels ...string,
) error {

	if len(kernels) == 0 {
		return errors.New("compatible kernels is empty")
	}

	return vd.updateCompatibleKernels(
		func(cks []string) ([]string, bool) {
			changed := false

			for _, nk := range kernels {
				if funk.InStrings(cks, nk) {
					continue
				}

				cks = append(cks, nk)
				changed = true
			}

			return cks, changed
		},
	)
}

func (vd *vdl) removeCompatibleKernels(
	kernels ...string,
) error {

	if len(kernels) == 0 {
		return nil
	}

	return vd.updateCompatibleKernels(
		func(cks []string) ([]string, bool) {
			changed := false
			newCks := make([]string, 0, len(cks))

			for _, ck := range cks {
				if funk.InStrings(kernels, ck) {
					changed = true
					continue
				}

				newCks = append(newCks, ck)
			}

			return newCks, changed
		},
	)
}

func (vd *vdl) updateCompatibleKernels(
	fn func([]string) ([]string, bool),
) error {

	if vd.Os != define.OsLinux {
		return errors.New("compatible kernels only supported on linux")
	}

	cks, err := vd.compatibleKernels()
	if err != nil {
		return err
	}

	newCks, changed := fn(cks)
	if !changed {
		return nil
	}

	data, err := json.Marshal(newCks)
	if err != nil {
		return err
	}

	if vd.Extra == nil {
		vd.Extra = make(map[string]string)
	}

	vd.Extra[extraKeyCompatibleKernels] = string(data)

	return nil
}
