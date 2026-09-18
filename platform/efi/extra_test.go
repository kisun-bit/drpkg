package efi

import "testing"

func TestDecodeUTF16(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, ""},
		{"ascii even", []byte{'a', 0x00, 'b', 0x00, 'c', 0x00}, "abc"},
		{"odd trailing byte dropped", []byte{'a', 0x00, 'b', 0x00, 0x00}, "ab"},
		{"single dangling byte", []byte{0x00}, ""},
		{"trailing null terminator", []byte{'a', 0x00, 0x00, 0x00}, "a\x00"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecodeUTF16(c.in); got != c.want {
				t.Fatalf("DecodeUTF16(% x) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}