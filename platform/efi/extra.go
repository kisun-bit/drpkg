package efi

import (
	"bytes"
	"fmt"
	"log"
	"regexp"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pkg/errors"
)

// DecodeUTF16 decodes the input as a utf16 string.
// Code from https://github.com/u-root/u-root/blob/master/pkg/uefivars/vars.go
// https://gist.github.com/bradleypeabody/185b1d7ed6c0c2ab6cec
func DecodeUTF16(b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", errors.New("must have even length byte slice")
	}

	u16s := make([]uint16, 1)
	ret := &bytes.Buffer{}
	b8buf := make([]byte, 4)

	lb := len(b)
	for i := 0; i < lb; i += 2 {
		v, e := BytesToU16(b[i : i+2])
		if e != nil {
			return "", e
		}
		u16s[0] = v
		r := utf16.Decode(u16s)
		n := utf8.EncodeRune(b8buf, r[0])
		ret.Write(b8buf[:n])
	}

	return ret.String(), nil
}

// BytesToU16 converts a []byte of length 2 to a uint16.
func BytesToU16(b []byte) (uint16, error) {
	if len(b) != 2 {
		log.Fatalf("BytesToU16: bad len %d (%x)", len(b), b)
	}
	return uint16(b[0]) + (uint16(b[1]) << 8), nil
}

func BootEntryName(bootNumber uint16) string {
	return fmt.Sprintf("Boot%04X", bootNumber)
}

// MatchUEFIPath 匹配 \EFI 开头、.efi 结尾的 UEFI 启动程序路径
func MatchUEFIPath(s string) (string, bool) {
	// 正则：以 \EFI 或 /EFI 开头，中间可以有任意非换行字符，.efi 结尾
	re := regexp.MustCompile(`(?i)([\\/]{1}EFI[\\/][\s\S]*?\.efi)`)
	match := re.FindString(s)
	if match != "" {
		return match, true
	}
	return "", false
}
