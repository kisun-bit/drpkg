// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const relativeTestsDir string = "./qcow2tests"

func testsDir() string {
	path, _ := filepath.Abs(relativeTestsDir)
	return path
}

// skipIfQemuImgMissing skips the test when the qemu-img executable is not
// available on the machine running the tests.
func skipIfQemuImgMissing(t testing.TB) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img executable is not available, skipping test")
	}
}

func prepareTestDir(testsDir string, t testing.TB) {
	fileExists, err := PathExists(testsDir)
	if err != nil {
		t.Fatalf("Error while performing stat of tests directory")
	}
	if !fileExists {
		err = os.Mkdir(testsDir, os.ModePerm)
		if err != nil {
			t.Fatalf("Error while creating tests directory")
		}
	}
}

func deleteDiskIfExists(imagePath string, t testing.TB) {
	fileExists, err := PathExists(imagePath)
	if err != nil {
		t.Fatalf("Error while preforming stat of test image file")
	}
	if fileExists {
		err = os.Remove(imagePath)
		if err != nil {
			t.Fatalf("Error while removing file from old tests")
		}
	}
}

func prepareQemuImage(imageName string, imageSize int, t *testing.T) string {
	skipIfQemuImgMissing(t)
	var err error
	prepareTestDir(testsDir(), t)
	imagePath := filepath.Join(testsDir(), imageName+".qcow2")
	deleteDiskIfExists(imagePath, t)
	_, err = createQcow2DiskThroughQemu(imagePath, uint64(imageSize))
	if err != nil {
		t.Fatalf(
			"While preparing qemu image for test and and error occured %s",
			err,
		)
	}
	return imagePath
}

func prepareQemuImageWithParent(
	imageName string,
	parentImageName string,
	imageSize int,
	parentImageSize int,
	t *testing.T,
) (string, string) {
	skipIfQemuImgMissing(t)
	var err error
	prepareTestDir(testsDir(), t)
	parentImagePath := filepath.Join(testsDir(), parentImageName+".qcow2")
	deleteDiskIfExists(parentImagePath, t)
	imagePath := filepath.Join(testsDir(), imageName+".qcow2")
	deleteDiskIfExists(imagePath, t)
	_, err = createQcow2DiskThroughQemuWithParent(
		imagePath,
		parentImagePath,
		uint64(imageSize),
		uint64(parentImageSize),
	)
	if err != nil {
		t.Fatalf(
			"While preparing qemu image for test and and error occured %s",
			err,
		)
	}
	return imagePath, parentImagePath
}
