package vss

import "time"

// SnapshotInfo 包含 VSS 快照的元数据信息。
type SnapshotInfo struct {
	SnapshotID    string    // 快照 GUID
	SnapshotSetID string    // 快照集 GUID
	VolumeName    string    // 原始卷名
	DeviceObject  string    // 快照设备对象路径
	CreationTime  time.Time // 创建时间
	Attributes    uint32    // 快照属性
}

// Snapshot 表示一个已创建的 VSS 快照。
// 使用完毕后必须调用 Delete 释放资源。
type Snapshot struct {
	deviceObject string
	volumeName   string
	creationTime time.Time
}

// DeviceObject 返回快照的设备对象路径，可用于直接访问快照中的文件。
func (s *Snapshot) DeviceObject() string {
	return s.deviceObject
}

// VolumeName 返回创建快照的原始卷名。
func (s *Snapshot) VolumeName() string {
	return s.volumeName
}

// CreationTime 返回快照的创建时间。
func (s *Snapshot) CreationTime() time.Time {
	return s.creationTime
}