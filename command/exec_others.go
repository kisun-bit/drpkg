//go:build !windows

package command

import (
	"bytes"
	"strings"
	"sync"
)

func waitCmdOutput(wg *sync.WaitGroup, stdoutBuf, stderrBuf *bytes.Buffer) (output string, errOutput string) {
	if wg == nil || stdoutBuf == nil || stderrBuf == nil {
		return "", ""
	}
	wg.Wait()
	return string(stdoutBuf.Bytes()), strings.TrimSpace(string(stderrBuf.Bytes()))
}
