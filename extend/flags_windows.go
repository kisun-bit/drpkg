package extend

import "os"

const (
	W_DSYNC_MODE = os.O_WRONLY | os.O_SYNC
	R_DSYNC_MODE = os.O_RDONLY | os.O_SYNC
)
