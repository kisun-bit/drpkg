package volume

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// elioCreateOrDeleteMutex 全局快照锁.
var elioCreateOrDeleteMutex sync.Mutex

type ElioSnapshot struct {
	uniqueID           string
	device             string
	mountPath          string
	cacheDir           string
	cacheDirMountPoint string
	cacheDevice        string
	snapDevice         string
	ctlBin             string
	snapDevPrefix      string
	sysCfgSnapSizePath string
	minor              int
	cowFile            string
	handle             *os.File
}

func (el *ElioSnapshot) Close() error {
	elioCreateOrDeleteMutex.Lock()
	defer elioCreateOrDeleteMutex.Unlock()

	if el.handle != nil {
		if err := el.handle.Close(); err != nil {
			return errors.Wrapf(err, "close %s", el.snapDevice)
		}
		logger.Debugf("%s.Close closed `%s`", el.Repr(), el.snapDevice)
	}
	if el.minor != -1 {
		var err error
		for i := 0; i < 99; i++ {
			err = RemoveCowDeviceByMinor(el.ctlBin, el.minor)
			if err != nil {
				logger.Warnf("%s.Close retry remove after 5s", el.Repr())
				time.Sleep(5 * time.Second)
				continue
			}
			return nil
		}
		if err != nil {
			o, e := exec.Command("sh", "-c", fmt.Sprintf("lsof -p %v", os.Getpid())).Output()
			logger.Errorf("%s.Close exec `lsof`:\noutput=%v\nerr=%v", el.Repr(), string(o), e)
			return err
		}
	}
	return nil
}

func (el *ElioSnapshot) ReadAt(p []byte, off int64) (int, error) {
	return el.handle.ReadAt(p, off)
}

func (el *ElioSnapshot) Type() string {
	return DevElastioOrDatto
}

func (el *ElioSnapshot) StartOffset() int64 {
	return 0
}

func (el *ElioSnapshot) EndOffset() int64 {
	return 0
}

func (el *ElioSnapshot) SnapshotPath() string {
	return el.snapDevice
}

func (el *ElioSnapshot) UniqueID() string {
	return el.uniqueID
}

func (el *ElioSnapshot) Create() error {
	elioCreateOrDeleteMutex.Lock()
	defer elioCreateOrDeleteMutex.Unlock()

	if el.snapDevice != "" {
		logger.Debugf("snapshot devic is not empty, ignore to create")
		return nil
	}
	// 获取空闲的minor号.
	getMinorCmdString := fmt.Sprintf("%s get-free-minor", el.ctlBin)
	o, e := exec.Command("sh", "-c", getMinorCmdString).Output()
	if e != nil {
		logger.Debugf("NewElioSnapshot for %s(mounted: %s), failed to get minor: %v",
			el.device, el.mountPath, e)
		return errors.New("no free minor for snapshot")
	}
	minor := strings.TrimSpace(string(o))
	minorInt, err := strconv.Atoi(minor)
	if err != nil {
		return errors.Wrapf(err, "invalid minor %s", minor)
	}
	// 生成cow文件路径.
	cowFile := el.cowFile
	if _, err = os.Stat(cowFile); err == nil {
		if err = os.Remove(cowFile); err != nil {
			// 更改其不可变属性.
			chImmuCmdString := fmt.Sprintf("chattr -i %s", cowFile)
			_, e = exec.Command("sh", "-c", chImmuCmdString).Output()
			if e != nil {
				return errors.Errorf("can not change immuaty for %s: %v", cowFile, e)
			}
			if err = os.Remove(cowFile); err != nil {
				return errors.Errorf("can not delete %s", cowFile)
			}
		}
	}
	newSnapshotCmdString := fmt.Sprintf("%s setup-snapshot %s %s %v",
		el.ctlBin, el.device, cowFile, minorInt)
	o, e = exec.Command("sh", "-c", newSnapshotCmdString).Output()
	logger.Debugf("NewElioSnapshot exec `%s`:\noutput:%v\nerr:%v",
		newSnapshotCmdString, string(o), e)
	if e != nil {
		return errors.Errorf("failed to create snapshot for %s", el.device)
	}

	el.minor = minorInt
	el.snapDevice = fmt.Sprintf("/dev/%s%v", el.snapDevPrefix, minorInt)
	el.sysCfgSnapSizePath = filepath.Join(ioctl.SysClassBlock, fmt.Sprintf("%s%v", el.snapDevPrefix, minorInt), "size")
	if _, err = os.Stat(el.sysCfgSnapSizePath); os.IsNotExist(err) {
		return errors.Errorf("%s not found", el.snapDevice)
	}
	el.handle, err = os.Open(el.snapDevice)
	return err
}

func (el *ElioSnapshot) DevicePath() string {
	return el.device
}

func (el *ElioSnapshot) Repr() string {
	return fmt.Sprintf("ElioDattoSnap(snap=%s,dev=%s)", el.snapDevice, el.device)
}

func (el *ElioSnapshot) CowFiles() []string {
	relCowFile, _ := filepath.Rel(el.cacheDirMountPoint, el.cowFile)
	return []string{relCowFile}
}

func (el *ElioSnapshot) CowFilesDev() string {
	if el.cacheDevice != "" && el.cacheDevice != el.device {
		return el.cacheDevice
	}
	return el.device
}

// NewElioSnapshot 创建一个datto/elastio-snap快照设备.
func NewElioSnapshot(deviceUniqueID, device, mountPath, cowCacheDir, cowCacheMountPoint, cowDevice, snapDriver string, lazyCreate bool) (el Reader, err error) {
	if mountPath == "" {
		return nil, errors.Errorf("create unsuccessfully snapshot for %s because mountpoint is empty", device)
	}
	if cowCacheDir == "" {
		return nil, errors.Errorf("create unsuccessfully snapshot for %s because cache-dir is empty", device)
	}
	ctlBinary, snapPreix, err := getCtlBinAndSnapDevPrefix(snapDriver)
	if err != nil {
		return nil, err
	}
	// 检测udev是否生成快照设备.
	elObj := new(ElioSnapshot)
	elObj.uniqueID = deviceUniqueID
	elObj.minor = -1
	elObj.device = device
	elObj.mountPath = mountPath
	elObj.cacheDir = cowCacheDir
	elObj.cacheDirMountPoint = cowCacheMountPoint
	elObj.cacheDevice = cowDevice
	elObj.ctlBin = ctlBinary
	elObj.cowFile = filepath.Join(elObj.cacheDir, fmt.Sprintf(".vol.%s.cow", strings.ReplaceAll(uuid.New().String(), "-", "")))
	elObj.snapDevPrefix = snapPreix
	if lazyCreate {
		return elObj, nil
	}
	err = elObj.Create()
	return elObj, err
}

// RemoveAllCowDevices 移除所有的满足/dev/datto*和/dev/elastio-snap*路径格式的设备.
func RemoveAllCowDevices(snapDriver string) error {
	switch runtime.GOOS {
	case "windows":
		return nil
	}
	files, err := os.ReadDir(ioctl.SysClassBlock)
	if err != nil {
		return errors.Wrap(err, "remove all cow-snap devices")
	}
	ctl, snapPrefix, err := getCtlBinAndSnapDevPrefix(snapDriver)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		minorStr := ""
		if strings.HasPrefix(f.Name(), snapPrefix) {
			minorStr = strings.TrimPrefix(f.Name(), snapPrefix)
			logger.Debugf("RemoveAllCowDevices find device %s, minor=`%s`", f.Name(), minorStr)
		}
		if minorStr == "" {
			continue
		}
		minorInt, e := strconv.Atoi(minorStr)
		if e != nil {
			return errors.Wrap(e, "convert minor")
		}
		if err = RemoveCowDeviceByMinor(ctl, minorInt); err != nil {
			return err
		}
		logger.Debugf("RemoveAllCowDevices delete device %s, minor=`%v`", f.Name(), minorInt)
	}
	return nil
}

// RemoveCowDeviceByMinor 删除一个Linux卷快照(elio或datto), 但是此过程必须加上重试,
// 以避免清理失败的情况.
func RemoveCowDeviceByMinor(ctl string, minor int) error {
	destroyCmdString := fmt.Sprintf("%s destroy %v", ctl, minor)
	o, e := exec.Command("sh", "-c", destroyCmdString).Output()
	logger.Debugf("RemoveCowDeviceByMinor exec `%s`:\noutput:%v\nerr:%v",
		destroyCmdString, string(o), e)
	if e != nil {
		return errors.Errorf("failed to recycle snapshot device which minor is %v", minor)
	}
	return nil
}

func getCtlBinAndSnapDevPrefix(snapDriver string) (string, string, error) {
	switch snapDriver {
	case SnapDriverElastioSnap:
		return "elioctl", "elastio-snap", nil
	case SnapDriverDattobd:
		return "dbdctl", "datto", nil
	default:
		return "", "", errors.Errorf("unsupported snapshot driver named %s", snapDriver)
	}
}

func IsElioOrDattoCowFile(path string) bool {
	baseName := filepath.Base(path)
	return strings.HasPrefix(baseName, ".vol") && strings.HasSuffix(baseName, ".cow")
}
