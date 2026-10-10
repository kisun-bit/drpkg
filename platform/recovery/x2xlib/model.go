package x2xlib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/xutil"
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
	ID             string    `gorm:"column:id;primaryKey;size:64"`
	Name           string    `gorm:"column:name;size:128;not null"`
	Modules        string    `gorm:"column:modules;type:text;not null"`
	Version        string    `gorm:"column:version;size:64"`
	VersionWeight  uint64    `gorm:"column:version_weight;"`
	Vendor         string    `gorm:"column:vendor;size:128"`
	Sign           string    `gorm:"column:sign;type:text"`
	SignWeight     uint64    `gorm:"column:sign_weight;"`
	OS             string    `gorm:"column:os;size:32;"`
	Arch           string    `gorm:"column:arch;size:32;"`
	Family         string    `gorm:"column:family;size:64;"`
	HwPlatformType uint16    `gorm:"column:hw_type;"`
	Extend         string    `gorm:"column:extend;type:text"`
	Remark         string    `gorm:"column:remark;type:text"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (*Driver) TableName() string {
	return "driver"
}

func (d *Driver) Pretty(db *gorm.DB) string {
	// ① 驱动标识符: name-version-arch(compat)
	name := strings.TrimSpace(d.Name)
	version := strings.TrimSpace(d.Version)
	arch := strings.TrimSpace(d.Arch)

	var ident string
	switch {
	case name != "" && version != "":
		ident = fmt.Sprintf("%s-%s", name, version)
	case name != "":
		ident = name
	default:
		ident = "<unknown>"
	}
	if arch != "" {
		ident += "-" + arch
	}

	// 兼容版本嵌入标识符括号内
	if db != nil {
		var compat string
		switch d.OS {
		case "windows":
			var vers []string
			db.Select("windows_version").Where("driver_id = ?", d.ID).
				Model(&NTCompat{}).Pluck("windows_version", &vers)
			if len(vers) > 0 {
				compat = strings.Join(vers, ",")
			}
		case "linux":
			var kernels []string
			db.Select("kernel").Where("driver_id = ?", d.ID).
				Model(&KernelCompat{}).Pluck("kernel", &kernels)
			if len(kernels) > 0 {
				compat = strings.Join(kernels, ",")
			}
		}
		if compat != "" {
			ident += fmt.Sprintf("(%s)", compat)
		}
	}

	// ② Family + Vendor 作为后缀标签
	var suffix []string
	if d.Family != "" {
		suffix = append(suffix, strings.ToLower(d.Family))
	}
	if d.Vendor != "" {
		suffix = append(suffix, fmt.Sprintf("(%s)", d.Vendor))
	}

	parts := []string{ident}
	parts = append(parts, suffix...)
	return strings.Join(parts, " ")
}

func (d *Driver) Directory(baseDir string) string {
	id := d.ID
	if id == "" {
		id = uuid.New().String()
	}
	return filepath.Join(baseDir, d.OS, d.Family, d.Arch, id)
}

func (d *Driver) ModuleList() []string {
	ret := make([]string, 0)
	_ = json.Unmarshal([]byte(d.Modules), &ret)
	return ret
}

// KernelCompat Linux内核兼容关系
type KernelCompat struct {
	ID       uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID string `gorm:"column:driver_id;size:64;not null;index"`
	Kernel   string `gorm:"column:kernel;size:64;not null;"`
}

func (*KernelCompat) TableName() string {
	return "kernel_compat"
}

// NTCompat Windows NT版本兼容关系
// 用于描述驱动支持的NT版本区间。
type NTCompat struct {
	ID             uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID       string `gorm:"column:driver_id;size:64;not null;index"`
	WindowsVersion string `gorm:"column:windows_version;size:64;not null;index"`
}

func (*NTCompat) TableName() string {
	return "nt_compat"
}

// HardwareCompat 硬件兼容关系
type HardwareCompat struct {
	ID       uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	DriverID string `gorm:"column:driver_id;size:64;not null;index"`
	CompatID string `gorm:"column:compat_id;size:128;not null;"`
}

func (*HardwareCompat) TableName() string {
	return "hardware_compat"
}

func InitDB(dbFile string, readonly bool) (*gorm.DB, error) {
	if readonly && !xutil.IsExisted(dbFile) {
		return nil, errors.Wrapf(os.ErrNotExist, dbFile)
	}

	// https://github.com/glebarez/sqlite/issues/52#issuecomment-1214160902
	dsnCfgs := make([]string, 0)
	dsnCfgs = append(dsnCfgs, "cache=shared")
	if readonly {
		dsnCfgs = append(dsnCfgs, "mode=ro")
		dsnCfgs = append(dsnCfgs, "immutable=1")
	} else {
		dsnCfgs = append(dsnCfgs, "_pragma=journal_mode(WAL)")
		dsnCfgs = append(dsnCfgs, "_pragma=busy_timeout(10000)")
	}

	dsn := fmt.Sprintf("file:%s?%s", dbFile, strings.Join(dsnCfgs, "&"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
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

	if !readonly {
		// SQLite 的 Windows VFS 在数据库文件被其他进程以排他方式占用时，
		// 会静默降级为只读打开（READWRITE 打开失败后回退到 READONLY）。
		// 此时若表已存在，AutoMigrate 会静默通过，后续写操作才报
		// "attempt to write a readonly database"，问题被延迟到事务里才暴露。
		// 这里用一条零行 DELETE 探测写能力，把错误提前到打开阶段并给出明确提示。
		if err = db.Exec("DELETE FROM driver WHERE 1=0").Error; err != nil {
			return nil, errors.Wrap(
				err,
				"driver database is not writable (file may be locked by another process or read-only)",
			)
		}
	}

	return db, nil
}
