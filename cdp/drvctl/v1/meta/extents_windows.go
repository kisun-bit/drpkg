package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

type Extent struct {
	NextVCN uint64
	LCN     uint64
}

type RetrievalPointersBuffer struct {
	ExtentCount uint32
	Padding     uint32
	StartingVCN uint64
	Extents     []Extent
}

func QueryFileExtentsOnVolume(file string, clusterSize int) (r RetrievalPointersBuffer, err error) {
	fd, err := os.Open(file)
	if err != nil {
		return r, err
	}
	defer fd.Close()

	stat, err := fd.Stat()
	if err != nil {
		return r, err
	}

	if stat.Size() < int64(clusterSize) {
		return r, errors.Errorf("file %s is too small (%vB)", file, stat.Size())
	}

	// 返回值定义：
	//  typedef struct RETRIEVAL_POINTERS_BUFFER {
	//    DWORD                    ExtentCount;
	//    LARGE_INTEGER            StartingVcn;
	//    struct {
	//      LARGE_INTEGER NextVcn;
	//      LARGE_INTEGER Lcn;
	//    };
	//    __unnamed_struct_195e_66 Extents[1];
	//  } RETRIEVAL_POINTERS_BUFFER, *PRETRIEVAL_POINTERS_BUFFER;

	// 计算最大有多少个extent.
	maxExtents := int(stat.Size() / int64(clusterSize))

	// 构造输出缓冲区.
	bufferSize := int(unsafe.Sizeof(uint32(0))) + // ExtentCount
		int(unsafe.Sizeof(uint32(0))) + // Padding
		int(unsafe.Sizeof(uint64(0))) + // StartingVCN
		maxExtents*int(unsafe.Sizeof(Extent{}))

	var startingVCN uint64 = 0
	var bytesReturned uint32

	outBuf := make([]byte, bufferSize)

	err = windows.DeviceIoControl(
		windows.Handle(fd.Fd()),
		windows.FSCTL_GET_RETRIEVAL_POINTERS,
		(*byte)(unsafe.Pointer(&startingVCN)),
		uint32(unsafe.Sizeof(startingVCN)),
		&outBuf[0],
		uint32(len(outBuf)),
		&bytesReturned,
		nil,
	)
	if err != nil {
		return r, err
	}
	if bytesReturned == 0 {
		return r, errors.Errorf("no bytes returned")
	}

	// 解析结构体字段
	r.ExtentCount = *(*uint32)(unsafe.Pointer(&outBuf[0]))
	r.StartingVCN = *(*uint64)(unsafe.Pointer(&outBuf[8])) // 4字节padding后是StartingVCN

	r.Extents = make([]Extent, r.ExtentCount)
	baseOffset := 16 // Extents从offset16开始
	for i := 0; i < int(r.ExtentCount); i++ {
		offset := baseOffset + i*int(unsafe.Sizeof(Extent{}))
		extent := (*Extent)(unsafe.Pointer(&outBuf[offset]))
		r.Extents[i] = *extent
	}

	return r, nil
}

func FileDiskExtents(file string) (es []FileDiskExtentSegment, err error) {
	stat, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return nil, errors.Errorf("%s is a directory", file)
	}
	fileSize := stat.Size()

	volName := filepath.VolumeName(file)
	if volName == "" || !strings.HasSuffix(volName, ":") {
		return nil, errors.Errorf("volume name of %s is invalid", file)
	}
	volUncPath := fmt.Sprintf("\\\\.\\%s", volName)
	volPath := fmt.Sprintf("%s\\", volName)

	volExtentsOnDisk, err := VolumeMountpointToExtents(volUncPath)
	if err != nil {
		return nil, err
	}
	bytesPerCluster, err := GetClusterSize(volPath)
	if err != nil {
		return nil, err
	}
	fileExtentsInfo, err := QueryFileExtentsOnVolume(file, int(bytesPerCluster))
	if err != nil {
		return nil, err
	}
	if fileExtentsInfo.ExtentCount == 0 {
		return nil, errors.Errorf("file %s has no extents", file)
	}

	fileExtentsOnVolume := make([]volumeSegment, 0)
	vcn := fileExtentsInfo.StartingVCN
	for i, e := range fileExtentsInfo.Extents {
		if i > 0 {
			vcn = fileExtentsInfo.Extents[i-1].NextVCN
		}
		logicStart := int64(vcn) * bytesPerCluster
		segStart := int64(e.LCN) * bytesPerCluster
		segSize := int64(e.NextVCN-vcn) * bytesPerCluster
		if logicStart+segSize > fileSize {
			segSize = fileSize - logicStart
		}
		fileExtentsOnVolume = append(fileExtentsOnVolume, volumeSegment{
			start: segStart,
			size:  segSize,
		})
	}

	for _, fe := range fileExtentsOnVolume {
		extentSize := fe.size
		fileExtentVolStart := fe.start
		fileExtentVolEnd := fileExtentVolStart + extentSize
		diskDelta := fe.start

		diskVolStart := int64(0)
		for _, ve := range volExtentsOnDisk {
			diskVolEnd := diskVolStart + int64(ve.ExtentLength)
			diskDelta = fileExtentVolStart - diskVolStart

			if fileExtentVolStart < diskVolStart {
				return nil, errors.New("unexcepted range")
			} else if fileExtentVolStart >= diskVolStart && fileExtentVolEnd <= diskVolEnd {
				// 全包含
				es = append(es, FileDiskExtentSegment{
					Disk:  WindowsDiskPathFromID(ve.DiskNumber),
					Start: int64(ve.StartingOffset) + diskDelta,
					Size:  extentSize,
				})
				break
			} else if fileExtentVolStart < diskVolEnd && fileExtentVolEnd > diskVolEnd {
				// 部分包含，做截断处理
				deltaExtentSize := diskVolEnd - fileExtentVolStart
				es = append(es, FileDiskExtentSegment{
					Disk:  WindowsDiskPathFromID(ve.DiskNumber),
					Start: int64(ve.StartingOffset) + diskDelta,
					Size:  deltaExtentSize,
				})
				extentSize -= deltaExtentSize
				fileExtentVolStart += deltaExtentSize
			}

			diskVolStart = diskVolEnd
		}
	}

	if len(es) == 0 {
		return nil, errors.New("failed to calculate extents")
	}
	return es, nil
}
