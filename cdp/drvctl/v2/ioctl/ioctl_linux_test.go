package ioctl

import "testing"

func TestIocMacros(t *testing.T) {
	// _IO('B', 1) = _IOC(_IOC_NONE, 'B', 1, 0)
	code := ioc(iocNone, biotrkIoctlMagic, 1, 0)
	if code == 0 {
		t.Error("_IO should be non-zero")
	}

	// _IOWR('B', 1, 0)
	iowr := ioc(iocRead|iocWrite, biotrkIoctlMagic, 1, 0)
	if iowr == 0 {
		t.Error("_IOWR should be non-zero")
	}
}

func TestGetIoctlCodeLinux(t *testing.T) {
	// getIoctlCode 在 Linux 上调用 ioc(iocRead|iocWrite, magic, code, 0)
	for _, code := range []uint{1, 2, 3, 15} {
		got := getIoctlCode(code)
		want := ioc(iocRead|iocWrite, biotrkIoctlMagic, code, 0)
		if got != want {
			t.Errorf("getIoctlCode(%d) = %d, want %d", code, got, want)
		}
	}
}
