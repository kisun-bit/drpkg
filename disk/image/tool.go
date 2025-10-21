//go:build linux

package image

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
)

var DefaultClusterSizeInKiB = int64(512)

func JsonInfo(ctx context.Context, path string) (string, error) {
	cmdline := fmt.Sprintf("%s info '%s' --output json --force-share", imgToolPath, path)
	_, o, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return "", errors.Wrapf(e, "JsonInfo `%s`", cmdline)
	}
	return strings.TrimSpace(o), nil
}

func GetSizeAndFormat(path string) (size int64, format string, err error) {
	imgInfo, err := JsonInfo(context.Background(), path)
	if err != nil {
		return 0, "", err
	}

	format = gjson.Get(imgInfo, "Format").String()
	size = gjson.Get(imgInfo, "virtual-size").Int()
	return
}

func CreateQCow2(ctx context.Context, image string, bytes_ int64) error {
	if strings.Contains(image, " ") {
		return errors.New("image name must not contain spaces")
	}
	cmdline := fmt.Sprintf("%s create -f qcow2 '%s' %vB -o cluster_size=%vk",
		imgToolPath, image, bytes_, DefaultClusterSizeInKiB)
	_, _, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return errors.Wrapf(e, "CreateQCow2 `%s`", cmdline)
	}
	return nil
}

func CreateQCow2WithBackingFile(ctx context.Context, image, backingImage string, bytes_ int64) error {
	if strings.Contains(image, " ") || strings.Contains(backingImage, " ") {
		return errors.New("image name must not contain spaces")
	}
	cmdline := fmt.Sprintf("%s create -f qcow2 -b '%s' -F qcow2 '%s' %vB -o cluster_size=%vk",
		imgToolPath, backingImage, image, bytes_, DefaultClusterSizeInKiB)
	_, _, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return errors.Wrapf(e, "CreateQCow2WithBackingFile `%s`", cmdline)
	}
	return nil
}

func RebaseQCow2(ctx context.Context, image, backingImage string, safe bool) error {
	cmdline := fmt.Sprintf("%s rebase -f qcow2 -b '%s' -F qcow2 '%s'", imgToolPath, backingImage, image)
	if !safe {
		cmdline += " -u"
	}
	_, _, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return errors.Wrapf(e, "RebaseQCow2 `%s`", cmdline)
	}
	return nil
}

func CommitQCow2(ctx context.Context, image string) error {
	cmdline := fmt.Sprintf("%s commit '%s'", imgToolPath, image)
	_, _, e := command.ExecuteWithContext(ctx, cmdline)
	if e != nil {
		return errors.Wrapf(e, "CommitQCow2 `%s`", cmdline)
	}
	return nil
}

func Remove(image string) error {
	return errors.Wrapf(os.Remove(image), "Remove `%s`", image)
}

// MapInfo 磁盘镜像的地址映射信息
// MapInfo 的 key 是磁盘镜像文件路径，val 是一组地址映射（AddrRecord）列表
type MapInfo map[string][]AddrRecord

type AddrRecord struct {
	ImageOffset int64
	DiskOffset  int64
	Length      int
}

func Map(ctx context.Context, image string) (imi MapInfo, err error) {
	imi = make(MapInfo)
	defer func() {
		err = errors.Wrapf(err, "Map `%s`", image)
	}()

	c, cancel := context.WithCancel(ctx)
	defer cancel()

	proc := exec.CommandContext(c, imgToolPath, "map", image)
	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = proc.Start(); err != nil {
		return nil, err
	}

	parseErrMsg := ""
	defer func() {
		if parseErrMsg != "" {
			if err == nil {
				err = errors.New(parseErrMsg)
			} else {
				err = errors.Wrapf(err, "goroutine: %s", parseErrMsg)
			}
		}
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func(_wg *sync.WaitGroup, _errMsg *string) {
		defer _wg.Done()

		_r := bufio.NewScanner(stdoutPipe)
		_lineCount := int64(0)
		for _r.Scan() {
			if extend.IsContextDone(c) {
				return
			}
			_lineCount++
			_line := strings.TrimSpace(_r.Text())
			if len(_line) == 0 {
				continue
			}
			_fields := strings.Fields(_line)
			if _lineCount == 1 {
				if len(_fields) == 5 &&
					_fields[0] == "Offset" &&
					_fields[1] == "Length" &&
					_fields[2] == "Mapped" &&
					_fields[3] == "to" &&
					_fields[4] == "File" {
					continue
				}
				*_errMsg = "header of line [1] dismatched"
				cancel()
				return
			}
			// qemu-img map输出示例：
			// Offset          Length          Mapped to       File
			// 0               0x6e00000       0x50000         full.qcow2
			if len(_fields) != 4 || len(_fields[0]) == 0 || !unicode.IsDigit(rune(_fields[0][0])) {
				*_errMsg = fmt.Sprintf("unrecoglized line: %v, fields: %v, line-count: %v", len(_fields))
				cancel()
				return
			}
			_filepath := strings.TrimSpace(_fields[3])
			_diskOff, _e := addrStrToInt64(_fields[0])
			if _e != nil {
				*_errMsg = fmt.Sprintf("parse disk offset: %v", _line)
				cancel()
				return
			}
			_length, _e := addrStrToInt64(_fields[1])
			if _e != nil {
				*_errMsg = fmt.Sprintf("parse length: %v", _line)
				cancel()
				return
			}
			_fileOff, _e := addrStrToInt64(_fields[2])
			if _e != nil {
				*_errMsg = fmt.Sprintf("parse file offset: %v", _line)
				cancel()
				return
			}
			imi[_filepath] = append(imi[_filepath], AddrRecord{
				DiskOffset:  _diskOff,
				ImageOffset: _fileOff,
				Length:      int(_length),
			})
		}
	}(&wg, &parseErrMsg)

	if e := proc.Wait(); e != nil {
		err = errors.Wrapf(e, "Wait")
		return nil, err
	}

	return imi, nil
}

func addrStrToInt64(number string) (int64, error) {
	number = strings.TrimPrefix(number, "0x")
	return strconv.ParseInt(number, 16, 64)
}
