package ioctl

import (
	"bytes"
	"encoding/binary"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"time"
)

func ReqDrvIoctl(code uint, inBuffer []byte, outBufferSize uint64) (outBuffer []byte, err error) {
	utf16Name, err := windows.UTF16PtrFromString(DriverSymbolName)
	if err != nil {
		return nil, errors.Wrapf(err, "UTF16PtrFromString")
	}

	hDevice, err := windows.CreateFile(
		utf16Name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "CreateFile")
	}
	defer windows.CloseHandle(hDevice)

	var inBufferArr *byte
	inSize := uint32(0)
	if len(inBuffer) > 0 {
		inBufferArr = &inBuffer[0]
		inSize = uint32(len(inBuffer))
	}

	var bytesReturned uint32
	outBuffer = make([]byte, outBufferSize)
	outSize := uint32(len(outBuffer))

	var outBufferPtr *byte = nil
	if outSize > 0 {
		outBufferPtr = &outBuffer[0]
	}

	err = windows.DeviceIoControl(
		hDevice,
		CtlCode(uint32(code)),
		inBufferArr,
		inSize,
		outBufferPtr,
		outSize,
		&bytesReturned,
		nil,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "DeviceIoControl")
	}

	return outBuffer[:bytesReturned], nil
}

// RegCreateDriverParameter 创建驱动的持久化参数
func RegCreateDriverParameter(start *DRVReqStart, _ time.Duration) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, cdpConfigRegPath, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrapf(err, "failed to open registry key")
	}
	defer k.Close()

	reqBuf := new(bytes.Buffer)
	if err = struc.PackWithOptions(reqBuf, start, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return errors.Wrapf(err, "failed to pack request")
	}
	policyConfigData := reqBuf.Bytes()

	if err = k.SetBinaryValue(regKeyDrvParameters, policyConfigData); err != nil {
		return errors.Wrapf(err, "failed to set %s", regKeyDrvParameters)
	}
	if err = k.SetDWordValue(regKeyCdpActive, 1); err != nil {
		return errors.Wrapf(err, "failed to set %s", regKeyCdpActive)
	}
	return nil
}

// RegReadDriverParameter 读取驱动的持久化参数
func RegReadDriverParameter() (req *DRVReqStart, err error) {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, cdpConfigRegPath, registry.READ)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open registry key")
	}
	defer k.Close()

	data, _, err := k.GetBinaryValue(regKeyDrvParameters)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s", regKeyDrvParameters)
	}

	req = new(DRVReqStart)
	err = struc.UnpackWithOptions(bytes.NewReader(data), req, &struc.Options{Order: binary.LittleEndian})
	return req, err
}

// RegDeleteDriverParameter 删除驱动的持久化参数
func RegDeleteDriverParameter(_ time.Duration) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, cdpConfigRegPath, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrapf(err, "failed to open registry key")
	}
	defer k.Close()

	if err = k.DeleteValue(regKeyCdpActive); err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil
		}
	}
	return nil
}
