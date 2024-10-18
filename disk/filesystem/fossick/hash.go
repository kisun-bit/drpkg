package fossick

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"io"
)

type _hashSign struct {
	ClusterSize,
	BlockSize,
	HashSize int `struc:"int32"`
	MaxBit, MaxBlock int64
}

// CalculateHashFileSignature 计算某一哈希文件的签名值.
// 当签名值发生变更时, 意味着此设备在本次任务下一定是全量备份的, 无法执行增量备份.
func CalculateHashFileSignature(clusterSize, blockSize, hashSize int, maxBit, maxBlock int64) (string, error) {
	var signStruc = _hashSign{
		ClusterSize: clusterSize,
		BlockSize:   blockSize,
		HashSize:    hashSize,
		MaxBit:      maxBit,
		MaxBlock:    maxBlock,
	}
	b := &bytes.Buffer{}
	err := struc.Pack(b, &signStruc)
	if err != nil {
		return "", err
	}
	s256 := sha256.New()
	s256.Write(b.Bytes())
	return hex.EncodeToString(s256.Sum(nil)), nil
}

func CalculateHashOffset(fsOffset int64, blockSize, hashSize int) (hashOffset int64, err error) {
	if fsOffset%int64(blockSize) != 0 {
		return 0, errors.Errorf("invalid offset %v when the size of block is %v", fsOffset, blockSize)
	}
	blockCount := fsOffset / int64(blockSize)
	return blockCount * int64(hashSize), nil
}

func ReadHash(hashReader io.ReaderAt, hashOffset int64, hashSize int) (hashVal uint64, err error) {
	buf := make([]byte, hashSize)
	n, err := hashReader.ReadAt(buf, hashOffset)
	if err != nil {
		return 0, errors.Wrapf(err, "hash-offset is %v, hash-size is %v", hashOffset, hashSize)
	}
	if n != hashSize {
		return 0, errors.Errorf("hash size mismatch, read size %v but should be %v", n, hashSize)
	}
	return binary.BigEndian.Uint64(buf), nil
}

func WriteHash(hashWriter io.WriterAt, hashOffset int64, hashVal uint64, hashSize int) (err error) {
	tmpBuf := make([]byte, hashSize)
	binary.BigEndian.PutUint64(tmpBuf, hashVal)
	n, err := hashWriter.WriteAt(tmpBuf, hashOffset)
	if err != nil {
		return errors.Wrapf(err, "hash-offset is %v, hash-size is %v", hashOffset, hashSize)
	}
	if n != hashSize {
		return errors.Errorf("hash size mismatch, write size %v but should be %v", n, hashSize)
	}
	return nil
}
