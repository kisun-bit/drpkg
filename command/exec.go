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

	opt := defaultCmdOptions
	for _, option := range options {
		option(&opt)
	}
	if err = checkOptions(opt); err != nil {
		return 1, "", err
	}

	argList := make([]string, 0, len(opt.callerArgs)+1)
	argList = append(argList, opt.callerArgs...)
	argList = append(argList, cmdline)

	return runProgram(ctx, cmdline, opt, opt.caller, argList)
}

func Execute(cmdline string, options ...CmdOption) (exit int, output string, err error) {
	return ExecuteWithContext(context.Background(), cmdline, options...)
}

// ExecuteArgsWithContext 直接执行指定程序与参数列表，不经过 shell
// （caller）包装。与 ExecuteWithContext（把命令行字符串交给
// cmd.exe /c 执行）不同，本函数将参数原样传递给目标程序，
// 路径中含空格或特殊字符时不会引入额外的引号问题。
//
// 注意：选项中的 caller / callerArgs 设置在本函数中不生效，
// 仅 WithDebug / WithTimeout / WithDir / WithEnv 有效。
func ExecuteArgsWithContext(ctx context.Context, program string, args []string, options ...CmdOption) (exit int, output string, err error) {
	display := program + " " + strings.Join(args, " ")
	defer func() {
		err = errors.Wrapf(err, "execute `%s`", display)
	}()

	if program == "" {
		return 1, "", errors.New("please specify program")
	}

	opt := defaultCmdOptions
	for _, option := range options {
		option(&opt)
	}

	return runProgram(ctx, display, opt, program, args)
}

func ExecuteArgs(program string, args []string, options ...CmdOption) (exit int, output string, err error) {
	return ExecuteArgsWithContext(context.Background(), program, args, options...)
}

// runProgram 是 ExecuteWithContext 与 ExecuteArgsWithContext 的
// 共享执行核心：解析程序路径、应用超时、采集输出并计算退出码。
func runProgram(ctx context.Context, display string, opt cmdConfig, program string, argList []string) (exit int, output string, err error) {
	if ctx == nil {
		return 1, "", errors.New("nil context")
	}

	if opt.dir != "" {
		if _, e := os.Stat(opt.dir); e != nil {
			return 1, "", e
		}
	}

	programPath, err := exec.LookPath(program)
	if err != nil {
		return 1, "", err
	}

	cmdCtx := ctx
	if opt.timeout > 0 {
		cancelCtx, cancel := context.WithTimeout(ctx, opt.timeout)
		cmdCtx = cancelCtx
		defer cancel()
	}

	if opt.debug {
		defer func() {
			logger.Debugf("runProgram: exec `%s`\nreturn:%v\noutput:\n%s\nerror:%v",
				display, exit, output, err)
		}()
	}

	cmdProc := exec.CommandContext(cmdCtx, programPath, argList...)
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
