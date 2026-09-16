package hkd

import (
	"bytes"
	"testing"

	"github.com/kisun-bit/drpkg/storage/backend"
)

func benchOptions() Options {
	return Options{
		DisasterSystemID: "sys00001",
		UserID:           "00000001",
		StorageMediaID:   "123e4567-e89b-12d3-a456-426614174000",
		PolicyID:         "123e4567-e89b-12d3-a456-426614174000",
		TaskID:           "123e4567-e89b-12d3-a456-426614174000",
		HostID:           "host0001",
		DiskID:           "disk0001",
		DiskSize:         64 << 20,
		LBASize:          512,
		PBASize:          4096,
		ClusterSize:      64 << 10,
		Check:            true,
	}
}

func benchAccessor(b *testing.B) backend.Accessor {
	b.Helper()
	acc, err := backend.New(backend.LocalFsConfig{SubPath: b.TempDir()})
	if err != nil {
		b.Fatalf("backend.New: %v", err)
	}
	if err := acc.Online(); err != nil {
		b.Fatalf("Online: %v", err)
	}
	b.Cleanup(func() { _ = acc.Offline() })
	return acc
}

func BenchmarkWriteAt(b *testing.B) {
	acc := benchAccessor(b)
	opt := benchOptions()
	data := make([]byte, opt.DiskSize)
	fillPattern(data, 7)

	b.SetBytes(int64(opt.DiskSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, err := Create(acc, opt)
		if err != nil {
			b.Fatal(err)
		}
		if err := h.WriteAt(0, data); err != nil {
			b.Fatal(err)
		}
		if err := h.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteAtCompress(b *testing.B) {
	acc := benchAccessor(b)
	opt := benchOptions()
	opt.Compress = true
	data := bytes.Repeat([]byte("compressible-pattern-0123456789"), int(opt.DiskSize)/32)

	b.SetBytes(int64(opt.DiskSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, err := Create(acc, opt)
		if err != nil {
			b.Fatal(err)
		}
		if err := h.WriteAt(0, data); err != nil {
			b.Fatal(err)
		}
		if err := h.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadAt(b *testing.B) {
	acc := benchAccessor(b)
	opt := benchOptions()
	data := make([]byte, opt.DiskSize)
	fillPattern(data, 7)

	h, err := Create(acc, opt)
	if err != nil {
		b.Fatal(err)
	}
	if err := h.WriteAt(0, data); err != nil {
		b.Fatal(err)
	}
	if err := h.Close(); err != nil {
		b.Fatal(err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		b.Fatal(err)
	}
	defer h2.Close()

	buf := make([]byte, opt.DiskSize)
	b.SetBytes(int64(opt.DiskSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := h2.ReadAt(0, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteAtOverwrite(b *testing.B) {
	acc := benchAccessor(b)
	opt := benchOptions()
	opt.Compress = true
	opt.Compact = true

	big := make([]byte, opt.ClusterSize)
	fillPattern(big, 9)

	b.SetBytes(int64(opt.ClusterSize))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, err := Create(acc, opt)
		if err != nil {
			b.Fatal(err)
		}
		// 对同一 Cluster 覆盖写多次，触发 COW/原地覆盖。
		for j := 0; j < 8; j++ {
			fillPattern(big, byte(j))
			if err := h.WriteAt(0, big); err != nil {
				b.Fatal(err)
			}
		}
		if err := h.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
