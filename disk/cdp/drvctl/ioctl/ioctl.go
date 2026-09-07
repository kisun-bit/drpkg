package ioctl

import (
	"bytes"
	"encoding/binary"
	"runtime"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

func ReqDrvStart(request *DRVReqStart) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_START, packedReq.Bytes(), 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvAdd(request *DRVReqAdd) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_ADD, packedReq.Bytes(), 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvReduce(request *DRVReqReduce) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_REDUCE, packedReq.Bytes(), 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvDelete() error {
	var buf [4]byte
	_, err := ReqDrvIoctl(BIOTRK_CMD_DELETE, buf[:], 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvSuspend() error {
	var buf [4]byte

	_, err := ReqDrvIoctl(BIOTRK_CMD_SUSPEND, buf[:], 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvRealtime() error {
	var buf [4]byte

	_, err := ReqDrvIoctl(BIOTRK_CMD_CONSISTENT, buf[:], 0)
	if err != nil {
		return err
	}

	return nil
}

func ReqDrvGetStatus(request *DRVReqGetStatus) (*DRVRespGetStatus, error) {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	reply, err := ReqDrvIoctl(BIOTRK_CMD_STATUS, packedReq.Bytes(), MsgBuffersize)

	if err != nil {
		return nil, errors.Wrapf(err, "ReqDrvIoctl")
	}

	// linux没有outputBuffer，因此，这里将返回的数据解包到请求结构体中
	var resp DRVRespGetStatus
	if err := struc.UnpackWithOptions(bytes.NewBuffer(reply), &resp, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	return &resp, nil
}

func ReqDrvClearBitmap(request *DRVReqClearBitmap) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_CLEAR_BITMAP, packedReq.Bytes(), 0)
	if err != nil {
		return err
	}

	return nil
}

func ReqDrvSelectBitmap(request *DRVReqSelectBitmap) (*DRVRespSelectBitmap, error) {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	outBufferSize := uint64(0)
	if runtime.GOOS == "windows" {
		respSize, err := struc.Sizeof(&DRVReqSelectBitmap{})
		if err != nil {
			return nil, err
		}
		outBufferSize = uint64(respSize)
	}
	reply, err := ReqDrvIoctl(BIOTRK_CMD_SELECT_BITMAP, packedReq.Bytes(), outBufferSize)

	if err != nil {
		return nil, errors.Wrapf(err, "ReqDrvIoctl")
	}

	// linux没有outputBuffer，因此，这里将返回的数据解包到请求结构体中
	var resp DRVRespSelectBitmap
	if err = struc.UnpackWithOptions(bytes.NewBuffer(reply), &resp, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	return &resp, nil
}

func DrvReqGetBitmap(request *DRVReqGetBitmap) (*DRVRespGetBitmap, error) {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	outBufferSize := uint64(0)
	if runtime.GOOS == "windows" {
		respSize, err := struc.Sizeof(&DRVReqGetBitmap{})
		if err != nil {
			return nil, err
		}
		outBufferSize = uint64(respSize) + request.GetBitmapLen
	}

	reply, err := ReqDrvIoctl(BIOTRK_CMD_GET_BITMAP, packedReq.Bytes(), outBufferSize)

	if err != nil {
		return nil, errors.Wrapf(err, "ReqDrvIoctl")
	}

	// linux没有outputBuffer，因此，这里将返回的数据解包到请求结构体中
	var resp DRVRespGetBitmap
	if err := struc.UnpackWithOptions(bytes.NewBuffer(reply), &resp, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	return &resp, nil
}

func ReqDrvCreateRingBuffer(request *DRVReqCreateRingBuffer) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	outBufferSize := uint64(0)
	if runtime.GOOS == "windows" {
		respSize, err := struc.Sizeof(&DRVReqCreateRingBuffer{})
		if err != nil {
			return err
		}
		outBufferSize = uint64(respSize)
	}

	outBuffer, err := ReqDrvIoctl(BIOTRK_CMD_CREATE_RINGBUFFER, packedReq.Bytes(), outBufferSize)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	if runtime.GOOS == "windows" {
		request.SHMAddress = binary.LittleEndian.Uint64(outBuffer[:8])
	}

	return nil
}

func ReqDrvDeleteRingBuffer() error {
	var buf [4]byte

	_, err := ReqDrvIoctl(BIOTRK_CMD_DELETE_RINGBUFFER, buf[:], 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}

	return nil
}

func ReqDrvSetLogEvent(request *DRVReqSetLogEvent) error {
	var packedReq bytes.Buffer
	if err := struc.PackWithOptions(&packedReq, request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return err
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_SET_LOG_EVENT, packedReq.Bytes(), 0)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvIoctl")
	}
	return nil
}

func ReqDrvGetLog(request *DRVReqGetLog) (*DRVRespGetLog, error) {
	var packedReq bytes.Buffer
	if err := struc.PackWithOptions(&packedReq, request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	outBufferSize := uint64(0)
	if runtime.GOOS == "windows" {
		respSize, err := struc.Sizeof(&DRVRespGetLog{})
		if err != nil {
			return nil, err
		}
		outBufferSize = uint64(respSize)
	}

	reply, err := ReqDrvIoctl(BIOTRK_CMD_GET_LOG, packedReq.Bytes(), outBufferSize)

	if err != nil {
		return nil, errors.Wrapf(err, "ReqDrvIoctl")
	}

	// linux没有outputBuffer，因此，这里将返回的数据解包到请求结构体中
	var resp DRVRespGetLog
	// hex.Dump(reply)
	if err = struc.UnpackWithOptions(bytes.NewBuffer(reply), &resp, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	return &resp, nil
}

func ReqDrvGetErrStr(request *DRVReqGetErrStr) (*DRVRespGetErrStr, error) {
	var packedReq bytes.Buffer
	if err := struc.PackWithOptions(&packedReq, request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	outBufferSize := uint64(0)
	if runtime.GOOS == "windows" {
		respSize, err := struc.Sizeof(&DRVRespGetErrStr{})
		if err != nil {
			return nil, err
		}
		outBufferSize = uint64(respSize)
	}

	reply, err := ReqDrvIoctl(BIOTRK_CMD_GET_ERR_STR, packedReq.Bytes(), outBufferSize)

	if err != nil {
		return nil, errors.Wrapf(err, "ReqDrvIoctl")
	}

	// linux没有outputBuffer，因此，这里将返回的数据解包到请求结构体中
	var resp DRVRespGetErrStr
	if err = struc.UnpackWithOptions(bytes.NewBuffer(reply), &resp, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, err
	}

	return &resp, nil
}

// //////////////////////////////////
// 测试ioctl
func ReqDrvSelectAllBits(request *DRVReqSelect) error {
	packedReq := bytes.NewBuffer(nil)
	if err := struc.PackWithOptions(packedReq, &request, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return errors.Wrapf(err, "PackWithOptions")
	}

	_, err := ReqDrvIoctl(BIOTRK_CMD_SELECT_ALL_BITS, packedReq.Bytes(), 0)
	if err != nil {
		return err
	}

	return nil
}

func ReqDrvSelectAllMem() error {
	var buf [4]byte

	_, err := ReqDrvIoctl(BIOTRK_CMD_SELECT_ALL_MEM, buf[:], 0)
	if err != nil {
		return err
	}

	return nil
}
