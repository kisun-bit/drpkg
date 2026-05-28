package extend

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version 表示任意段版本号，如：1.2.3.4
type Version struct {
	parts []uint64
}

// Parse 从任意字符串解析版本
//
// 支持：
//
//	"1.2.3"
//	"v1.2.3"
//	"windows-10.0.19045"
//	"6.1"
//	"3"
func Parse(s string) Version {
	// 提取所有连续数字
	re := regexp.MustCompile(`\d+`)
	nums := re.FindAllString(s, -1)

	parts := make([]uint64, 0, len(nums))

	for _, n := range nums {
		v, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			continue
		}

		parts = append(parts, v)
	}

	// 空版本默认 0
	if len(parts) == 0 {
		parts = []uint64{0}
	}

	return Version{
		parts: trimTrailingZero(parts),
	}
}

// MustParse 解析失败时 panic（实际上不会失败）
func MustParse(s string) Version {
	return Parse(s)
}

// String 返回标准版本字符串
func (v Version) String() string {
	if len(v.parts) == 0 {
		return "0"
	}

	arr := make([]string, len(v.parts))

	for i, p := range v.parts {
		arr[i] = strconv.FormatUint(p, 10)
	}

	return strings.Join(arr, ".")
}

// Equal 是否相等
func (v Version) Equal(other Version) bool {
	return v.Compare(other) == 0
}

// GreaterThan 是否大于
func (v Version) GreaterThan(other Version) bool {
	return v.Compare(other) > 0
}

// LessThan 是否小于
func (v Version) LessThan(other Version) bool {
	return v.Compare(other) < 0
}

// GreaterOrEqual 是否 >=
func (v Version) GreaterOrEqual(other Version) bool {
	return v.Compare(other) >= 0
}

// LessOrEqual 是否 <=
func (v Version) LessOrEqual(other Version) bool {
	return v.Compare(other) <= 0
}

// Compare
//
// 返回：
//
//	-1 v < other
//	 0 v == other
//	 1 v > other
func (v Version) Compare(other Version) int {
	maxLen := len(v.parts)

	if len(other.parts) > maxLen {
		maxLen = len(other.parts)
	}

	for i := 0; i < maxLen; i++ {
		var a uint64
		var b uint64

		if i < len(v.parts) {
			a = v.parts[i]
		}

		if i < len(other.parts) {
			b = other.parts[i]
		}

		if a > b {
			return 1
		}

		if a < b {
			return -1
		}
	}

	return 0
}

// Parts 返回版本段
func (v Version) Parts() []uint64 {
	ret := make([]uint64, len(v.parts))
	copy(ret, v.parts)
	return ret
}

func (v Version) IsZero() bool {
	for _, p := range v.parts {
		if p != 0 {
			return false
		}
	}

	return true
}

func trimTrailingZero(parts []uint64) []uint64 {
	last := len(parts)

	for last > 1 && parts[last-1] == 0 {
		last--
	}

	return parts[:last]
}

//
// 排序支持
//

type Versions []Version

func (v Versions) Len() int {
	return len(v)
}

func (v Versions) Less(i, j int) bool {
	return v[i].LessThan(v[j])
}

func (v Versions) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func Example() {
	v1 := Parse("10.0.19045")
	v2 := Parse("10.0.16299")

	fmt.Println(v1.GreaterThan(v2)) // true

	fmt.Println(Parse("1.2").Equal(Parse("1.2.0"))) // true

	fmt.Println(Parse("v1.2.3"))
	fmt.Println(Parse("NTamd64.10.0...16299"))

	versions := Versions{
		Parse("10.0.19045"),
		Parse("6.1"),
		Parse("10.0.16299"),
	}

	// sort.Sort(versions)
	fmt.Println(versions)
}
