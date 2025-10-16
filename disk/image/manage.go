package image

import (
	"context"
	"fmt"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/pkg/errors"
)

func ImageJsonInfo(ctx context.Context, path string) (string, error) {
	cmdline := fmt.Sprintf("%s info '%s' --output json --force-share", imgToolPath, path)
	_, o, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return "", errors.Wrapf(e, "execute `%s`", cmdline)
	}
	return strings.TrimSpace(o), nil
}
