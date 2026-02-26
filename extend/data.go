package extend

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/bits"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float64 | ~float32
}

func MustInt64(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}

// Min 返回一组数的最小值
func Min[T Number](nums ...T) T {
	if len(nums) == 0 {
		return T(0)
	}
	min := nums[0]
	for _, v := range nums[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// Max 返回一组数中的最大值
func Max[T Number](nums ...T) T {
	if len(nums) == 0 {
		return T(0)
	}
	max := nums[0]
	for _, v := range nums[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

// SetBits 设置位图前numBits个比特为1
func SetBits(array []byte, numBits int) {
	byteIndex := numBits / 8
	bitOffset := numBits % 8

	for i := 0; i < byteIndex; i++ {
		array[i] = 0xFF
	}

	if bitOffset > 0 {
		mask := byte(0xFF << (8 - bitOffset))
		array[byteIndex] |= mask
	}
}

// SetBit 设置位图，i从0开始
func SetBit(array []byte, i int64, v bool) {
	index := i / 8
	bit := int(7 - i%8)

	if v {
		array[index] |= 1 << bit
	} else {
		array[index] &= ^(1 << bit)
	}
}

// ExistedNonZeroBit 遍历字节数组的指定比特区间，返回首个非0比特位索引
func ExistedNonZeroBit(array []byte, startBit, endBit int64) (firstNonZeroBitIdx int64, existed bool) {
	totalBit := int64(len(array)) * 8
	if startBit > totalBit {
		return 0, false
	}
	if endBit > totalBit {
		endBit = totalBit - 1
	}

	for bitIndex := startBit; bitIndex < endBit; bitIndex++ {
		byteIndex := bitIndex / 8
		bitPosition := bitIndex % 8

		if array[byteIndex]&(1<<(7-bitPosition)) != 0 {
			return bitIndex, true
		}
	}
	return 0, false
}

// IterateBitmapOnesFast 遍历位图为1的比特索引，并对其处理
func IterateBitmapOnesFast(buf []byte, bufferSize uint64, fn func(bitIndex uint64)) {
	if bufferSize == 0 {
		return
	}

	maxBytes := uint64(len(buf))
	if bufferSize < maxBytes {
		maxBytes = bufferSize
	}

	for byteIdx := uint64(0); byteIdx < maxBytes; byteIdx++ {
		b := buf[byteIdx]
		for b != 0 {
			bit := bits.TrailingZeros8(b)
			bitIndex := byteIdx*8 + uint64(bit)
			fn(bitIndex)

			b &= b - 1
		}
	}
}

// InsertBitsToHead 在data头部插入bitCount个bit（来自bits，高位优先）
// 示例：
// 原数据:
// [10110010][01100011]
// 插入 bits: 1101  (4 bit)
// 结果:
// [11011011][00100110][00110000]
func InsertBitsToHead(data []byte, bits uint64, bitCount int) []byte {
	if bitCount <= 0 {
		return append([]byte{}, data...)
	}

	totalBits := len(data)*8 + bitCount
	totalBytes := (totalBits + 7) / 8

	result := make([]byte, totalBytes)

	// 写入插入的 bits（MSB-first）
	for i := 0; i < bitCount; i++ {
		bit := (bits >> (bitCount - 1 - i)) & 1

		byteIndex := i / 8
		bitIndex := 7 - (i % 8)

		if bit == 1 {
			result[byteIndex] |= 1 << bitIndex
		}
	}

	// 写入原 data（整体后移 bitCount）
	for i := 0; i < len(data)*8; i++ {
		srcByte := i / 8
		srcBit := 7 - (i % 8)
		bit := (data[srcByte] >> srcBit) & 1

		dstPos := i + bitCount
		dstByte := dstPos / 8
		dstBit := 7 - (dstPos % 8)

		if bit == 1 {
			result[dstByte] |= 1 << dstBit
		}
	}

	return result
}

// BitOnes 生成一个低n位全为1的整数
// 示例：
//
//	传入1，返回0b1
//	传入2，返回0b11
//	传入3，返回0b111
//	......
func BitOnes(n int) uint64 {
	if n <= 0 {
		return 0
	}
	if n >= 64 {
		return ^uint64(0) // 64位全1
	}
	return (uint64(1) << n) - 1
}

// StringEndWithDigit 判断字符串的最后一个字符是否是数字
func StringEndWithDigit(s string) bool {
	if len(s) == 0 {
		return false
	}

	lastChar := s[len(s)-1]
	return unicode.IsDigit(rune(lastChar))
}

// Md5 计算md5
func Md5(b []byte) string {
	m := md5.New()
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

// IsNilType 判断变量是否是空指针
func IsNilType(input interface{}) bool {
	if input == nil {
		return true
	}
	if reflect.TypeOf(input).Kind() == reflect.Ptr && reflect.ValueOf(input).IsNil() {
		return true
	}
	return false
}

func IsContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func IsWinGUIDFormat(guid string) bool {
	if !strings.HasPrefix(guid, "{") || !strings.HasSuffix(guid, "}") {
		return false
	}
	if len(guid) != len("{XXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}") {
		return false
	}
	return true
}

func TrimAllSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func ReadNullTerminatedAscii(buf []byte, offset int) string {
	if offset <= 0 {
		return ""
	}
	buf = buf[offset:]
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			return string(buf[:i])
		}
	}
	return ""
}

func ReadIntFromFile(path string) (int64, error) {
	ret, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(ret)), 0, 64)
}

func ReadStringFromFile(path string) (string, error) {
	ret, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ret)), nil
}

func FileMd5sum(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	code := h.Sum(nil)
	return hex.EncodeToString(code), nil
}

func UnmarshalMsBinary(data []byte, v any) error {
	return json.Unmarshal(TrimUtf8Bom(data), v)
}

func TrimUtf8Bom(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}
