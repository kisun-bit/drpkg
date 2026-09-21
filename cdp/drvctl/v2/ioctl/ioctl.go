// Package ioctl 实现与 biotrk 驱动通信的 IOCTL 协议层。
//
// 本包提供以下功能分组：
//   - 任务管理：启动、释放、状态查询、一致性标记设置、错误字符串获取。
//   - 受保护设备管理：列举、增加、移除受保护设备。
//   - 位图管理：获取位图详情、获取位图数据、清理位图引用。
//   - 共享内存管理：创建、删除共享内存。
//   - 日志管理：设置日志事件、获取日志。
//
// 平台差异（设备路径、CTL_CODE 宏、IOC 宏）被隔离在 define_windows.go 与
// define_linux.go 中；IOCTL 系统调用封装在 ioctl_windows.go 与 ioctl_linux.go 中。
// 本文件不含任何平台条件编译或全局变量。
package ioctl

import (
	"bytes"
	"encoding/binary"

	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

// maxResponseBuffer 是可变长度响应（如 ListProtectedDevices）的输出缓冲区上限。
//
// 在 Windows 平台上，DeviceIoControl 需要的输出缓冲区大小应足够容纳预期的最大响应。
// 若实际响应超出此值，DeviceIoControl 会返回 ERROR_MORE_DATA。
const maxResponseBuffer = 65536

// pack 使用 struc 将 v 按 LittleEndian 编码为二进制。
//
// RemoveProtectedDevicesRequest 因其含 []biotrkmeta.ID（命名 [512]byte 数组切片，
// struc 无法编码），走专用的手工编码。
func pack(v interface{}) ([]byte, error) {
	if req, ok := v.(*RemoveProtectedDevicesRequest); ok {
		return packRemoveProtectedDevicesRequest(req)
	}

	buf := new(bytes.Buffer)
	if err := struc.PackWithOptions(buf, v, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, errors.Wrapf(err, "struc pack")
	}
	return buf.Bytes(), nil
}

// unpack 使用 struc 将 LittleEndian 二进制数据解码到 v 中。
func unpack(data []byte, v interface{}) error {
	if req, ok := v.(*RemoveProtectedDevicesRequest); ok {
		return unpackRemoveProtectedDevicesRequest(data, req)
	}

	return struc.UnpackWithOptions(bytes.NewReader(data), v, &struc.Options{Order: binary.LittleEndian})
}

// packRemoveProtectedDevicesRequest 手工编码 RemoveProtectedDevicesRequest。
//
// 线缆格式：DiskIDsLen + DiskIDs[] + DeviceIDsLen + DeviceIDs[] + NewMetadataExtentsLen + NewMetadataExtents[]，
// 其中长度字段均为元素个数，ID 每个 512 字节，DiskExtent 每个 536 字节。
func packRemoveProtectedDevicesRequest(req *RemoveProtectedDevicesRequest) ([]byte, error) {
	buf := new(bytes.Buffer)
	le := binary.LittleEndian

	writeID := func(id biotrkmeta.ID) error {
		return binary.Write(buf, le, id)
	}
	writeExtent := func(e *biotrkmeta.DiskExtent) error {
		if err := binary.Write(buf, le, e.DiskID.ID); err != nil {
			return err
		}
		if err := binary.Write(buf, le, e.DiskID.Major); err != nil {
			return err
		}
		if err := binary.Write(buf, le, e.DiskID.Minor); err != nil {
			return err
		}
		if err := binary.Write(buf, le, e.Start); err != nil {
			return err
		}
		return binary.Write(buf, le, e.Size)
	}

	if err := binary.Write(buf, le, uint32(len(req.DiskIDs))); err != nil {
		return nil, err
	}
	for i := range req.DiskIDs {
		if err := writeID(req.DiskIDs[i]); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(buf, le, uint32(len(req.DeviceIDs))); err != nil {
		return nil, err
	}
	for i := range req.DeviceIDs {
		if err := writeID(req.DeviceIDs[i]); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(buf, le, uint32(len(req.NewMetadataExtents))); err != nil {
		return nil, err
	}
	for i := range req.NewMetadataExtents {
		if err := writeExtent(&req.NewMetadataExtents[i]); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// unpackRemoveProtectedDevicesRequest 手工解码 RemoveProtectedDevicesRequest。
func unpackRemoveProtectedDevicesRequest(data []byte, req *RemoveProtectedDevicesRequest) error {
	r := bytes.NewReader(data)
	le := binary.LittleEndian

	readCount := func() (uint32, error) {
		var n uint32
		if err := binary.Read(r, le, &n); err != nil {
			return 0, err
		}
		return n, nil
	}
	readID := func(id *biotrkmeta.ID) error {
		return binary.Read(r, le, id)
	}
	readExtent := func(e *biotrkmeta.DiskExtent) error {
		if err := binary.Read(r, le, &e.DiskID.ID); err != nil {
			return err
		}
		if err := binary.Read(r, le, &e.DiskID.Major); err != nil {
			return err
		}
		if err := binary.Read(r, le, &e.DiskID.Minor); err != nil {
			return err
		}
		if err := binary.Read(r, le, &e.Start); err != nil {
			return err
		}
		return binary.Read(r, le, &e.Size)
	}

	n, err := readCount()
	if err != nil {
		return err
	}
	req.DiskIDs = make([]biotrkmeta.ID, n)
	for i := range req.DiskIDs {
		if err := readID(&req.DiskIDs[i]); err != nil {
			return err
		}
	}

	n, err = readCount()
	if err != nil {
		return err
	}
	req.DeviceIDs = make([]biotrkmeta.ID, n)
	for i := range req.DeviceIDs {
		if err := readID(&req.DeviceIDs[i]); err != nil {
			return err
		}
	}

	n, err = readCount()
	if err != nil {
		return err
	}
	req.NewMetadataExtents = make([]biotrkmeta.DiskExtent, n)
	for i := range req.NewMetadataExtents {
		if err := readExtent(&req.NewMetadataExtents[i]); err != nil {
			return err
		}
	}

	return nil
}

// sizeof 返回 v 的 struc 二进制编码大小。
//
// 对于含 sizeof 标签的可变长度字段（如 BitmapData.Data），其大小取决于对应的长度字段
// 当前值。例如 &BitmapData{Length: 0} 返回的 size 不包含 Data 内容。
func sizeof(v interface{}) int {
	n, err := struc.Sizeof(v)
	if err != nil {
		return 0
	}
	return n
}

// ===========================================================================
// 任务管理
// ===========================================================================

// StartTask 向驱动发送启动任务请求。
//
// 工作流程：
// s1. 用户层创建biotrkmeta文件并组装StartTaskRequest
// s2. 用户层通知驱动发起CDP备份
// s3. 驱动基于MetadataExtents的物理分布区间：完整加载保存各个磁盘的位图物理映射分布信息、受保护设备的保护区域信息
// s4. 驱动基于磁盘大小初始化位图数据
// s5. 驱动基于受保护设备的保护区域信息，开始hook数据，并持续更新位图及引用
// s6. 驱动返回成功
func StartTask(req *StartTaskRequest) error {
	inBuf, err := pack(req)
	if err != nil {
		return err
	}
	_, err = doIoctl(IOCTL_BIOTRK_START_TASK, inBuf, 0)
	if err != nil {
		return err
	}
	return PersistStartRequest(req)
}

// ReleaseTask 向驱动发送释放并删除任务请求。
//
// 工作流程：
// s2. 用户层通知驱动结束CDP备份
// s3. 驱动删除各个磁盘的位图物理映射分布信息、受保护设备的保护区域信息、各个磁盘的位图数据、shm共享内存、结束所有磁盘的hook
// s6. 驱动返回成功
func ReleaseTask() error {
	_, err := doIoctl(IOCTL_BIOTRK_RELEASE_TASK, nil, 0)
	if err != nil {
		return err
	}
	if err = DeleteShm(); err != nil {
		return err
	}
	return RemovePersist()
}

// GetTaskStatus 查询当前 CDP 任务状态。
//
// 返回值中的 Status 字段指示空闲、实时备份、定时备份或异常状态。
// 当 Status 为 biotrkmeta.CDPStatusError 时，ErrorCode 字段包含具体错误码。
func GetTaskStatus() (*TaskStatus, error) {
	resp := &TaskStatus{}
	inBuf, err := pack(resp)
	if err != nil {
		return nil, err
	}
	outBuf, err := doIoctl(IOCTL_BIOTRK_GET_TASK_STATUS, inBuf, sizeof(resp))
	if err != nil {
		return nil, err
	}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SetTaskConsistency 设置任务一致性标记，将任务切换为 CDP 模式（实时模式）。
//
// 工作流程：
// s2. 用户层通知驱动启用CDP模式（注意必须已创建SHM）
// s3. 驱动随即在预处理阶段为所有IO打入实时IO的标记，这些IO将写入SHM
// s6. 驱动返回成功
func SetTaskConsistency() error {
	_, err := doIoctl(IOCTL_BIOTRK_SET_TASK_CONSISTENCY, nil, 0)
	return err
}

// GetTaskErrorString 根据错误码获取对应的错误描述字符串。
func GetTaskErrorString(errCode uint64) (*TaskErrorString, error) {
	req := &TaskErrorString{ErrCode: errCode}
	inBuf, err := pack(req)
	if err != nil {
		return nil, err
	}
	outBuf, err := doIoctl(IOCTL_BIOTRK_GET_TASK_ERROR_STRING, inBuf, sizeof(req))
	if err != nil {
		return nil, err
	}
	resp := &TaskErrorString{}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ReadErrorString 根据错误码获取对应的错误描述字符串。
//
// 这是 GetTaskErrorString 的便捷封装。
func ReadErrorString(errCode uint64) (string, error) {
	resp, err := GetTaskErrorString(errCode)
	if err != nil {
		return "", err
	}
	return xutil.TrimZeroString(resp.ErrStr[:]), nil
}

// ===========================================================================
// 受保护设备管理
// ===========================================================================

// GetProtectedDevices 从驱动内存中查询当前所有受保护设备。
//
// 返回的列表与驱动内存中缓存的 ProtectedDevice 一致。
// 若设备数量较多超出 maxResponseBuffer，Windows 上返回 ERROR_MORE_DATA。
func GetProtectedDevices() (*ListProtectedDevices, error) {
	outBuf, err := doIoctl(IOCTL_BIOTRK_LIST_PROTECTED_DEVICE, nil, maxResponseBuffer)
	if err != nil {
		return nil, err
	}
	resp := &ListProtectedDevices{}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// AddProtectedDevices 向驱动发送批量增加受保护设备请求。
//
// 工作流程：
// s1. 应用层调用biotrkmeta的AddProtectedDevice方法并调用Flush，获取最新的元数据文件的物理分布信息
// s2. 将AddProtectedDevicesRequest发送给驱动
// s3. 驱动更新维护在内存中的磁盘位图的物理分布区域信息，用于关机阶段刷盘，注意驱动不用更新维护在内存中的位图数据，因为他总是连续的，只是刷盘时，根据最新的磁盘位图的物理分布区域信息进行刷盘即可。
// s4. 驱动更新维护在内存中的受保护设备信息，用于后续CDP工作时，进行IO的hook
// s5. 驱动成功更新磁盘位图的物理分布区域信息和受保护设备信息，向用户层返回成功
// s6. 用户层基于AddProtectedDevicesRequest组装新的StartTaskRequest，并写入注册表或Initramfs
//
// 注意：
//   - 不允许添加相同 ID 的设备。
//   - 任意一个添加失败则整体报错。
func AddProtectedDevices(req *AddProtectedDevicesRequest) error {
	inBuf, err := pack(req)
	if err != nil {
		return err
	}
	_, err = doIoctl(IOCTL_BIOTRK_ADD_PROTECTED_DEVICE, inBuf, 0)
	if err != nil {
		return err
	}

	// 从持久化读取原始 StartTaskRequest，替换为新的 MetadataExtents 后写回
	persistReq, err := ReadPersistRequest()
	if err != nil {
		return errors.Wrapf(err, "read persist request")
	}
	persistReq.MetadataExtents = req.NewMetadataExtents
	persistReq.MetadataExtentsLen = uint32(len(req.NewMetadataExtents))

	return PersistStartRequest(persistReq)
}

// RemoveProtectedDevices 向驱动发送批量移除受保护设备请求。
//
// 工作流程：
// s1. 应用层调用biotrkmeta的RemoveProtectedDevice方法并调用Flush，获取最新的元数据文件的物理分布信息
// s2. 将RemoveProtectedDevicesRequest发送给驱动
// s3. 驱动基于DiskIDs删除在内存中的磁盘位图的物理分布区域信息。
// s4. 驱动更新维护在内存中的受保护设备信息，用于后续CDP工作时，进行IO的hook
// s5. 驱动成功更新磁盘位图的物理分布区域信息和受保护设备信息，向用户层返回成功
// s6. 用户层基于RemoveProtectedDevicesRequest组装新的StartTaskRequest，并写入注册表或Initramfs
//
// 注意：
//   - 不允许全部删除。
//   - 任意一个删除失败则整体报错，不存在的设备除外。
func RemoveProtectedDevices(req *RemoveProtectedDevicesRequest) error {
	inBuf, err := pack(req)
	if err != nil {
		return err
	}
	_, err = doIoctl(IOCTL_BIOTRK_REMOVE_PROTECTED_DEVICE, inBuf, 0)
	if err != nil {
		return err
	}

	// 从持久化读取原始 StartTaskRequest，替换为新的 MetadataExtents 后写回
	persistReq, err := ReadPersistRequest()
	if err != nil {
		return errors.Wrapf(err, "read persist request")
	}
	persistReq.MetadataExtents = req.NewMetadataExtents
	persistReq.MetadataExtentsLen = uint32(len(req.NewMetadataExtents))

	return PersistStartRequest(persistReq)
}

// ===========================================================================
// 位图管理
// ===========================================================================

// GetBitmapDetail 获取指定磁盘的位图详情，返回位图大小。
func GetBitmapDetail(diskID biotrkmeta.DiskID) (*BitmapDetail, error) {
	req := &BitmapDetail{DiskID: diskID}
	inBuf, err := pack(req)
	if err != nil {
		return nil, err
	}
	outBuf, err := doIoctl(IOCTL_BIOTRK_GET_BITMAP_DETAIL, inBuf, sizeof(req))
	if err != nil {
		return nil, err
	}
	resp := &BitmapDetail{}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetBitmapData 获取指定磁盘区域的位图数据。
//
// req 中的 DiskID、Offset、Length 必须填写。返回时 DataSize 和 Data 被填充。
// Length 指定期望获取的位图数据长度（字节）。
//
// 响应缓冲区大小 = 结构体固定大小 + Length。
// Linux 平台上，输入缓冲区会被自动扩展到能容纳响应数据。
// Windows 平台上，若实际返回数据超出输出缓冲区，DeviceIoControl 返回错误。
func GetBitmapData(req *BitmapData) (*BitmapData, error) {
	inBuf, err := pack(req)
	if err != nil {
		return nil, err
	}

	outSize := sizeof(req) + int(req.Length)

	outBuf, err := doIoctl(IOCTL_BIOTRK_GET_BITMAP_DATA, inBuf, outSize)
	if err != nil {
		return nil, err
	}
	resp := &BitmapData{}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ClearBitmapReference 向驱动发送清理位图引用请求。
//
// RecordType 区分实时 I/O 产生的位图记录还是磁盘位图 I/O 产生的记录。
// Segments 指定需要清理引用的磁盘区域。
func ClearBitmapReference(req *ClearBitmapReferenceRequest) error {
	inBuf, err := pack(req)
	if err != nil {
		return err
	}
	_, err = doIoctl(IOCTL_BIOTRK_CLEAR_BITMAP_REFERENCE, inBuf, 0)
	return err
}

// ===========================================================================
// 共享内存管理
// ===========================================================================

// CreateShm 向驱动请求创建共享内存。
//
// size 为共享内存大小（字节），event 为用户层创建的读写事件对象句柄。
// 返回值包含驱动映射的共享内存地址。
func CreateShm(size uint32, event uint64) (*ShmConfig, error) {
	req := &ShmConfig{Size: size, Event: event}
	inBuf, err := pack(req)
	if err != nil {
		return nil, err
	}
	outBuf, err := doIoctl(IOCTL_BIOTRK_CREATE_SHM, inBuf, sizeof(req))
	if err != nil {
		return nil, err
	}
	resp := &ShmConfig{}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteShm 向驱动请求删除共享内存。
//
// 删除后若存在 CDP 任务，驱动会自动将任务转为 CBT 模式（位图模式）。
func DeleteShm() error {
	_, err := doIoctl(IOCTL_BIOTRK_DELETE_SHM, nil, 0)
	return err
}

// ===========================================================================
// 日志管理
// ===========================================================================

// SetLogEvent 向驱动设置日志读写事件句柄。
//
// event 为共享日志缓冲区的事件对象句柄（Windows）或 eventfd（Linux）。
func SetLogEvent(event uint64) error {
	req := &LogEventSetRequest{Event: event}
	inBuf, err := pack(req)
	if err != nil {
		return err
	}
	_, err = doIoctl(IOCTL_BIOTRK_SET_LOG_EVENT, inBuf, 0)
	return err
}

// GetLog 从驱动获取一条日志条目。
//
// 返回的 LogEntry 包含日志级别、时间戳和最多 512 字节的日志内容。
func GetLog() (*LogEntry, error) {
	resp := &LogEntry{}
	inBuf, err := pack(resp)
	if err != nil {
		return nil, err
	}
	outBuf, err := doIoctl(IOCTL_BIOTRK_GET_LOG, inBuf, sizeof(resp))
	if err != nil {
		return nil, err
	}
	if err := unpack(outBuf, resp); err != nil {
		return nil, err
	}
	return resp, nil
}
