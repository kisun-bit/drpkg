package hkc

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"hash/crc32"
	"io"
	"strings"

	"github.com/lunixbochs/struc"
	"github.com/pierrec/lz4"
	"github.com/pkg/errors"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// encryptionKey 是 AES-256-GCM 加密密钥，必须通过 SetEncryptionKey 初始化。
var encryptionKey []byte

// SetEncryptionKey 设置 AES-256-GCM 加密密钥，长度必须为 32 字节。
func SetEncryptionKey(key []byte) error {
	if len(key) != 32 {
		return errors.Errorf("invalid key length %d, AES-256 requires 32 bytes", len(key))
	}
	encryptionKey = make([]byte, 32)
	copy(encryptionKey, key)
	return nil
}

func crc32c(data []byte) uint32 {
	return crc32.Checksum(data, crc32cTable)
}

type Cluster struct {
	Magic   [3]byte // 数据块标识，固定为 "hkc"
	Flags   byte    // 数据块标志：0x01=已加密（AES-256-GCM），0x02=已压缩（LZ4），0x04=已校验（CRC32C）
	ID      uint64  // 数据块唯一标识。作用域为当前备份链；每条备份链从0开始分配，并按数据块生成顺序递增。
	CRC32C  uint32  // 原始数据的 CRC32C 校验值
	Offset  uint64  // 数据块在源磁盘中的字节偏移
	RawSize uint64  // 原始数据大小（压缩和加密前）
	Size    uint64  `struc:"sizeof=Payload"` // Payload 实际存储大小（压缩和加密后）
	Payload []byte  // 数据块内容（根据 Flags 标识决定是否经过压缩或加密）
}

func (c *Cluster) String() string {
	var flags []string

	if c.Flags&0x02 != 0 {
		flags = append(flags, "compressed")
	}
	if c.Flags&0x04 != 0 {
		flags = append(flags, "checked")
	}
	if c.Flags&0x01 != 0 {
		flags = append(flags, "encrypted")
	}

	return fmt.Sprintf(
		"[Cluster-%d<off=%d,size=%d,rsize=%d>(%s)]",
		c.ID,
		c.Offset,
		c.Size,
		c.RawSize,
		strings.Join(flags, ","),
	)
}

// Check 验证数据块的静态完整性：Magic 标识正确且 Size 与 Payload 长度一致。
func (c *Cluster) Check() error {
	if c.Magic != [3]byte{'h', 'k', 'c'} {
		return errors.Errorf("invalid magic: %x", c.Magic)
	}
	if uint64(len(c.Payload)) != c.Size {
		return errors.Errorf("payload size mismatch: header=%d, actual=%d", c.Size, len(c.Payload))
	}
	return nil
}

func (c *Cluster) Write(w io.Writer) error {
	if e := c.Check(); e != nil {
		return e
	}
	return struc.Pack(w, c)
}

func (c *Cluster) Pack() ([]byte, error) {
	buf := new(bytes.Buffer)
	if e := struc.Pack(buf, c); e != nil {
		return nil, errors.Wrap(e, "pack Cluster")
	}
	return buf.Bytes(), nil
}

// GetRawData 对 Payload 执行解密、解压，并验证原始数据的 CRC32C 校验值。
func (c *Cluster) GetRawData() ([]byte, error) {
	data := c.Payload
	var err error

	// 解密
	if c.Flags&0x01 != 0 {
		data, err = decryptPayload(data)
		if err != nil {
			return nil, errors.Wrap(err, "decrypt payload")
		}
	}

	// 解压
	if c.Flags&0x02 != 0 {
		data, err = decompressPayload(data, int(c.RawSize))
		if err != nil {
			return nil, errors.Wrap(err, "decompress payload")
		}
	}

	// 验证原始数据大小
	if uint64(len(data)) != c.RawSize {
		return nil, errors.Errorf("raw data size mismatch: expected=%d, actual=%d", c.RawSize, len(data))
	}

	// 验证 CRC32C
	if c.Flags&0x04 != 0 {
		crc := crc32c(data)
		if crc != c.CRC32C {
			return nil, errors.Errorf("CRC32C mismatch: expected=0x%08x, actual=0x%08x", c.CRC32C, crc)
		}
	}

	return data, nil
}

// CreateCluster 基于磁盘数据创建 Cluster。
// data 必须是原始磁盘数据。处理顺序：校验 → 压缩 → 加密。
func CreateCluster(id uint64, offset uint64, data []byte, compress, encrypt, check bool) (*Cluster, error) {
	c := &Cluster{
		Magic:   [3]byte{'h', 'k', 'c'},
		ID:      id,
		Offset:  offset,
		RawSize: uint64(len(data)),
	}

	payload := data

	// 计算校验值
	if check {
		c.CRC32C = crc32c(data)
		c.Flags |= 0x04
	}

	// 压缩
	if compress {
		compressed, compressedOK, err := compressPayload(data)
		if err != nil {
			return nil, errors.Wrap(err, "compress data")
		}
		if compressedOK {
			payload = compressed
			c.Flags |= 0x02
		}
	}

	// 加密
	if encrypt {
		encrypted, err := encryptPayload(payload)
		if err != nil {
			return nil, errors.Wrap(err, "encrypt data")
		}
		payload = encrypted
		c.Flags |= 0x01
	}

	c.Payload = payload
	c.Size = uint64(len(payload))

	return c, nil
}

// ReadCluster 从 Reader 中读取一个完整的 Cluster 数据块并验证其静态完整性。
func ReadCluster(r io.Reader) (*Cluster, error) {
	c := &Cluster{}
	if err := struc.Unpack(r, c); err != nil {
		return nil, errors.Wrap(err, "unpack Cluster")
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	return c, nil
}

// compressPayload 使用 LZ4 块压缩。如果压缩后没有收益，返回原始数据且 compressed=false。
func compressPayload(data []byte) (result []byte, compressed bool, err error) {
	if len(data) == 0 {
		return data, false, nil
	}
	bound := lz4.CompressBlockBound(len(data))
	dst := make([]byte, bound)
	n, err := lz4.CompressBlock(data, dst, nil)
	if err != nil {
		return nil, false, err
	}
	if n == 0 || n >= len(data) {
		return data, false, nil
	}
	return dst[:n], true, nil
}

// decompressPayload 使用 LZ4 块解压，需要知道原始数据大小以分配目标缓冲区。
func decompressPayload(data []byte, rawSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	dst := make([]byte, rawSize)
	n, err := lz4.UncompressBlock(data, dst)
	if err != nil {
		return nil, errors.Wrap(err, "lz4 uncompress")
	}
	return dst[:n], nil
}

// encryptPayload 使用 AES-256-GCM 加密。输出格式：nonce(12字节) || ciphertext || tag(16字节)。
func encryptPayload(data []byte) ([]byte, error) {
	if len(encryptionKey) == 0 {
		return nil, errors.New("encryption key not set, call SetEncryptionKey first")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.Wrap(err, "generate nonce")
	}
	// Seal(dst, nonce, plaintext, additionalData) → dst || ciphertext || tag
	return gcm.Seal(nonce, nonce, data, nil), nil
}

// decryptPayload 使用 AES-256-GCM 解密。输入格式：nonce(12字节) || ciphertext || tag(16字节)。
func decryptPayload(data []byte) ([]byte, error) {
	if len(encryptionKey) == 0 {
		return nil, errors.New("encryption key not set, call SetEncryptionKey first")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize+gcm.Overhead() {
		return nil, errors.Errorf("encrypted data too short: %d bytes", len(data))
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
