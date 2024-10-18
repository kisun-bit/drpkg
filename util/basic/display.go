package basic

import (
	"strings"
)

func TrimAllSpace(s string) string {
	return strings.Join(strings.Fields(s), "")
}
