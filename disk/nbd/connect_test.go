//go:build linux

package nbd

import (
	"testing"
)

func TestBuildConnectCmd(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		index int
		opt   *ConnectOptions
		want  string
	}{
		{
			name:  "simple raw file",
			tool:  "qemu-nbd",
			index: 1,
			opt:   &ConnectOptions{Backend: "/data/disk.raw"},
			want:  "qemu-nbd --connect=/dev/nbd1 '/data/disk.raw'",
		},
		{
			name:  "qcow2 with format and read-only",
			tool:  "/usr/bin/qemu-nbd",
			index: 3,
			opt: &ConnectOptions{
				Backend:  "/data/disk.qcow2",
				Format:   "qcow2",
				ReadOnly: true,
			},
			want: "/usr/bin/qemu-nbd --connect=/dev/nbd3 --format=qcow2 --read-only '/data/disk.qcow2'",
		},
		{
			name:  "cache and discard",
			tool:  "qemu-nbd",
			index: 0,
			opt: &ConnectOptions{
				Backend: "/data/disk.raw",
				Cache:   "writeback",
				Discard: "unmap",
			},
			want: "qemu-nbd --connect=/dev/nbd0 --cache=writeback --discard=unmap '/data/disk.raw'",
		},
		{
			name:  "remote backend",
			tool:  "qemu-nbd",
			index: 2,
			opt:   &ConnectOptions{Remote: "10.0.0.1:10809"},
			want:  "qemu-nbd --connect=/dev/nbd2 -- 10.0.0.1:10809",
		},
		{
			name:  "extra args",
			tool:  "qemu-nbd",
			index: 5,
			opt: &ConnectOptions{
				Backend:   "/data/disk.raw",
				ExtraArgs: []string{"--persistent", "--shared=2"},
			},
			want: "qemu-nbd --connect=/dev/nbd5 --persistent --shared=2 '/data/disk.raw'",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildConnectCmd(c.tool, c.index, c.opt)
			if got != c.want {
				t.Errorf("buildConnectCmd() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDevicePathHelpers(t *testing.T) {
	if got := DevicePath(3); got != "/dev/nbd3" {
		t.Errorf("DevicePath(3) = %q, want /dev/nbd3", got)
	}

	if got := PartitionPath(3, 1); got != "/dev/nbd3p1" {
		t.Errorf("PartitionPath(3,1) = %q, want /dev/nbd3p1", got)
	}
}
