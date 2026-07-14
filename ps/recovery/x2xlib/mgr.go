package x2xlib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// X2XLib 表示驱动库对象。
type X2XLib struct {
	library        string
	driverStoreDir string
	driverStoreDB  string
	readonly       bool
	db             *gorm.DB
}

type DriverResource struct {
	Id           string
	FriendlyName string
	Modules      []string
	Dir          string
}

// NewX2XLib 创建驱动库实例。
func NewX2XLib(libraryDir string, readonly bool) (*X2XLib, error) {
	drvStoreDir := filepath.Join(libraryDir, DriverStoreDirName)
	if err := ensureDir(drvStoreDir); err != nil {
		return nil, err
	}

	drvStoreDB := filepath.Join(libraryDir, DriverStoreDBName)

	l := &X2XLib{
		library:        libraryDir,
		driverStoreDir: drvStoreDir,
		driverStoreDB:  drvStoreDB,
		readonly:       readonly,
		db:             new(gorm.DB),
	}

	db, err := InitDB(drvStoreDB, readonly)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		l.db = db
	}

	return l, nil
}

func (x *X2XLib) String() string {
	return fmt.Sprintf(
		"x2xlib{library=%s}",
		x.library,
	)
}

func (x *X2XLib) Close() error {
	if x.db != nil {
		dbi, err := x.db.DB()
		if err != nil {
			return err
		}
		return dbi.Close()
	}
	return nil
}

// Destroy 销毁驱动库。
func (x *X2XLib) Destroy() error {
	if err := os.RemoveAll(x.driverStoreDir); err != nil {
		return err
	}
	if err := os.RemoveAll(x.driverStoreDB); err != nil {
		return err
	}
	return nil
}

// AddWindowsVirtualDriver 添加 Windows 的虚拟化驱动库
func (x *X2XLib) AddWindowsVirtualDriver(
	name string,
	version string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	signatures []Signature,
	modules []string,
	windowsVersions []define.WindowsVersion,
) (
	driverID string,
	driverDir string,
	err error,
) {

	if x.readonly {
		return "", "", errors.New("readonly is enabled")
	}
	for _, v := range windowsVersions {
		if err = checkWindowsVersion(v); err != nil {
			return "", "", err
		}
	}
	drvType, err := getDriverTypeByVirtType(virtual)
	if err != nil {
		return "", "", err
	}
	driver, err := buildWindowsDriver(
		name,
		version,
		vendor,
		architecture,
		sourceDir,
		remark,
		signatures,
		modules,
		drvType,
	)
	if err != nil {
		return "", "", err
	}

	return x.createDriverTx(
		driver,
		sourceDir,
		func(tx *gorm.DB, driverID string) error {
			return createNTCompat(
				tx,
				driverID,
				windowsVersions,
			)
		},
	)
}

// AddWindowsNormalDriver 添加 Windows 的普通硬件驱动
func (x *X2XLib) AddWindowsNormalDriver(
	name string,
	version string,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	signatures []Signature,
	module string,
	windowsVersions []define.WindowsVersion,
	hardwareIdsArr []string,
) (
	driverID string,
	driverDir string,
	err error,
) {

	if x.readonly {
		return "", "", errors.New("readonly is enabled")
	}
	for _, v := range windowsVersions {
		if err = checkWindowsVersion(v); err != nil {
			return "", "", err
		}
	}
	driver, err := buildWindowsDriver(
		name,
		version,
		vendor,
		architecture,
		sourceDir,
		remark,
		signatures,
		[]string{module},
		driverTypeNormal,
	)
	if err != nil {
		return "", "", err
	}

	return x.createDriverTx(
		driver,
		sourceDir,
		func(tx *gorm.DB, driverID string) error {

			if err = createNTCompat(
				tx,
				driverID,
				windowsVersions,
			); err != nil {

				return err
			}

			return createHardwareCompat(
				tx,
				driverID,
				hardwareIdsArr,
			)
		},
	)
}

// AddLinuxVirtualDriver 添加 Linux 的虚拟化驱动库
func (x *X2XLib) AddLinuxVirtualDriver(
	name string,
	version string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	family string,
	signature Signature,
	modules []string,
	compatibleKernels []string,
) (
	driverID string,
	driverDir string,
	err error,
) {

	if x.readonly {
		return "", "", errors.New("readonly is enabled")
	}
	if err = checkFamily(family); err != nil {
		return "", "", err
	}
	if err = checkKernels(compatibleKernels); err != nil {
		return "", "", err
	}
	drvType, err := getDriverTypeByVirtType(virtual)
	if err != nil {
		return "", "", err
	}
	driver, err := buildLinuxDriver(
		name,
		version,
		vendor,
		architecture,
		sourceDir,
		remark,
		family,
		signature,
		modules,
		drvType,
	)
	if err != nil {
		return "", "", err
	}

	return x.createDriverTx(
		driver,
		sourceDir,
		func(tx *gorm.DB, driverID string) error {
			return createKernelCompat(
				tx,
				driverID,
				compatibleKernels,
			)
		},
	)
}

// AddLinuxNormalDriver 添加 Linux 的普通硬件驱动
func (x *X2XLib) AddLinuxNormalDriver(
	name string,
	version string,
	vendor string,
	architecture string,
	sourceDir string,
	remark string,
	family string,
	signature Signature,
	modules []string,
	compatibleKernels []string,
	compatibleAlias []string,
) (
	driverID string,
	driverDir string,
	err error,
) {

	if x.readonly {
		return "", "", errors.New("readonly is enabled")
	}
	if err = checkFamily(family); err != nil {
		return "", "", err
	}
	if err = checkKernels(compatibleKernels); err != nil {
		return "", "", err
	}
	driver, err := buildLinuxDriver(
		name,
		version,
		vendor,
		architecture,
		sourceDir,
		remark,
		family,
		signature,
		modules,
		driverTypeNormal,
	)
	if err != nil {
		return "", "", err
	}

	return x.createDriverTx(
		driver,
		sourceDir,
		func(tx *gorm.DB, driverID string) error {

			if err := createKernelCompat(
				tx,
				driverID,
				compatibleKernels,
			); err != nil {

				return err
			}

			return createHardwareCompat(
				tx,
				driverID,
				compatibleAlias,
			)
		},
	)
}

// SelectWindowsBestVirtualDriver 获取 Windows 兼容的虚拟化驱动库。
func (x *X2XLib) SelectWindowsBestVirtualDriver(
	virtual define.HPVirtType,
	architecture string,
	windowsVersion define.WindowsVersion,
	ignoreCheckSignature bool,
) (
	dr *DriverResource,
	err error,
) {
	drvType, err := getDriverTypeByVirtType(virtual)
	if err != nil {
		return nil, err
	}
	if err = checkArchitecture(architecture); err != nil {
		return nil, err
	}
	if err = checkWindowsVersion(windowsVersion); err != nil {
		return nil, err
	}

	var drivers []Driver

	err = x.db.
		Table("driver").
		Joins(
			"INNER JOIN nt_compat ON nt_compat.driver_id = driver.id",
		).
		Where("driver.os = ?", define.OsWindows).
		Where("driver.arch = ?", architecture).
		Where("driver.type = ?", drvType).
		Where("? = nt_compat.windows_version", windowsVersion).
		Order("driver.version_weight DESC").
		Find(&drivers).
		Error

	if err != nil {
		return nil, err
	}

	driver, err := x.pickWindowsDriver(
		drivers,
		windowsVersion,
		ignoreCheckSignature,
	)
	if err != nil {
		return nil, err
	}

	return x.driverResult(driver)
}

// SelectWindowsBestNormalDriver 获取 Windows 兼容的普通硬件驱动
func (x *X2XLib) SelectWindowsBestNormalDriver(
	architecture string,
	windowsVersion define.WindowsVersion,
	unipci string,
	ignoreCheckSignature bool,
) (
	dr *DriverResource,
	err error,
) {
	if err = checkArchitecture(architecture); err != nil {
		return nil, err
	}

	if err = checkWindowsVersion(windowsVersion); err != nil {
		return nil, err
	}

	compatIds, err := compatIdsFromUniPci(unipci)
	if err != nil {
		return nil, err
	}

	var drivers []Driver

	err = x.db.
		Table("driver").
		Select("driver.*").
		Joins(`
			INNER JOIN hardware_compat
				ON hardware_compat.driver_id = driver.id
		`).
		Joins(`
			INNER JOIN nt_compat
				ON nt_compat.driver_id = driver.id
		`).
		Where("driver.os = ?", define.OsWindows).
		Where("driver.arch = ?", architecture).
		Where("driver.type = ?", driverTypeNormal).
		Where(
			"hardware_compat.compat_id IN ?",
			compatIds,
		).
		Where(
			"? = nt_compat.windows_version",
			windowsVersion,
		).
		Group("driver.id").
		Order("MAX(hardware_compat.compat_weight) DESC").
		Order("driver.version_weight DESC").
		Order("driver.sign_weight DESC").
		Find(&drivers).
		Error

	if err != nil {
		return nil, err
	}

	driver, err := x.pickWindowsDriver(
		drivers,
		windowsVersion,
		ignoreCheckSignature,
	)
	if err != nil {
		return nil, err
	}

	return x.driverResult(driver)
}

type DriverWithCompat struct {
	Driver
	CompatID string `gorm:"column:compat_id"`
}

// SelectLinuxBestNormalDriver 获取 Linux 兼容的普通硬件驱动
func (x *X2XLib) SelectLinuxBestNormalDriver(
	architecture string,
	family string,
	kernel string,
	unipci string,
) (
	*DriverResource,
	error,
) {

	if err := checkArchitecture(architecture); err != nil {
		return nil, err
	}

	if err := checkFamily(family); err != nil {
		return nil, err
	}

	if kernel == "" {
		return nil, errors.New("kernel is required")
	}

	up, err := universal.UniPciFromString(unipci)
	if err != nil {
		return nil, err
	}

	upAlias := up.Modalias()

	var rows []DriverWithCompat

	err = x.db.
		Table("driver").
		Select(`
			driver.*,
			hardware_compat.compat_id
		`).
		Joins(`
			INNER JOIN hardware_compat
				ON hardware_compat.driver_id = driver.id
		`).
		Joins(`
			INNER JOIN kernel_compat
				ON kernel_compat.driver_id = driver.id
		`).
		Where("driver.os = ?", define.OsLinux).
		Where("driver.arch = ?", architecture).
		Where("driver.family = ?", family).
		Where("driver.type = ?", driverTypeNormal).
		Where("kernel_compat.kernel = ?", kernel).
		Order("driver.version_weight DESC").
		Order("driver.sign_weight DESC").
		Find(&rows).
		Error

	if err != nil {
		return nil, err
	}

	logger.Debug(len(rows))

	for _, row := range rows {
		ok, e := filepath.Match(row.CompatID, strings.ToLower(upAlias))
		if e != nil {
			return nil, e
		}
		if ok {
			driver := row.Driver
			return x.driverResult(&driver)
		}
	}

	return nil, errors.Wrap(os.ErrNotExist, "driver not found")
}

// SelectLinuxBestVirtualDriver 获取 Linux 兼容的虚拟化驱动库。
func (x *X2XLib) SelectLinuxBestVirtualDriver(
	virtual define.HPVirtType,
	architecture string,
	family string,
	kernel string,
	vendor string,
) (
	dr *DriverResource,
	err error,
) {
	drvType, err := getDriverTypeByVirtType(virtual)
	if err != nil {
		return nil, err
	}
	if err = checkArchitecture(architecture); err != nil {
		return nil, err
	}
	if err = checkFamily(family); err != nil {
		return nil, err
	}

	var drivers []Driver

	err = x.db.
		Table("driver").
		Joins(
			"INNER JOIN kernel_compat ON kernel_compat.driver_id = driver.id",
		).
		Where("driver.os = ?", define.OsLinux).
		Where("driver.arch = ?", architecture).
		Where("driver.type = ?", drvType).
		Where("? = kernel_compat.kernel", kernel).
		Order("driver.version_weight DESC").
		Find(&drivers).
		Error

	if err != nil {
		return nil, err
	}

	driver, err := x.findDriverByVendor(
		drivers,
		vendor,
	)
	if err != nil {
		return nil, err
	}

	return x.driverResult(driver)
}

// DeleteDriver 删除指定的驱动
func (x *X2XLib) DeleteDriver(
	driverID string,
) error {

	var driver Driver

	err := x.db.
		Where("id = ?", driverID).
		First(&driver).
		Error

	if err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}

		return err
	}

	driverDir := driver.Directory(x.driverStoreDir)

	err = x.db.Transaction(func(tx *gorm.DB) error {

		if err = tx.Delete(
			&Driver{},
			"id = ?",
			driverID,
		).Error; err != nil {
			return err
		}

		if err = tx.Delete(
			&KernelCompat{},
			"driver_id = ?",
			driverID,
		).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err = tx.Delete(
			&NTCompat{},
			"driver_id = ?",
			driverID,
		).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err = tx.Delete(
			&HardwareCompat{},
			"driver_id = ?",
			driverID,
		).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if err = os.RemoveAll(driverDir); err != nil {
		return errors.Errorf(
			"driver deleted but directory cleanup failed: %v",
			err,
		)
	}

	return nil
}

func (x *X2XLib) createDriverTx(
	driver *Driver,
	sourceDir string,
	compatCreator func(tx *gorm.DB, driverID string) error,
) (
	driverID string,
	driverDir string,
	err error,
) {

	driverDir = filepath.Join(
		x.driverStoreDir,
		driver.OS,
		driver.Family,
		driver.Arch,
		driver.ID,
	)

	tx := x.db.Begin()

	if tx.Error != nil {
		return "", "", tx.Error
	}

	defer func() {

		if err == nil {
			return
		}

		tx.Rollback()

		if driverDir != "" {
			_ = os.RemoveAll(driverDir)
		}
	}()

	//-----------------------------------
	// Driver
	//-----------------------------------

	if err = tx.Create(driver).Error; err != nil {
		return "", "", err
	}

	//-----------------------------------
	// Compat
	//-----------------------------------

	if compatCreator != nil {

		if err = compatCreator(
			tx,
			driver.ID,
		); err != nil {

			return "", "", err
		}
	}

	//-----------------------------------
	// Driver目录
	//-----------------------------------

	if err = ensureDir(driverDir); err != nil {
		return "", "", err
	}

	if err = extend.CopyDir(
		sourceDir,
		driverDir,
	); err != nil {

		return "", "", err
	}

	//-----------------------------------
	// Commit
	//-----------------------------------

	if err = tx.Commit().Error; err != nil {
		return "", "", err
	}

	return driver.ID, driverDir, nil
}

func (x *X2XLib) pickWindowsDriver(
	drivers []Driver,
	windowsVersion define.WindowsVersion,
	ignoreCheckSignature bool,
) (*Driver, error) {

	if len(drivers) == 0 {
		return nil, errors.Wrap(os.ErrNotExist, "driver not found")
	}

	ntVer, _ := define.OsNTVersion[windowsVersion]
	if ntVer >= define.NT62 || ignoreCheckSignature {
		return &drivers[0], nil
	}

	for _, d := range drivers {
		ds, err := LoadDriverSignature(d.Sign)
		if err != nil {
			return nil, err
		}

		if ds.IsSha1() {
			return &d, nil
		}
	}

	return nil, errors.Wrap(os.ErrNotExist, "driver not found")
}

func (x *X2XLib) driverResult(
	d *Driver,
) (
	*DriverResource,
	error,
) {
	if d == nil {
		return nil, errors.Wrap(os.ErrNotExist, "driver not found")
	}

	dr := new(DriverResource)
	dr.Id = d.ID
	dr.FriendlyName = d.Name
	dr.Modules = d.ModuleList()
	dr.Dir = d.Directory(x.driverStoreDir)

	return dr, nil
}

func (x *X2XLib) findDriverByVendor(
	drivers []Driver,
	vendor string,
) (*Driver, error) {

	if len(drivers) == 0 {
		return nil, errors.Wrap(os.ErrNotExist, "driver not found")
	}

	if vendor == "" {
		return &drivers[0], nil
	}

	for _, d := range drivers {
		if d.Vendor == vendor {
			return &d, nil
		}
	}

	return nil, errors.Wrap(os.ErrNotExist, "driver not found")
}
