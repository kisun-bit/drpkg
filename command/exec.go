package command

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func ExecuteWithContext(ctx context.Context, cmdline string, options ...CmdOption) (exit int, output string, err error) {
	defer func() {
		err = errors.Wrapf(err, "execute `%s`", cmdline)
	}()

	if ctx == nil {
		return 1, "", errors.New("nil context")
	}

	opt := defaultCmdOptions
	for _, option := range options {
		option(&opt)
	}
	if err = checkOptions(opt); err != nil {
		return 1, "", err
	}

	argList := make([]string, 0)
	argList = append(argList, opt.callerArgs...)
	argList = append(argList, cmdline)

	cmdCtx := ctx
	if opt.timeout > 0 {
		cancelCtx, cancel := context.WithTimeout(ctx, opt.timeout)
		cmdCtx = cancelCtx
		defer cancel()
	}

	callerPath, err := exec.LookPath(opt.caller)
	if err != nil {
		return 1, "", err
	}

	if opt.debug {
		defer func() {
			logger.Debugf("ExecuteWithContext: exec `%s`\nreturn:%v\noutput:\n%s\nerror:%v",
				cmdline, exit, output, err)
		}()
	}

	cmdProc := exec.CommandContext(cmdCtx, callerPath, argList...)
	cmdProc.Dir = opt.dir
	cmdProc.Env = opt.env

	stdoutBuf := bytes.NewBuffer(nil)
	stderrBuf := bytes.NewBuffer(nil)

	cmdProc.Stdout = stdoutBuf
	cmdProc.Stderr = stderrBuf

	defer func() {
		exit = 1
		if cmdProc.ProcessState != nil {
			exit = cmdProc.ProcessState.ExitCode()
		} else if err == nil {
			exit = 0
		}

		output = stdoutBuf.String()

		if err != nil {
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				err = errors.Wrapf(err, "stderr: %s", stderr)
			}
		}
	}()

	if err = cmdProc.Start(); err != nil {
		return
	}

	if err = cmdProc.Wait(); err != nil {
		return
	}

	return
}

func Execute(cmdline string, options ...CmdOption) (exit int, output string, err error) {
	return ExecuteWithContext(context.Background(), cmdline, options...)
}

func checkOptions(opt cmdConfig) error {
	if opt.caller == "" {
		return errors.New("please specify caller")
	}
	if _, err := exec.LookPath(opt.caller); err != nil {
		return errors.Wrapf(err, "caller(%s) not found", opt.caller)
	}
	if opt.dir != "" {
		if _, e := os.Stat(opt.dir); e != nil {
			return e
		}
	}
	return nil
}

func codePageToEncoding(cp uint32) encoding.Encoding {
	switch cp {
	case 437:
		return charmap.CodePage437
	case 932:
		return japanese.ShiftJIS
	case 936:
		return simplifiedchinese.GBK
	case 949:
		return korean.EUCKR
	case 950:
		return simplifiedchinese.HZGB2312
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
	case 65001:
		return unicode.UTF8
	default:
		return charmap.Windows1252
	}
}
