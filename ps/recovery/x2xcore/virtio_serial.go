package x2xcore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

const (
	RequestVirtioPortName = "drserial.request"
	LogVirtioPortName     = "drserial.log"
)

// 定义修复虚拟机中 Host 与 Guest 之间的通信消息类型。
// 用于控制修复流程、传递执行日志以及同步执行结果。
const (
	// SerialMessageTypeGuestReady:
	// Guest 启动完成，通知 Host 当前修复环境已就绪。
	SerialMessageTypeGuestReady = iota

	// SerialMessageTypeStartRepair:
	// Host 通知 Guest 开始执行修复任务。
	SerialMessageTypeStartRepair

	// SerialMessageTypeRepairLog:
	// Guest 向 Host 返回修复过程日志。
	SerialMessageTypeRepairLog

	// SerialMessageTypeRepairResult:
	// Guest 向 Host 返回修复任务最终执行结果。
	SerialMessageTypeRepairResult
)

type SerialMessage struct {
	Type       int    // 消息类型：见 SerialMessageType* 定义
	BodyLength int    `struc:"sizeof=Body"` // 消息体长度
	Body       []byte // 消息体
}

type RepairResult struct {
	Success  bool
	ErrorMsg string
	Extra    map[string]string
}

func NewSerialMessageTypeGuestReady() *SerialMessage {
	return &SerialMessage{
		Type: SerialMessageTypeGuestReady,
	}
}

func NewSerialMessageTypeStartRepair(options FixerCreateOptions) *SerialMessage {
	data, _ := json.Marshal(options)
	return &SerialMessage{
		Type:       SerialMessageTypeStartRepair,
		BodyLength: len(data),
		Body:       data,
	}
}

func NewSerialMessageTypeRepairLog(logE LogEntry) *SerialMessage {
	data, _ := json.Marshal(logE)
	return &SerialMessage{
		Type:       SerialMessageTypeRepairLog,
		BodyLength: len(data),
		Body:       data,
	}
}

func NewSerialMessageTypeRepairResult(ret RepairResult) *SerialMessage {
	data, _ := json.Marshal(ret)
	return &SerialMessage{
		Type:       SerialMessageTypeRepairResult,
		BodyLength: len(data),
		Body:       data,
	}
}

func WriteSerialMessageTypeGuestReady(w io.Writer) error {
	logger.Debugf("WriteSerialMessageTypeGuestReady: ++")
	defer logger.Debugf("WriteSerialMessageTypeGuestReady: --")
	return struc.Pack(w, NewSerialMessageTypeGuestReady())
}

func WriteSerialMessageTypeStartRepair(w io.Writer, options FixerCreateOptions) error {
	logger.Debugf("WriteSerialMessageTypeStartRepair: ++")
	defer logger.Debugf("WriteSerialMessageTypeStartRepair: --")
	return struc.Pack(w, NewSerialMessageTypeStartRepair(options))
}

func WriteSerialMessageTypeRepairLog(w io.Writer, logE LogEntry) error {
	logger.Debugf("WriteSerialMessageTypeRepairLog: ++")
	defer logger.Debugf("WriteSerialMessageTypeRepairLog: --")
	return struc.Pack(w, NewSerialMessageTypeRepairLog(logE))
}

func WriteSerialMessageTypeRepairResult(w io.Writer, ret RepairResult) error {
	logger.Debugf("WriteSerialMessageTypeRepairResult: ++")
	defer logger.Debugf("WriteSerialMessageTypeRepairResult: --")
	return struc.Pack(w, NewSerialMessageTypeRepairResult(ret))
}

func ReadReceivedSerialMessageTypeGuestReady(r io.Reader) error {
	logger.Debugf("ReadReceivedSerialMessageTypeGuestReady: ++")
	defer logger.Debugf("ReadReceivedSerialMessageTypeGuestReady: --")
	sm := &SerialMessage{}
	if err := struc.Unpack(r, sm); err != nil {
		return err
	}
	if sm.Type != SerialMessageTypeGuestReady {
		return errors.New("serial message: expect SerialMessageTypeGuestReady")
	}
	return nil
}

func ReadReceivedSerialMessageTypeStartRepair(r io.Reader) (startRepair FixerCreateOptions, err error) {
	logger.Debugf("ReadReceivedSerialMessageTypeStartRepair: ++")
	defer logger.Debugf("ReadReceivedSerialMessageTypeStartRepair: --")
	sm := &SerialMessage{}
	if err = struc.Unpack(r, sm); err != nil {
		return FixerCreateOptions{}, err
	}
	if sm.Type != SerialMessageTypeStartRepair {
		return FixerCreateOptions{}, errors.New("serial message: expect SerialMessageTypeStartRepair")
	}
	if err = json.Unmarshal(sm.Body, &startRepair); err != nil {
		return FixerCreateOptions{}, err
	}
	return startRepair, nil
}

func ReadReceivedSerialMessageTypeRepairLog(r io.Reader) (logE LogEntry, err error) {
	logger.Debugf("ReadReceivedSerialMessageTypeRepairLog: ++")
	defer logger.Debugf("ReadReceivedSerialMessageTypeRepairLog: --")
	sm := &SerialMessage{}
	if err = struc.Unpack(r, sm); err != nil {
		return LogEntry{}, err
	}
	if sm.Type != SerialMessageTypeRepairLog {
		return LogEntry{}, errors.New("serial message: expect SerialMessageTypeRepairLog")
	}
	if err = json.Unmarshal(sm.Body, &logE); err != nil {
		return LogEntry{}, err
	}
	return logE, nil
}

func ReadReceivedSerialMessageTypeRepairResult(r io.Reader) (repairResult RepairResult, err error) {
	logger.Debugf("ReadReceivedSerialMessageTypeRepairResult: ++")
	defer logger.Debugf("ReadReceivedSerialMessageTypeRepairResult: --")
	sm := &SerialMessage{}
	if err = struc.Unpack(r, sm); err != nil {
		return RepairResult{}, err
	}
	if sm.Type != SerialMessageTypeRepairResult {
		return RepairResult{}, errors.New("serial message: expect SerialMessageTypeRepairResult")
	}
	if err = json.Unmarshal(sm.Body, &repairResult); err != nil {
		return RepairResult{}, err
	}
	return repairResult, nil
}

// FindVirtioPort 通过通道名称自动查找 virtio-serial 设备
// On Linux: returns /dev/vportNpM
// On Windows: returns \\.\Global\{name} (Named Pipe) or empty string if not found
func FindVirtioPort(name string) string {
	if runtime.GOOS == "windows" {
		return findVirtioPortWindows(name)
	}
	return findVirtioPortLinux(name)
}

//func findVirtioPortLinux(name string) string {
//	const portsDir = "/sys/class/virtio-ports/"
//	entries, err := os.ReadDir(portsDir)
//	if err != nil {
//		return ""
//	}
//	for _, entry := range entries {
//		nameFile := filepath.Join(portsDir, entry.Name(), "name")
//		data, err := os.ReadFile(nameFile)
//		if err != nil {
//			continue
//		}
//		if strings.TrimSpace(string(data)) == name {
//			return "/dev/" + entry.Name()
//		}
//	}
//	return ""
//}

func findVirtioPortLinux(name string) string {
	const portsDir = "/sys/class/virtio-ports/"

	logger.Debugf("FindVirtioPort: target name=%q", name)

	entries, err := os.ReadDir(portsDir)
	if err != nil {
		logger.Debugf("ReadDir %s failed: %v", portsDir, err)
		return ""
	}

	for _, entry := range entries {
		logger.Debugf("checking virtio port entry=%q", entry.Name())

		nameFile := filepath.Join(
			portsDir,
			entry.Name(),
			"name",
		)

		data, err := os.ReadFile(nameFile)
		if err != nil {
			logger.Debugf("ReadFile %s failed: %v", nameFile, err)
			continue
		}

		portName := strings.TrimSpace(string(data))

		logger.Debugf(
			"virtio port: entry=%q, portName=%q, target=%q, equal=%v",
			entry.Name(),
			portName,
			name,
			portName == name,
		)

		if portName == name {
			devPath := "/dev/" + entry.Name()

			logger.Debugf(
				"virtio port found: name=%q dev=%q",
				name,
				devPath,
			)

			return devPath
		}
	}

	logger.Debugf("virtio port not found: target=%q", name)

	return ""
}

func findVirtioPortWindows(name string) string {
	// VirtIO-WIN exposes serial ports as Named Pipes under \\.\Global\
	pipePath := fmt.Sprintf(`\\.\Global\%s`, name)

	// Verify the pipe actually exists before returning
	if _, err := os.Stat(pipePath); err == nil {
		return pipePath
	}

	// Fallback: try without Global prefix (older drivers)
	pipePathLocal := fmt.Sprintf(`\\.\%s`, name)
	if _, err := os.Stat(pipePathLocal); err == nil {
		return pipePathLocal
	}

	return ""
}
