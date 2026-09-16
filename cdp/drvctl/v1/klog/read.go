package klog

import (
	"runtime"

	"github.com/kisun-bit/drpkg/cdp/drvctl/v1/ioctl"
	"github.com/kisun-bit/drpkg/xutil"
)

func LogRead() (log *ioctl.DRVRespGetLog, err error, status LogStatus) {
	var req ioctl.DRVReqGetLog

	resp, err := ioctl.ReqDrvGetLog(&req)
	if err != nil {
		return nil, err, LogError
	}

	if resp.LogLevel == 0 {
		return nil, nil, LogNoData
	}

	if runtime.GOOS == "windows" {
		resp.TimeStamp = uint64(xutil.TimeByMicrosoftTimestamp(resp.TimeStamp).UnixMicro())
	}

	return resp, nil, LogSuccess
}

func LogGetErrStr(errCode int32) (str string, err error) {
	var req ioctl.DRVReqGetErrStr

	req.ErrCode = errCode
	resp, err := ioctl.ReqDrvGetErrStr(&req)
	if err != nil {
		return "", err
	}

	return xutil.TrimZeroString(resp.ErrStr[:]), nil
}
