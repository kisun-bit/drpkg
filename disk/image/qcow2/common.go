//go:build linux

package qcow2

import (
	"context"
	"reflect"
	"strconv"
	"strings"
)

func isNil(input interface{}) bool {
	if input == nil {
		return true
	}
	if reflect.TypeOf(input).Kind() == reflect.Ptr && reflect.ValueOf(input).IsNil() {
		return true
	}
	return false
}

func isCancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func strToInt64(number string) (int64, error) {
	number = strings.TrimPrefix(number, "0x")
	return strconv.ParseInt(number, 16, 64)
}
