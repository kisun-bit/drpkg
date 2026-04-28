package command

import "time"

type CmdOption func(o *cmdConfig)

type cmdConfig struct {
	timeout    time.Duration
	debug      bool
	dir        string
	env        []string
	caller     string
	callerArgs []string
}

func WithDebug() CmdOption {
	return func(o *cmdConfig) {
		o.debug = true
	}
}

func WithTimeout(timeout time.Duration) CmdOption {
	return func(o *cmdConfig) {
		o.timeout = timeout
	}
}

func WithDir(dir string) CmdOption {
	return func(o *cmdConfig) {
		o.dir = dir
	}
}

func WithEnv(env []string) CmdOption {
	return func(o *cmdConfig) {
		o.env = env
	}
}

func WithCustomCaller(caller string, callerArgs []string) CmdOption {
	return func(o *cmdConfig) {
		o.caller = caller
		o.callerArgs = callerArgs
	}
}
