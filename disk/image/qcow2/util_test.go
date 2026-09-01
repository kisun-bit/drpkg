// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrettyFormatDiskSize(t *testing.T) {
	var kb uint64 = 1024
	var mb uint64 = 1024 * 1024
	var gb = mb * kb
	var tb = gb * kb
	var pb = tb * kb
	var eb = pb * kb
	diskSizes := []uint64{
		1,
		1023,
		1 * kb,
		3 * kb / 2,
		1023 * kb,
		1 * mb,
		3 * mb / 2,
		1023 * mb,
		1 * gb,
		3 * gb / 2,
		1023 * gb,
		1 * tb,
		3 * tb / 2,
		1023 * tb,
		1 * pb,
		3 * pb / 2,
		1023 * pb,
		1 * eb,
		3 * eb / 2,
		16*eb - 1,
	}
	expectedValues := []string{
		"1B",
		"1023B",
		"1KB",
		"1.5KB",
		"1023KB",
		"1MB",
		"1.5MB",
		"1023MB",
		"1GB",
		"1.5GB",
		"1023GB",
		"1TB",
		"1.5TB",
		"1023TB",
		"1PB",
		"1.5PB",
		"1023PB",
		"1EB",
		"1.5EB",
		"16EB",
	}
	for index, diskSize := range diskSizes {
		if format := formatDiskSize(diskSize); format != expectedValues[index] {
			t.Errorf(
				"for value %d expected disk format '%s', received '%s'",
				diskSize,
				expectedValues[index],
				format,
			)
		}
	}
}

func TestSubDirectory(t *testing.T) {
	base := t.TempDir()
	type subDirectoryCase struct {
		path     string
		dir      string
		expected bool
	}
	cases := []subDirectoryCase{
		{
			filepath.Join(base, "abc", "def", "hig"),
			filepath.Join(base, "abc", "def") + string(os.PathSeparator),
			true,
		},
		{
			filepath.Join(base, "dd", "cc"),
			filepath.Join(base, "dd"),
			true,
		},
		{
			filepath.Join(base, "t"),
			filepath.Join(base, "l"),
			false,
		},
		{
			filepath.Join(base, "eeee", "l", "l  l  1", "x.qcow2"),
			filepath.Join(base, "eeee", "l") + string(os.PathSeparator),
			true,
		},
		{
			"relative/path",
			base,
			false,
		},
	}
	for _, testCase := range cases {
		expectedString := "matched"
		if !testCase.expected {
			expectedString = "doesn't match"
		}
		if testCase.expected != isSubDirectory(testCase.path, testCase.dir) {
			t.Errorf(
				"path='%s' %s subdirectory of '%s'",
				testCase.path,
				expectedString,
				testCase.dir,
			)
		}
	}
}
