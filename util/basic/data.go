package basic

import (
	"crypto/md5"
	"encoding/hex"
	"reflect"
	"strconv"
	"unicode"
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

func IsNil(input interface{}) bool {
	if input == nil {
		return true
	}
	if reflect.TypeOf(input).Kind() == reflect.Ptr && reflect.ValueOf(input).IsNil() {
		return true
	}
	return false
}

func Md5(b []byte) string {
	m := md5.New()
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}
