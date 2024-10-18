//go:build !windows
// +build !windows

package vss

import (
	"errors"
	"time"
)

// MountPoint is a dummy for non-windows platforms to let client code compile.
type MountPoint struct {
}

var unsupportedErr = errors.New("VSS snapshots are only supported on windows")

// IsSnapshotted is true if this mount point was snapshotted successfully.
func (p *MountPoint) IsSnapshotted() bool {
	return false
}

// GetSnapshotDeviceObject returns root path to access the snapshot files and folders.
func (p *MountPoint) GetSnapshotDeviceObject() string {
	return ""
}

// VssSnapshot is a dummy for non-windows platforms to let client code compile.
type VssSnapshot struct {
	mountPointInfo map[string]MountPoint
}

// HasSufficientPrivilegesForVSS returns true if the user is allowed to use VSS.
func HasSufficientPrivilegesForVSS() error {
	return unsupportedErr
}

// GetVolumeNameForVolumeMountPoint add trailing backslash to input parameter
// and calls the equivalent windows api.
func GetVolumeNameForVolumeMountPoint(mountPoint string) (string, error) {
	return mountPoint, nil
}

// NewVssSnapshot creates a new vss snapshot. If creating the snapshots doesn't
// finish within the timeout an error is returned.
func NewVssSnapshot(_ string,
	_ string, _ time.Duration, _ VolumeFilter, _ bool, _ ErrorHandler) (VssSnapshot, error) {
	return VssSnapshot{}, unsupportedErr
}

func DeleteVssSnapshot(_ string) error {
	return unsupportedErr
}

func QueryVssSnapshot() ([]Snapshot, error) {
	return nil, unsupportedErr
}

// Delete deletes the created snapshot.
func (p *VssSnapshot) Delete() error {
	return nil
}

func (p *VssSnapshot) GetSnapshotID() string {
	return ""
}

func (p *VssSnapshot) GetOriginalVolumeName() string {
	return ""
}

func (p *VssSnapshot) GetCreateTime() uint64 {
	return 0
}

// GetSnapshotDeviceObject returns root path to access the snapshot files
// and folders.
func (p *VssSnapshot) GetSnapshotDeviceObject() string {
	return ""
}
