package util

import (
	"context"
	"github.com/pkg/errors"
	"time"
)

var (
	timeoutError = errors.New("Timeout")
)

func Retry(ctx context.Context, cb func() error,
	number int, sleep time.Duration) error {
	var err error
	for i := 0; i < number; i++ {
		err = cb()
		if err == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return timeoutError
		case <-time.After(sleep):
		}
	}
	return err
}
