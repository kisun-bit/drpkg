package others

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsGrpcConnectionLost 判断Grpc连接是否断开（无法连接或连接超时）
func IsGrpcConnectionLost(err error) (lost bool, outErr error) {
	if err == nil {
		return false, nil
	}
	if s, ok := status.FromError(err); ok && (s.Code() == codes.DeadlineExceeded || s.Code() == codes.Unavailable) {
		return true, nil
	}
	return false, err
}
