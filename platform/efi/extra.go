package efi

import (
	"fmt"
	"log"
	"regexp"
	"unicode/utf16"
)

// DecodeUTF16 将输入按 little-endian UTF-16 解码为字符串。
//
// UEFI 变量数据不保证偶数长度：Linux 上 efivarfs 返回的是「4 字节属性 + 载荷」，
// 而 BootXXXX 这类启动项本身又是「属性 + 路径长度 + 描述串(UTF-16) + 设备路径 + 可选数据」
// 的变长布局，可选数据长度任意，部分固件还会以单个 0x00 或不带 NULL 结尾，
// 因而末尾可能残留不成对的单个字节。这里按 2 字节一组成对解码，忽略末尾不成对的
// 半个码元，避免因奇数字节长度报错。
func DecodeUTF16(b []byte) string {
	u16s := make([]uint16, 0, (len(b)+1)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16s = append(u16s, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16s))
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
