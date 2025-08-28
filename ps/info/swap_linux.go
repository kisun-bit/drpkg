package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func SwapInfo() (ss []LinuxSwap, err error) {
	bs, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(bs), "\n") {
		lineItems := strings.Fields(line)
		if len(lineItems) < 5 || len(lineItems) > 0 && !strings.HasPrefix(lineItems[0], "/") {
			continue
		}
		s := LinuxSwap{
			Filename: lineItems[0],
			Type:     lineItems[1],
			Size:     utils.MustInt64(lineItems[2]) * 1024,
			Used:     utils.MustInt64(lineItems[3]) * 1024,
			Priority: int(utils.MustInt64(lineItems[4])),
		}
		if strings.HasPrefix(s.Filename, "/dev") {
			s.UUID = ioctl.MatchDiskBy(ioctl.DevDiskByUUID, filepath.Base(s.Filename))
			s.Label = ioctl.MatchDiskBy(ioctl.DevDiskByLabel, filepath.Base(s.Filename))
		}
		s.Brief = fmt.Sprintf("LinuxSwap-%s:(Used/Total:%s/%s)",
			s.Filename,
			utils.TrimAllSpace(humanize.IBytes(uint64(s.Used))),
			utils.TrimAllSpace(humanize.IBytes(uint64(s.Size))))
		ss = append(ss, s)
	}
	return ss, nil
}
