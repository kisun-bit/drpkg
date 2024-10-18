package storage

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/basic"
	"os"
	"path/filepath"
	"strings"
)

func SwapInfo() (ss []Swap, err error) {
	bs, err := os.ReadFile(ioctl.ProcSwaps)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(bs), "\n") {
		lineItems := strings.Fields(line)
		if len(lineItems) < 5 || len(lineItems) > 0 && !strings.HasPrefix(lineItems[0], "/") {
			continue
		}
		s := Swap{
			Filename: lineItems[0],
			Type:     lineItems[1],
			Size:     basic.MustInt64(lineItems[2]) * 1024,
			Used:     basic.MustInt64(lineItems[3]) * 1024,
			Priority: int(basic.MustInt64(lineItems[4])),
		}
		if strings.HasPrefix(s.Filename, "/dev") {
			s.UUID = ioctl.MatchDiskBy(ioctl.DevDiskByUUID, filepath.Base(s.Filename))
			s.Label = ioctl.MatchDiskBy(ioctl.DevDiskByLabel, filepath.Base(s.Filename))
		}
		s.Brief = fmt.Sprintf("Swap-%s:(Used/Total:%s/%s)",
			s.Filename,
			basic.TrimAllSpace(humanize.IBytes(uint64(s.Used))),
			basic.TrimAllSpace(humanize.IBytes(uint64(s.Size))))
		ss = append(ss, s)
	}
	return ss, nil
}
