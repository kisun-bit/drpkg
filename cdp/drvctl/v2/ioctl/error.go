package ioctl

import "github.com/pkg/errors"

var (
	ErrorOverflow             = errors.New("shared-memory overflowed")
	ErrorInsufficientMemSpace = errors.New("insufficient memory space")
)
