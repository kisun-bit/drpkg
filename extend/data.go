package extend

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
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
