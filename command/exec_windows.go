package command

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"github.com/kisun-bit/drpkg/logger"
	"golang.org/x/sys/windows"
	"golang.org/x/text/transform"
)

func waitCmdOutput(wg *sync.WaitGroup, stdoutBuf, stderrBuf *bytes.Buffer) (output string, errOutput string) {
	if wg == nil || stdoutBuf == nil || stderrBuf == nil {
		return "", ""
	}
	wg.Wait()

	stdoutRaw, stderrRaw := stdoutBuf.Bytes(), stderrBuf.Bytes()

	cp, err := windows.GetConsoleCP()
	if err != nil {
		logger.Warnf("waitCmdOutput() GetConsoleCP: %v", err)
		return string(stdoutRaw), string(stderrRaw)
	}

	stdoutReader := transform.NewReader(stdoutBuf, codePageToEncoding(cp).NewDecoder())
	stdoutOutput, e := io.ReadAll(stdoutReader)
	if e != nil {
		logger.Warnf("waitCmdOutput() decode stdout with cp(%v): %v", cp, e)
		return string(stdoutRaw), string(stderrRaw)
	}

	stderrReader := transform.NewReader(stderrBuf, codePageToEncoding(cp).NewDecoder())
	stderrOutput, e := io.ReadAll(stderrReader)
	if e != nil {
		logger.Warnf("waitCmdOutput() decode stderr with cp(%v): %v", cp, e)
		return string(stdoutRaw), string(stderrRaw)
	}

	return string(stdoutOutput), strings.TrimSpace(string(stderrOutput))
}
