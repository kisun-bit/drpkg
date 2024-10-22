package util

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"reflect"
	"runtime"
	"strconv"
	"unicode"
	"unsafe"
)

func MustInt64(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}

// SetBits 设置位图前 numBits 个比特为1.
func SetBits(array []byte, numBits int) {
	// 计算需要设置的字节数和最后一个字节的偏移量
	byteIndex := numBits / 8
	bitOffset := numBits % 8

	// 设置完整字节的比特位为1
	for i := 0; i < byteIndex; i++ {
		array[i] = 0xFF
	}

	// 设置最后一个字节的比特位为1
	if bitOffset > 0 {
		mask := byte(0xFF << (8 - bitOffset))
		array[byteIndex] |= mask
	}
}

// 设置位图, i从0开始.
func SetBit(array []byte, i int64, v bool) {
	index := i / 8
	bit := int(7 - i%8)
	if v {
		array[index] |= 1 << bit
	} else {
		array[index] &= ^(1 << bit)
	}
}

// ExistedNonZeroBit 遍历字节数组的指定比特区间，返回首个非0比特位索引.
func ExistedNonZeroBit(array []byte, startBit, endBit int64) (firstNonZeroBitIdx int64, existed bool) {
	totalBit := int64(len(array)) * 8
	if startBit > totalBit {
		return 0, false
	}
	if endBit > totalBit {
		endBit = totalBit - 1
	}
	// 遍历指定的比特区间
	for bitIndex := startBit; bitIndex < endBit; bitIndex++ {
		byteIndex := bitIndex / 8
		bitPosition := bitIndex % 8

		// 检查当前比特位是否为1
		if array[byteIndex]&(1<<(7-bitPosition)) != 0 {
			return bitIndex, true
		}
	}
	return 0, false
}

// IsLastCharDigit 判断字符串的最后一个字符是否是数字.
func IsLastCharDigit(s string) bool {
	if len(s) == 0 {
		return false
	}

	lastChar := s[len(s)-1]
	return unicode.IsDigit(rune(lastChar))
}

func IsNil(v interface{}) bool {
	if v == nil {
		return true
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Ptr, reflect.Map, reflect.Chan, reflect.Slice:
		return reflect.ValueOf(v).IsNil()
	default:
		return false
	}
}

func CompareFuncs(a func(), b func()) bool {
	if a == nil || b == nil {
		return false
	}

	name_a := runtime.FuncForPC(reflect.ValueOf(a).Pointer()).Name()
	name_b := runtime.FuncForPC(reflect.ValueOf(b).Pointer()).Name()

	return name_a == name_b
}

func Md5(b []byte) string {
	m := md5.New()
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

// AllocateBuff 分配一个8字节对齐的字节缓冲区.
func AllocateBuff(length int) []byte {
	buffer := make([]byte, length+8)
	offset := int(uintptr(unsafe.Pointer(&buffer[0])) & uintptr(0xF))

	return buffer[offset:]
}

func BytesEqual(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for idx, a_item := range a {
		if a_item != b[idx] {
			return false
		}
	}

	return true
}

func ToString(x interface{}) string {
	switch t := x.(type) {
	case string:
		return t

	case []byte:
		return string(t)

	case fmt.Stringer:
		return t.String()

	default:
		return fmt.Sprintf("%v", x)
	}
}

func ToInt64(x interface{}) (int64, bool) {
	switch t := x.(type) {
	case bool:
		if t {
			return 1, true
		} else {
			return 0, true
		}
	case int:
		return int64(t), true
	case uint8:
		return int64(t), true
	case int8:
		return int64(t), true
	case uint16:
		return int64(t), true
	case int16:
		return int64(t), true
	case uint32:
		return int64(t), true
	case int32:
		return int64(t), true
	case uint64:
		return int64(t), true
	case int64:
		return t, true

	case string:
		value, err := strconv.ParseInt(t, 0, 64)
		return value, err == nil

	case float64:
		return int64(t), true

	default:
		return 0, false
	}
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func GUIDFromBytes(arr []uint8) (g windows.GUID, err error) {
	if len(arr) != 16 {
		return windows.GUID{}, errors.New("length of bytes array of GUID is insufficient")
	}
	g.Data1 = uint32(arr[3])<<24 | uint32(arr[2])<<16 | uint32(arr[1])<<8 | uint32(arr[0])
	g.Data2 = uint16(arr[5])<<8 | uint16(arr[4])
	g.Data3 = uint16(arr[7])<<8 | uint16(arr[6])
	copy(g.Data4[:], arr[8:])
	return g, nil
}
