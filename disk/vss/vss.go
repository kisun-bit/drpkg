//go:build !windows

// Package vss 提供 Windows 卷影复制服务 (Volume Shadow Copy Service) 的 Go 语言封装。
//
// 支持的功能：
//   - 创建单个卷的快照 (CreateSnapshot)
//   - 批量创建多个卷的快照 (CreateSnapshots)
//   - 查询系统已有快照 (QuerySnapshots)
//   - 删除快照 (Snapshot.Delete / DeleteSnapshot)
//   - 权限检查 (HasSufficientPrivileges)
//
// 最小使用示例：
//
//	if err := vss.HasSufficientPrivileges(); err != nil {
//	    log.Fatal("需要管理员权限:", err)
//	}
//	snap, err := vss.CreateSnapshot(`C:\`, 120*time.Second)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer snap.Delete()
//	fmt.Println("快照设备路径:", snap.DeviceObject)
package vss

import (
	"time"

	"github.com/pkg/errors"
)

var errNotSupported = errors.New("VSS snapshots are only supported on Windows")

// HasSufficientPrivileges 检查当前进程是否具有使用 VSS 的权限。
// 返回 nil 表示有权限，否则返回错误说明原因。
func HasSufficientPrivileges() error {
	return errNotSupported
}

// CreateSnapshot 为指定卷创建一个 VSS 快照。
// volume 是卷路径，如 `C:\` 或 `\\?\Volume{...}\`。
// timeout 控制快照创建的超时时间。
func CreateSnapshot(volume string, timeout time.Duration) (*Snapshot, error) {
	return nil, errNotSupported
}

// CreateSnapshots 为多个卷批量创建 VSS 快照。
// 返回的快照顺序与输入 volumes 一一对应。
func CreateSnapshots(volumes []string, timeout time.Duration) ([]*Snapshot, error) {
	return nil, errNotSupported
}

// QuerySnapshots 查询系统中所有已有的 VSS 快照。
func QuerySnapshots() ([]SnapshotInfo, error) {
	return nil, errNotSupported
}

// DeleteSnapshot 根据快照 ID 删除快照。
func DeleteSnapshot(snapshotID string) error {
	return errNotSupported
}

// GetVolumeNameForMountPoint 返回指定挂载点对应的卷 GUID 路径。
// 例如传入 "C:" 返回 "\\?\Volume{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}\"
func GetVolumeNameForMountPoint(mountPoint string) (string, error) {
	return "", errNotSupported
}