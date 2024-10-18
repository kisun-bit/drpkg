package lvm

import (
	"fmt"
	"github.com/go-cmd/cmd"
	"runtime"
	"strings"
)

var commandNameWithGOOS = map[string]string{
	"windows": "cmd.exe",
	"linux":   "bash",
}

var commandArgsWithGOOS = map[string][]string{
	"windows": {"/C"},
	"linux":   {"-c"},
}

func commandName(os_ string) string {
	value, ok := commandNameWithGOOS[os_]
	if !ok {
		value = "bash"
	}
	return value
}

func commandArgs(os_ string) []string {
	value, ok := commandArgsWithGOOS[os_]
	if !ok {
		value = []string{}
	}
	return value
}

func ExecV1(format string, formatArgs ...any) (returnCode int, out, errOut string) {
	args := make([]string, 0)
	args = append(args, commandArgs(runtime.GOOS)...)
	args = append(args, fmt.Sprintf(format, formatArgs...))

	c := cmd.NewCmd(commandName(runtime.GOOS), args...)
	status := <-c.Start()
	returnCode = status.Exit
	if len(status.Stdout) != 0 {
		out = strings.Join(status.Stdout, "\n")
	}
	if len(status.Stderr) != 0 {
		errOut = strings.Join(status.Stderr, "\n")
	}

	//if returnCode != 0 {
	//	logging.StreamLog.Warnf("failed to execute `%s`. \nreturn-code: %d \noutput: %s \nerror: %s",
	//		status.Cmd+" "+strings.Join(c.Args, " "), returnCode, out, errOut)
	//}
	return returnCode, out, errOut
}
