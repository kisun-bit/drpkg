package command

import (
	"os/exec"
	"runtime"
)

var (
	callerMap = map[string][]string{
		"sh":             {"-c"},
		"cmd.exe":        {"/C"},
		"powershell.exe": {"-NoLogo", "-Command"},
	}

	defaultCaller = "sh"

	defaultCmdOptions = cmdConfig{
		caller:     defaultCaller,
		callerArgs: callerMap[defaultCaller],
	}
)

func init() {
	if runtime.GOOS == "windows" {
		defaultCaller = "cmd.exe"
		if _, e := exec.LookPath(defaultCaller); e != nil {
			defaultCaller = "C:\\Windows\\System32\\cmd.exe"
		}
		defaultCmdOptions.caller = defaultCaller
		defaultCmdOptions.callerArgs = callerMap[defaultCaller]
	}
}
