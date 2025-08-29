package efi

import "golang.org/x/text/encoding/unicode"

var Encoding = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)

type EfiVariable struct {
	Namespace string
	Name      string
	Value     []byte
}
