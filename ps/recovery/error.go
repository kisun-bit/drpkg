package recovery

import "github.com/pkg/errors"

var (
	ErrorRootEnvNotMounted = errors.New("root environment is not mounted")
)
