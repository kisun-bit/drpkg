package x2xlib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Driver 表示驱动库中的一个驱动包。
// 驱动包的种类分为如下：
// * Windows虚拟化驱动库
// * Linux虚拟化驱动库
// * Windows驱动库
// * Linux驱动库
type Driver struct {
	ID            string    `gorm:"column:id;primaryKey;size:64"`
	Name          string    `gorm:"column:name;size:128;not null"`
	Version       string    `gorm:"column:version;size:64"`
	VersionWeight uint64    `gorm:"column:version_weight;"`
	Vendor        string    `gorm:"column:vendor;size:128"`
	Sign          string    `gorm:"column:sign;type:text"`
	SignWeight    uint64    `gorm:"column:sign_weight;"`
	OS            string    `gorm:"column:os;size:32;"`
	Arch          string    `gorm:"column:arch;size:32;"`
	Family        string    `gorm:"column:family;size:64;"`
	Type          uint16    `gorm:"column:type;"`
	Extend        string    `gorm:"column:extend;type:text"`
	Remark        string    `gorm:"column:remark;type:text"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Driver) TableName() string {
	return "driver"
}

func (d *Driver) Pretty() string {
	str := fmt.Sprintf("%s %s %s %s %s", d.Name, d.Version, d.Family, d.Arch, d.ID)
	if d.Vendor != "" {
		str += fmt.Sprintf(" (%s)", d.Vendor)
	}
	return str
}

func (d *Driver) Directory(baseDir string) string {
	id := d.ID
	if id == "" {
		id = uuid.New().String()
	}
	return filepath.Join(baseDir, d.OS, d.Family, d.Arch, id)
}

// KernelCompat Linux内核兼容关系
type KernelCompat struct {
	ID       uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID string `gorm:"column:driver_id;size:64;not null;index"`
	Kernel   string `gorm:"column:kernel;size:64;not null;"`
}

func (KernelCompat) TableName() string {
	return "kernel_compat"
}

// NTCompat Windows NT版本兼容关系
// 用于描述驱动支持的NT版本区间。
type NTCompat struct {
	ID          uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID    string `gorm:"column:driver_id;size:64;not null;index"`
	NTMin       string `gorm:"column:nt_min;size:64;not null;"`
	NTMinWeight uint64 `gorm:"column:nt_min_weight;"`
	NTMax       string `gorm:"column:nt_max;size:64;not null;"`
	NTMaxWeight uint64 `gorm:"column:nt_max_weight;"`
}

func (NTCompat) TableName() string {
	return "nt_compat"
}

// HardwareCompat 硬件兼容关系
type HardwareCompat struct {
	ID           uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID     string `gorm:"column:driver_id;size:64;not null;index"`
	CompatID     string `gorm:"column:compat_id;size:128;not null;"`
	CompatWeight uint64 `gorm:"column:compat_weight;not null;"`
}

func (HardwareCompat) TableName() string {
	return "hardware_compat"
}

func InitDB(dbFile string, readonly bool) (*gorm.DB, error) {
	if readonly && !extend.IsExisted(dbFile) {
		return nil, errors.Wrapf(os.ErrNotExist, dbFile)
	}

	// https://github.com/glebarez/sqlite/issues/52#issuecomment-1214160902
	dsnCfgs := make([]string, 0)
	dsnCfgs = append(dsnCfgs, "cache=shared")
	if readonly {
		dsnCfgs = append(dsnCfgs, "mode=ro")
	}
	dsnCfgs = append(dsnCfgs, "_pragma=journal_mode(WAL)")
	dsnCfgs = append(dsnCfgs, "_pragma=busy_timeout(10000)")

	dsn := fmt.Sprintf("file:%s?%s", dbFile, strings.Join(dsnCfgs, "&"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	if err = db.AutoMigrate(
		&Driver{},
		&KernelCompat{},
		&NTCompat{},
		&HardwareCompat{},
	); err != nil {
		return nil, err
	}

	return db, nil
}
