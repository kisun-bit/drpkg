//go:build linux

package qcow2

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// CreateQCow2WithContext 创建镜像文件.
func CreateQCow2WithContext(ctx context.Context, image string, sizeInBytes int64) error {
	cmdLine := fmt.Sprintf("%s create -f qcow2 '%s' %vB", _QemuImgExecPath, image, sizeInBytes)
	return exec.CommandContext(ctx, "sh", "-c", cmdLine).Run()
}

func CreateQCow2(image string, sizeInBytes int64) error {
	return CreateQCow2WithContext(context.TODO(), image, sizeInBytes)
}

// CreateOverlayQCow2WithContext 创建覆盖镜像.
func CreateOverlayQCow2WithContext(ctx context.Context, image, backingImage string, sizeInBytes int64) error {
	// TODO 为了支持后续的备份数据远程迁移，以相对路径创建.
	cmdLine := fmt.Sprintf("%s create -f qcow2 -b '%s' -F qcow2 '%s' %vB -o cluster_size=256k",
		_QemuImgExecPath, backingImage, image, sizeInBytes)
	return exec.CommandContext(ctx, "sh", "-c", cmdLine).Run()
}

func CreateOverlayQCow2(image, backingImage string, sizeInBytes int64) error {
	return CreateOverlayQCow2WithContext(context.TODO(), image, backingImage, sizeInBytes)
}

// RebaseQCow2WithContext 变基.
func RebaseQCow2WithContext(ctx context.Context, image, newBackingImage string) error {
	cmdLine := fmt.Sprintf("%s rebase -b '%s' '%s'", _QemuImgExecPath, newBackingImage, image)
	return exec.CommandContext(ctx, "sh", "-c", cmdLine).Run()
}

func RebaseQCow2(image, newBackingImage string) error {
	return RebaseQCow2WithContext(context.TODO(), image, newBackingImage)
}

// CommitQCow2WithContext 提交(合并).
func CommitQCow2WithContext(ctx context.Context, image string) error {
	cmdLine := fmt.Sprintf("%s commit '%s'", _QemuImgExecPath, image)
	return exec.CommandContext(ctx, "sh", "-c", cmdLine).Run()
}

func CommitQCow2(image string) error {
	return CommitQCow2WithContext(context.TODO(), image)
}

// RemoveQCow2WithContext 删除.
func RemoveQCow2WithContext(ctx context.Context, image string) error {
	cmdLine := fmt.Sprintf("rm -f '%s'", image)
	return exec.CommandContext(ctx, "sh", "-c", cmdLine).Run()
}

func RemoveQCow2(image string) error {
	return RemoveQCow2WithContext(context.TODO(), image)
}

// GeneralInfoQCow2WithContext 查询单个镜像文件的属性.
func GeneralInfoQCow2WithContext(ctx context.Context, image string) (img ImgGeneralInfo, err error) {
	cmdLine := fmt.Sprintf("%s info '%s' --output=json --force-share", _QemuImgExecPath, image)
	output, err := exec.CommandContext(ctx, "sh", "-c", cmdLine).Output()
	if err != nil {
		return img, err
	}
	err = json.Unmarshal(output, &img)
	return img, err
}

func GeneralInfoImage(image string) (img ImgGeneralInfo, err error) {
	return GeneralInfoQCow2WithContext(context.TODO(), image)
}

// GeneralInfoQCow2BackingListWithContext 查询当前镜像文件及其所有的后备文件的属性集合(当前镜像文件总是处于集合第一个).
func GeneralInfoQCow2BackingListWithContext(ctx context.Context, image string) (img []ImgGeneralInfo, err error) {
	cmdLine := fmt.Sprintf("%s info '%s' --output=json --force-share --backing-chain", _QemuImgExecPath, image)
	output, err := exec.CommandContext(ctx, "sh", "-c", cmdLine).Output()
	if err != nil {
		return img, err
	}
	err = json.Unmarshal(output, &img)
	return img, err
}

func GeneralInfoQCow2BackingList(image string) (img []ImgGeneralInfo, err error) {
	return GeneralInfoQCow2BackingListWithContext(context.TODO(), image)
}

func CheckImage(image, imageType string) error {
	cmdLine := fmt.Sprintf("%s check '%s' -f %s", _QemuImgExecPath, image, imageType)
	return exec.Command("sh", "-c", cmdLine).Run()
}
