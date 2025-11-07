package extend

import (
	"os"
	"syscall"
)

const (
	W_DSYNC_MODE = os.O_WRONLY | syscall.O_DSYNC
	R_DSYNC_MODE = os.O_RDONLY | syscall.O_DSYNC | syscall.O_DIRECT
)
