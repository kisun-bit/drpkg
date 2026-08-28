package hkc

import (
	"bytes"
	"crypto/rand"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// testKey 返回一个固定的 32 字节测试密钥。
func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

// withTestKey 设置全局测试密钥，并在测试结束后清除，避免污染其他用例。
func withTestKey(t *testing.T) {
	t.Helper()
	if err := SetEncryptionKey(testKey()); err != nil {
		t.Fatalf("SetEncryptionKey failed: %v", err)
	}
	t.Cleanup(func() { encryptionKey = nil })
}

// compressibleData 生成高压缩率的数据（重复模式）。
func compressibleData(n int) []byte {
	data := make([]byte, n)
	pattern := []byte("hkc-cluster-test-pattern-0123456789")
	for i := range data {
		data[i] = pattern[i%len(pattern)]
	}
	return data
}

// randomData 生成 n 字节随机数据（几乎不可压缩）。
func randomData(t *testing.T, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}
	return data
}

// newValidCluster 构造一个通过 Check 的最简 Cluster（无标志位，未加工数据）。
func newValidCluster(payload []byte) *Cluster {
	return &Cluster{
		Magic:   [3]byte{'h', 'k', 'c'},
		Offset:  4096,
		RawSize: uint64(len(payload)),
		Size:    uint64(len(payload)),
		Payload: payload,
	}
}

// errWriter 总是写入失败的 io.Writer。
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

// ---------------------------------------------------------------------------
// SetEncryptionKey
// ---------------------------------------------------------------------------

func TestSetEncryptionKey(t *testing.T) {
	t.Cleanup(func() { encryptionKey = nil })

	cases := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{"valid 32 bytes", 32, false},
		{"empty key", 0, true},
		{"16 bytes (AES-128 length)", 16, true},
		{"31 bytes", 31, true},
		{"33 bytes", 33, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := make([]byte, tc.keyLen)
			err := SetEncryptionKey(key)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestSetEncryptionKeyCopiesKey 验证密钥被复制而非引用：
// 修改调用方持有的切片不应影响已保存的密钥。
func TestSetEncryptionKeyCopiesKey(t *testing.T) {
	key := testKey()
	if err := SetEncryptionKey(key); err != nil {
		t.Fatalf("SetEncryptionKey failed: %v", err)
	}
	saved := make([]byte, len(encryptionKey))
	copy(saved, encryptionKey)

	// 篡改调用方切片
	key[0] ^= 0xFF

	if !bytes.Equal(saved, encryptionKey) {
		t.Fatalf("SetEncryptionKey did not copy the key; internal key was mutated")
	}
	encryptionKey = nil
}

// ---------------------------------------------------------------------------
// Cluster.String
// ---------------------------------------------------------------------------

func TestClusterString(t *testing.T) {
	cases := []struct {
		name    string
		cluster Cluster
		want    string
	}{
		{
			name:    "no flags",
			cluster: Cluster{Offset: 0, Size: 10, RawSize: 10},
			want:    "[Cluster<off=0,size=10,rawsize=10>()]",
		},
		{
			name:    "compressed only",
			cluster: Cluster{Flags: 0x02, Offset: 1, Size: 5, RawSize: 10},
			want:    "[Cluster<off=1,size=5,rawsize=10>(compressed)]",
		},
		{
			name:    "checked only",
			cluster: Cluster{Flags: 0x04, Offset: 2, Size: 10, RawSize: 10},
			want:    "[Cluster<off=2,size=10,rawsize=10>(checked)]",
		},
		{
			name:    "encrypted only",
			cluster: Cluster{Flags: 0x01, Offset: 3, Size: 38, RawSize: 10},
			want:    "[Cluster<off=3,size=38,rawsize=10>(encrypted)]",
		},
		{
			name:    "all flags in fixed order compressed,checked,encrypted",
			cluster: Cluster{Flags: 0x07, Offset: 100, Size: 20, RawSize: 40},
			want:    "[Cluster<off=100,size=20,rawsize=40>(compressed,checked,encrypted)]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cluster.String()
			if got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cluster.Check
// ---------------------------------------------------------------------------

func TestClusterCheck(t *testing.T) {
	t.Run("valid cluster", func(t *testing.T) {
		c := newValidCluster([]byte("hello"))
		if err := c.Check(); err != nil {
			t.Fatalf("Check() = %v, want nil", err)
		}
	})

	t.Run("valid empty payload", func(t *testing.T) {
		c := newValidCluster(nil)
		if err := c.Check(); err != nil {
			t.Fatalf("Check() = %v, want nil", err)
		}
	})

	t.Run("bad magic", func(t *testing.T) {
		c := newValidCluster([]byte("hello"))
		c.Magic = [3]byte{'b', 'a', 'd'}
		err := c.Check()
		if err == nil {
			t.Fatal("expected error for bad magic")
		}
		if !strings.Contains(err.Error(), "invalid magic") {
			t.Fatalf("error should mention magic, got: %v", err)
		}
	})

	t.Run("size mismatch header larger", func(t *testing.T) {
		c := newValidCluster([]byte("hello"))
		c.Size = 100
		err := c.Check()
		if err == nil {
			t.Fatal("expected error for size mismatch")
		}
		if !strings.Contains(err.Error(), "payload size mismatch") {
			t.Fatalf("error should mention size mismatch, got: %v", err)
		}
	})

	t.Run("size mismatch header smaller", func(t *testing.T) {
		c := newValidCluster([]byte("hello"))
		c.Size = 1
		if err := c.Check(); err == nil {
			t.Fatal("expected error for size mismatch")
		}
	})
}

// ---------------------------------------------------------------------------
// CreateCluster
// ---------------------------------------------------------------------------

func TestCreateClusterPlain(t *testing.T) {
	data := []byte("plain disk block data")
	c, err := CreateCluster(8192, data, false, false, false)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	if c.Magic != [3]byte{'h', 'k', 'c'} {
		t.Errorf("Magic = %x, want hkc", c.Magic)
	}
	if c.Offset != 8192 {
		t.Errorf("Offset = %d, want 8192", c.Offset)
	}
	if c.RawSize != uint64(len(data)) {
		t.Errorf("RawSize = %d, want %d", c.RawSize, len(data))
	}
	if c.Size != uint64(len(data)) {
		t.Errorf("Size = %d, want %d", c.Size, len(data))
	}
	if c.Flags != 0 {
		t.Errorf("Flags = %#x, want 0", c.Flags)
	}
	if !bytes.Equal(c.Payload, data) {
		t.Error("Payload should equal original data when no processing is enabled")
	}
	if err := c.Check(); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

func TestCreateClusterCheckFlag(t *testing.T) {
	data := compressibleData(256)
	c, err := CreateCluster(0, data, false, false, true)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	if c.Flags&0x04 == 0 {
		t.Error("check flag (0x04) should be set")
	}
	want := crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
	if c.CRC32C != want {
		t.Errorf("CRC32C = %#08x, want %#08x", c.CRC32C, want)
	}
}

func TestCreateClusterCompress(t *testing.T) {
	t.Run("compressible data is compressed", func(t *testing.T) {
		data := compressibleData(64 * 1024)
		c, err := CreateCluster(0, data, true, false, false)
		if err != nil {
			t.Fatalf("CreateCluster failed: %v", err)
		}
		if c.Flags&0x02 == 0 {
			t.Fatal("compressed flag (0x02) should be set for compressible data")
		}
		if c.Size >= c.RawSize {
			t.Errorf("compressed Size %d should be smaller than RawSize %d", c.Size, c.RawSize)
		}
		if uint64(len(c.Payload)) != c.Size {
			t.Errorf("Payload len %d != Size %d", len(c.Payload), c.Size)
		}
		if bytes.Equal(c.Payload, data) {
			t.Error("Payload should differ from original data after compression")
		}
	})

	t.Run("incompressible data keeps raw payload", func(t *testing.T) {
		data := randomData(t, 64*1024)
		c, err := CreateCluster(0, data, true, false, false)
		if err != nil {
			t.Fatalf("CreateCluster failed: %v", err)
		}
		if c.Flags&0x02 != 0 {
			t.Error("compressed flag should not be set for incompressible data")
		}
		if !bytes.Equal(c.Payload, data) {
			t.Error("Payload should equal original data when compression has no gain")
		}
		if c.Size != c.RawSize {
			t.Errorf("Size %d should equal RawSize %d", c.Size, c.RawSize)
		}
	})

	t.Run("tiny data keeps raw payload", func(t *testing.T) {
		data := []byte("abc")
		c, err := CreateCluster(0, data, true, false, false)
		if err != nil {
			t.Fatalf("CreateCluster failed: %v", err)
		}
		if c.Flags&0x02 != 0 {
			t.Error("compressed flag should not be set for tiny data")
		}
		if !bytes.Equal(c.Payload, data) {
			t.Error("Payload should equal original data for tiny input")
		}
	})
}

func TestCreateClusterEncrypt(t *testing.T) {
	t.Run("with key", func(t *testing.T) {
		withTestKey(t)
		data := []byte("secret disk block")
		c, err := CreateCluster(512, data, false, true, false)
		if err != nil {
			t.Fatalf("CreateCluster failed: %v", err)
		}
		if c.Flags&0x01 == 0 {
			t.Error("encrypted flag (0x01) should be set")
		}
		// AES-GCM 输出 = 明文 + nonce(12) + tag(16)
		if c.Size != c.RawSize+12+16 {
			t.Errorf("Size = %d, want %d (raw+nonce+tag)", c.Size, c.RawSize+12+16)
		}
		if bytes.Equal(c.Payload, data) {
			t.Error("Payload should differ from plaintext after encryption")
		}
	})

	t.Run("without key", func(t *testing.T) {
		encryptionKey = nil
		_, err := CreateCluster(0, []byte("data"), false, true, false)
		if err == nil {
			t.Fatal("expected error when encryption key is not set")
		}
	})
}

func TestCreateClusterAllFlags(t *testing.T) {
	withTestKey(t)
	data := compressibleData(32 * 1024)
	c, err := CreateCluster(1<<20, data, true, true, true)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	if c.Flags != 0x07 {
		t.Errorf("Flags = %#x, want 0x07", c.Flags)
	}
	if uint64(len(c.Payload)) != c.Size {
		t.Errorf("Payload len %d != Size %d", len(c.Payload), c.Size)
	}
	// CRC 必须基于原始数据（压缩加密之前）计算
	want := crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli))
	if c.CRC32C != want {
		t.Errorf("CRC32C = %#08x, want %#08x (CRC of raw data)", c.CRC32C, want)
	}
}

func TestCreateClusterEmptyData(t *testing.T) {
	c, err := CreateCluster(0, nil, false, false, false)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	if c.RawSize != 0 || c.Size != 0 {
		t.Errorf("RawSize=%d Size=%d, want both 0", c.RawSize, c.Size)
	}
	if err := c.Check(); err != nil {
		t.Errorf("Check() = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Cluster.GetRawData
// ---------------------------------------------------------------------------

func TestGetRawDataRoundTrip(t *testing.T) {
	withTestKey(t)

	data := compressibleData(16 * 1024)
	rand := randomData(t, 16*1024)

	cases := []struct {
		name     string
		data     []byte
		compress bool
		encrypt  bool
		check    bool
	}{
		{"plain", data, false, false, false},
		{"check only", data, false, false, true},
		{"compress only", data, true, false, false},
		{"compress+check", data, true, false, true},
		{"encrypt only", data, false, true, false},
		{"encrypt+check", data, false, true, true},
		{"compress+encrypt", data, true, true, false},
		{"compress+encrypt+check", data, true, true, true},
		{"incompressible+compress", rand, true, false, true},
		{"incompressible+all", rand, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := CreateCluster(4096, tc.data, tc.compress, tc.encrypt, tc.check)
			if err != nil {
				t.Fatalf("CreateCluster failed: %v", err)
			}
			got, err := c.GetRawData()
			if err != nil {
				t.Fatalf("GetRawData failed: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("GetRawData returned %d bytes differing from original %d bytes", len(got), len(tc.data))
			}
		})
	}
}

func TestGetRawDataCRCMismatch(t *testing.T) {
	data := []byte("crc verification data")
	c, err := CreateCluster(0, data, false, false, true)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	// 篡改 payload（未压缩未加密，直接就是原始数据）
	c.Payload[0] ^= 0xFF
	_, err = c.GetRawData()
	if err == nil {
		t.Fatal("expected CRC mismatch error")
	}
	if !strings.Contains(err.Error(), "CRC32C mismatch") {
		t.Fatalf("error should mention CRC32C mismatch, got: %v", err)
	}
}

func TestGetRawDataDecryptTampered(t *testing.T) {
	withTestKey(t)
	data := []byte("tamper detection data")
	c, err := CreateCluster(0, data, false, true, false)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	// 篡改密文最后一个字节（tag 区域），GCM 认证必须失败
	c.Payload[len(c.Payload)-1] ^= 0xFF
	_, err = c.GetRawData()
	if err == nil {
		t.Fatal("expected decrypt error after tampering ciphertext")
	}
}

func TestGetRawDataDecryptWithoutKey(t *testing.T) {
	withTestKey(t)
	c, err := CreateCluster(0, []byte("data"), false, true, false)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	encryptionKey = nil // 模拟密钥丢失
	_, err = c.GetRawData()
	if err == nil {
		t.Fatal("expected error when key is not set")
	}
}

func TestGetRawDataDecompressInvalid(t *testing.T) {
	data := compressibleData(10 * 1024)
	c, err := CreateCluster(0, data, true, false, false)
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	if c.Flags&0x02 == 0 {
		t.Skip("test data was not compressed")
	}
	// RawSize 设得太小，解压目标缓冲区不足 → 必须报错
	c.RawSize = 10
	_, err = c.GetRawData()
	if err == nil {
		t.Fatal("expected decompress error with insufficient raw size")
	}
}

func TestGetRawDataSizeMismatch(t *testing.T) {
	c := newValidCluster([]byte("abc"))
	c.RawSize = 100 // 与实际数据长度不符
	_, err := c.GetRawData()
	if err == nil {
		t.Fatal("expected raw data size mismatch error")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("error should mention size mismatch, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cluster.Write / Cluster.Pack / ReadCluster
// ---------------------------------------------------------------------------

// headerSize 是固定头部长度：Magic(3)+Flags(1)+CRC32C(4)+Offset(8)+RawSize(8)+Size(8)=32。
const headerSize = 3 + 1 + 4 + 8 + 8 + 8

func TestClusterPack(t *testing.T) {
	data := []byte("pack test payload")
	c := newValidCluster(data)
	packed, err := c.Pack()
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}
	if len(packed) != headerSize+len(data) {
		t.Fatalf("packed length = %d, want %d", len(packed), headerSize+len(data))
	}
	if !bytes.Equal(packed[:3], []byte("hkc")) {
		t.Errorf("packed data should start with magic 'hkc', got %x", packed[:3])
	}
	if !bytes.Equal(packed[headerSize:], data) {
		t.Error("packed tail should be the payload")
	}
}

func TestClusterWrite(t *testing.T) {
	t.Run("valid cluster", func(t *testing.T) {
		c := newValidCluster([]byte("write test"))
		buf := new(bytes.Buffer)
		if err := c.Write(buf); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if buf.Len() != headerSize+len("write test") {
			t.Fatalf("written bytes = %d, want %d", buf.Len(), headerSize+len("write test"))
		}
	})

	t.Run("invalid magic fails before packing", func(t *testing.T) {
		c := newValidCluster([]byte("x"))
		c.Magic = [3]byte{0, 0, 0}
		buf := new(bytes.Buffer)
		err := c.Write(buf)
		if err == nil {
			t.Fatal("expected error from Check")
		}
		if buf.Len() != 0 {
			t.Error("nothing should be written when Check fails")
		}
	})

	t.Run("writer error propagates", func(t *testing.T) {
		c := newValidCluster([]byte("x"))
		if err := c.Write(errWriter{}); err == nil {
			t.Fatal("expected writer error to propagate")
		}
	})
}

func TestReadCluster(t *testing.T) {
	withTestKey(t)

	t.Run("round trip all flags", func(t *testing.T) {
		data := compressibleData(8 * 1024)
		orig, err := CreateCluster(12345, data, true, true, true)
		if err != nil {
			t.Fatalf("CreateCluster failed: %v", err)
		}
		packed, err := orig.Pack()
		if err != nil {
			t.Fatalf("Pack failed: %v", err)
		}
		got, err := ReadCluster(bytes.NewReader(packed))
		if err != nil {
			t.Fatalf("ReadCluster failed: %v", err)
		}
		if got.Magic != orig.Magic {
			t.Errorf("Magic = %x, want %x", got.Magic, orig.Magic)
		}
		if got.Flags != orig.Flags {
			t.Errorf("Flags = %#x, want %#x", got.Flags, orig.Flags)
		}
		if got.CRC32C != orig.CRC32C {
			t.Errorf("CRC32C = %#08x, want %#08x", got.CRC32C, orig.CRC32C)
		}
		if got.Offset != orig.Offset {
			t.Errorf("Offset = %d, want %d", got.Offset, orig.Offset)
		}
		if got.RawSize != orig.RawSize {
			t.Errorf("RawSize = %d, want %d", got.RawSize, orig.RawSize)
		}
		if got.Size != orig.Size {
			t.Errorf("Size = %d, want %d", got.Size, orig.Size)
		}
		if !bytes.Equal(got.Payload, orig.Payload) {
			t.Error("Payload mismatch after round trip")
		}
		// 读回的数据必须能还原原始内容
		raw, err := got.GetRawData()
		if err != nil {
			t.Fatalf("GetRawData failed: %v", err)
		}
		if !bytes.Equal(raw, data) {
			t.Error("restored data differs from original")
		}
	})

	t.Run("empty reader", func(t *testing.T) {
		if _, err := ReadCluster(bytes.NewReader(nil)); err == nil {
			t.Fatal("expected error for empty reader")
		}
	})

	t.Run("truncated payload", func(t *testing.T) {
		c := newValidCluster(compressibleData(1024))
		packed, err := c.Pack()
		if err != nil {
			t.Fatalf("Pack failed: %v", err)
		}
		if _, err := ReadCluster(bytes.NewReader(packed[:len(packed)-10])); err == nil {
			t.Fatal("expected error for truncated payload")
		}
	})

	t.Run("bad magic in stream", func(t *testing.T) {
		c := newValidCluster([]byte("abc"))
		c.Magic = [3]byte{'x', 'y', 'z'}
		packed, err := c.Pack()
		if err != nil {
			t.Fatalf("Pack failed: %v", err)
		}
		_, err = ReadCluster(bytes.NewReader(packed))
		if err == nil {
			t.Fatal("expected invalid magic error")
		}
		if !strings.Contains(err.Error(), "invalid magic") {
			t.Fatalf("error should mention magic, got: %v", err)
		}
	})

	t.Run("size field inconsistent with payload", func(t *testing.T) {
		// 手工构造一个 Size 字段被破坏的字节流（把 Size 写成比实际多 5）
		c := newValidCluster([]byte("12345"))
		packed, err := c.Pack()
		if err != nil {
			t.Fatalf("Pack failed: %v", err)
		}
		// Size 位于偏移 3+1+4+8+8=24 处，大端 8 字节
		packed[24+7] += 5
		if _, err := ReadCluster(bytes.NewReader(packed)); err == nil {
			t.Fatal("expected payload size mismatch error")
		}
	})
}

// TestReadClusterSequential 验证多个数据块可以背靠背写入同一个流并依次读回。
func TestReadClusterSequential(t *testing.T) {
	blocks := [][]byte{
		[]byte("first block"),
		compressibleData(4096),
		randomData(t, 512),
	}

	buf := new(bytes.Buffer)
	for i, b := range blocks {
		c, err := CreateCluster(uint64(i*4096), b, false, false, true)
		if err != nil {
			t.Fatalf("CreateCluster(%d) failed: %v", i, err)
		}
		if err := c.Write(buf); err != nil {
			t.Fatalf("Write(%d) failed: %v", i, err)
		}
	}

	for i, want := range blocks {
		c, err := ReadCluster(buf)
		if err != nil {
			t.Fatalf("ReadCluster(%d) failed: %v", i, err)
		}
		if c.Offset != uint64(i*4096) {
			t.Errorf("block %d Offset = %d, want %d", i, c.Offset, i*4096)
		}
		got, err := c.GetRawData()
		if err != nil {
			t.Fatalf("GetRawData(%d) failed: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("block %d data mismatch", i)
		}
	}

	// 流应被完整消费
	if _, err := ReadCluster(buf); err == nil && err != io.EOF {
		t.Fatal("expected no more clusters in stream")
	}
}

// ---------------------------------------------------------------------------
// 内部辅助函数
// ---------------------------------------------------------------------------

func TestCrc32cKnownValue(t *testing.T) {
	// CRC32C (Castagnoli) 的标准校验值：ASCII "123456789" → 0xE3069283
	got := crc32c([]byte("123456789"))
	if got != 0xE3069283 {
		t.Fatalf("crc32c(\"123456789\") = %#08x, want 0xE3069283", got)
	}
}

func TestCompressPayload(t *testing.T) {
	t.Run("compressible", func(t *testing.T) {
		data := compressibleData(8 * 1024)
		out, ok, err := compressPayload(data)
		if err != nil {
			t.Fatalf("compressPayload failed: %v", err)
		}
		if !ok {
			t.Fatal("expected compression to succeed")
		}
		if len(out) >= len(data) {
			t.Errorf("compressed %d bytes should be smaller than %d", len(out), len(data))
		}
	})

	t.Run("empty data", func(t *testing.T) {
		out, ok, err := compressPayload(nil)
		if err != nil {
			t.Fatalf("compressPayload failed: %v", err)
		}
		if ok {
			t.Error("empty data should not be marked compressed")
		}
		if len(out) != 0 {
			t.Error("empty input should yield empty output")
		}
	})

	t.Run("incompressible", func(t *testing.T) {
		data := randomData(t, 4*1024)
		out, ok, err := compressPayload(data)
		if err != nil {
			t.Fatalf("compressPayload failed: %v", err)
		}
		if ok {
			t.Error("random data should not be marked compressed")
		}
		if !bytes.Equal(out, data) {
			t.Error("incompressible data should be returned unchanged")
		}
	})
}

func TestCompressDecompressPayloadRoundTrip(t *testing.T) {
	data := compressibleData(64 * 1024)
	compressed, ok, err := compressPayload(data)
	if err != nil || !ok {
		t.Fatalf("compressPayload failed: ok=%v err=%v", ok, err)
	}
	restored, err := decompressPayload(compressed, len(data))
	if err != nil {
		t.Fatalf("decompressPayload failed: %v", err)
	}
	if !bytes.Equal(restored, data) {
		t.Fatal("round trip data mismatch")
	}
}

func TestDecompressPayload(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		out, err := decompressPayload(nil, 100)
		if err != nil {
			t.Fatalf("decompressPayload failed: %v", err)
		}
		if out != nil {
			t.Error("empty input should yield nil output")
		}
	})

	t.Run("destination too small", func(t *testing.T) {
		data := compressibleData(4 * 1024)
		compressed, ok, err := compressPayload(data)
		if err != nil || !ok {
			t.Fatalf("compressPayload failed: ok=%v err=%v", ok, err)
		}
		if _, err := decompressPayload(compressed, 10); err == nil {
			t.Fatal("expected error when destination buffer is too small")
		}
	})

	t.Run("invalid stream", func(t *testing.T) {
		garbage := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		if _, err := decompressPayload(garbage, 1024); err == nil {
			t.Fatal("expected error for invalid LZ4 stream")
		}
	})
}

func TestEncryptDecryptPayload(t *testing.T) {
	withTestKey(t)
	data := []byte("encrypt me please")

	t.Run("round trip", func(t *testing.T) {
		enc, err := encryptPayload(data)
		if err != nil {
			t.Fatalf("encryptPayload failed: %v", err)
		}
		if len(enc) != len(data)+12+16 {
			t.Fatalf("encrypted length = %d, want %d", len(enc), len(data)+12+16)
		}
		dec, err := decryptPayload(enc)
		if err != nil {
			t.Fatalf("decryptPayload failed: %v", err)
		}
		if !bytes.Equal(dec, data) {
			t.Fatal("decrypted data differs from plaintext")
		}
	})

	t.Run("nonce is random", func(t *testing.T) {
		a, err := encryptPayload(data)
		if err != nil {
			t.Fatalf("encryptPayload failed: %v", err)
		}
		b, err := encryptPayload(data)
		if err != nil {
			t.Fatalf("encryptPayload failed: %v", err)
		}
		if bytes.Equal(a, b) {
			t.Fatal("two encryptions of the same data must differ (random nonce)")
		}
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		enc, err := encryptPayload(data)
		if err != nil {
			t.Fatalf("encryptPayload failed: %v", err)
		}
		enc[len(enc)/2] ^= 0xFF
		if _, err := decryptPayload(enc); err == nil {
			t.Fatal("expected authentication failure for tampered ciphertext")
		}
	})

	t.Run("too short input", func(t *testing.T) {
		if _, err := decryptPayload(make([]byte, 27)); err == nil {
			t.Fatal("expected error for input shorter than nonce+tag")
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		enc, err := encryptPayload(data)
		if err != nil {
			t.Fatalf("encryptPayload failed: %v", err)
		}
		otherKey := testKey()
		otherKey[31] ^= 0xFF
		if err := SetEncryptionKey(otherKey); err != nil {
			t.Fatalf("SetEncryptionKey failed: %v", err)
		}
		if _, err := decryptPayload(enc); err == nil {
			t.Fatal("expected authentication failure with wrong key")
		}
	})
}

func TestEncryptDecryptPayloadWithoutKey(t *testing.T) {
	encryptionKey = nil
	if _, err := encryptPayload([]byte("x")); err == nil {
		t.Fatal("expected error from encryptPayload without key")
	}
	if _, err := decryptPayload(make([]byte, 64)); err == nil {
		t.Fatal("expected error from decryptPayload without key")
	}
}
