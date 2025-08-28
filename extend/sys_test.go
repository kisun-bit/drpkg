package extend

import "testing"

func TestGetFileSize_windows(t *testing.T) {
	devAndSize := []struct {
		filename string
		expected uint64
	}{
		{`\\.\PHYSICALDRIVE0`, 480103981056},
		{`C:\`, 106760843264},
		{`\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\`, 106760843264},
		{`\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`, 369506643968},
		{`D:\rsa_cdp.log`, 26384167},
	}

	for _, dev := range devAndSize {
		size, err := GetFileSize(dev.filename)
		if err != nil {
			t.Fatal(dev.filename, err)
		}
		if size != dev.expected {
			t.Fatal(dev.filename, size, dev.expected)
		}
	}
}
