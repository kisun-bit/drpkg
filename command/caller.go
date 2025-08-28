package command

import "runtime"

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
		defaultCmdOptions.caller = defaultCaller
		defaultCmdOptions.callerArgs = callerMap[defaultCaller]
	}
}
