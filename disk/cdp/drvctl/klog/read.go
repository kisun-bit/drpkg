package klog

import (
	"cdpctl/driver/ioctl"
	"cdpctl/utils"
	"runtime"
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
		resp.TimeStamp = utils.ConvertWindowsTimeToUnixMicros(resp.TimeStamp)
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

	return string(utils.TrimNullBytes(resp.ErrStr[:])), nil
}
