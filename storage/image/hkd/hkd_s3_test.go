package hkd

import (
	"bytes"
	"os"
	"testing"

	"github.com/kisun-bit/drpkg/storage/backend"
)

// s3ConfigFromEnv 从环境变量构造 S3 配置；未设置 S3_ENDPOINT 时返回 nil。
//
// 需要设置：S3_ENDPOINT / S3_AK / S3_SK / S3_REGION / S3_BUCKET，
// 可选：S3_SSL（"true"）、S3_STYLE（auto/path/virtual-hosted）。
func s3ConfigFromEnv() *backend.S3Config {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		return nil
	}
	return &backend.S3Config{
		Endpoint:    endpoint,
		AccessKey:   os.Getenv("S3_AK"),
		SecretKey:   os.Getenv("S3_SK"),
		Region:      os.Getenv("S3_REGION"),
		Bucket:      os.Getenv("S3_BUCKET"),
		EnableSSL:   os.Getenv("S3_SSL") == "true",
		AccessStyle: os.Getenv("S3_STYLE"),
	}
}

// TestS3WriteReadOverwriteCompact 用极小文件走 S3 的完整写读 / 覆盖写 / Compact
// 流程，验证 hkd 通过 backend 抽象在对象存储介质上的一致性。
//
// 默认未设置 S3_ENDPOINT 时跳过；仅当显式提供 S3_* 环境变量时运行。
func TestS3WriteReadOverwriteCompact(t *testing.T) {
	cfg := s3ConfigFromEnv()
	if cfg == nil {
		t.Skip("S3_ENDPOINT not set; set S3_* env vars to run S3 integration test")
	}

	acc, err := backend.New(*cfg)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	if err := acc.Online(); err != nil {
		t.Fatalf("Online: %v", err)
	}
	defer acc.Offline()

	// ---- 完整写读 ----
	opt := testOptions()
	opt.DiskID = "s3-e2e-0001"
	opt.DiskSize = 1 << 20
	opt.ClusterSize = 64 << 10
	opt.Check = true

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := make([]byte, opt.DiskSize)
	fillPattern(want, 11)
	if err := h.WriteAt(0, want); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	defer func() { _ = h.Release() }()

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := make([]byte, opt.DiskSize)
	if _, err := h2.ReadAt(0, got); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	_ = h2.Close()
	if !bytes.Equal(got, want) {
		t.Fatalf("s3 full read mismatch")
	}

	// ---- 覆盖写 + Compact ----
	opt2 := testOptions()
	opt2.DiskID = "s3-e2e-0002"
	opt2.DiskSize = 1 << 20
	opt2.ClusterSize = 64 << 10
	opt2.Compress = true
	opt2.Compact = true

	h3, err := Create(acc, opt2)
	if err != nil {
		t.Fatalf("Create (overwrite): %v", err)
	}
	small := bytes.Repeat([]byte("ab"), int(opt2.ClusterSize)/2)
	big := make([]byte, opt2.ClusterSize)
	fillPattern(big, 12)

	if err := h3.WriteAt(0, small); err != nil {
		t.Fatalf("WriteAt small: %v", err)
	}
	if err := h3.WriteAt(0, big); err != nil {
		t.Fatalf("WriteAt big (overwrite): %v", err)
	}
	if err := h3.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := h3.Close(); err != nil {
		t.Fatalf("Close (overwrite): %v", err)
	}
	defer func() { _ = h3.Release() }()

	h4, err := Open(acc, opt2)
	if err != nil {
		t.Fatalf("Open (after compact): %v", err)
	}
	buf := make([]byte, opt2.ClusterSize)
	if _, err := h4.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt (after compact): %v", err)
	}
	_ = h4.Close()
	if !bytes.Equal(buf, big) {
		t.Fatalf("s3 overwrite+compact data mismatch")
	}
}
