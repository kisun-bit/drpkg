package journal

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
	"github.com/kisun-bit/drpkg/cdp/drvctl/v2/ioctl"
	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
	"github.com/kisun-bit/drpkg/disk/image/hkc"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// CdpJournalBuilder 用于构建 CdpJournal，对于同一任务同一IOMask类型的可重复复用。
//
// 使用流程：
//   1. 调用 NewCdpJournalBuilder 创建 Builder。
//   2. 多次调用 AddDiskRecord 或 AddSharedMemoryRecord 添加记录。
//   3. 调用 Build 生成一个 CdpJournal。
//
// Build 完成后，Builder 会自动重置内部状态，可继续添加记录并再次调用
// Build，无需重新创建 Builder。

type CdpJournal = proto.CdpJournal

type CdpJournalBuilder struct {
	mu sync.Mutex

	schedule string
	task     string
	mask     proto.IOMask

	logger            *zap.SugaredLogger
	sequenceGenerator SequenceGenerator

	realtime       bool
	enableChecksum bool
	enableCompress bool
	enableEncrypt  bool
	encryptionKey  string

	// 当前构建状态
	buf bytes.Buffer

	minSequence uint64
	maxSequence uint64

	minTimestamp uint64
	maxTimestamp uint64

	rawTotalBytes uint64

	packetComplete atomic.Bool
}

type CdpJournalOption struct {
	Schedule string
	Task     string
	Mask     proto.IOMask

	EnableChecksum bool
	EnableCompress bool
	EnableEncrypt  bool
	EncryptionKey  string

	Logger            *zap.SugaredLogger
	SequenceGenerator SequenceGenerator
}

// WriteJournalChecksum 为CDP日志数据包写入校验和（由原机调用）
func WriteJournalChecksum(cdpJournal *proto.CdpJournal) {
	if cdpJournal == nil {
		return
	}
	if IsFlagRecordByMask(cdpJournal.Mask) {
		return
	}
	cdpJournal.CdpRecordsChecksum = xxhash.Sum64(cdpJournal.CdpRecords)
	return
}

// ValidateJournalChecksum 校验CDP日志数据包的校验和（由介质侧调用）
func ValidateJournalChecksum(cdpJournal *proto.CdpJournal) error {
	if cdpJournal == nil {
		return nil
	}
	if IsFlagRecordByMask(cdpJournal.Mask) {
		return nil
	}
	if cdpJournal.CdpRecordsChecksum != xxhash.Sum64(cdpJournal.CdpRecords) {
		return errors.Errorf("checksum does not match")
	}
	return nil
}

func NewCdpJournalBuilder(opt *CdpJournalOption) (*CdpJournalBuilder, error) {
	if opt == nil {
		return nil, errors.New("nil option")
	}

	if !IsDataRecordByMask(opt.Mask) {
		return nil, errors.New("invalid mask")
	}

	if opt.SequenceGenerator == nil {
		return nil, errors.New("invalid sequence generator")
	}

	if opt.EnableEncrypt {
		if err := hkc.SetEncryptionKey([]byte(opt.EncryptionKey)); err != nil {
			return nil, err
		}
	}

	return &CdpJournalBuilder{
		schedule:          opt.Schedule,
		task:              opt.Task,
		mask:              opt.Mask,
		logger:            opt.Logger,
		sequenceGenerator: opt.SequenceGenerator,
		realtime:          opt.Mask == proto.IOMask_DATA_REALTIME,
		enableChecksum:    opt.EnableChecksum,
		enableCompress:    opt.EnableCompress,
		enableEncrypt:     opt.EnableEncrypt,
		encryptionKey:     opt.EncryptionKey,
	}, nil
}

func (b *CdpJournalBuilder) String() string {
	return fmt.Sprintf("CdpJournalBuilder(%s)", MaskString(b.mask))
}

func (b *CdpJournalBuilder) AddDiskRecord(
	diskID biotrkmeta.DiskID,
	offset uint64,
	data []byte,
) error {

	if len(data) == 0 {
		return nil
	}

	record, err := b.generateRecord(diskID.String(), offset, data, 0)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.append(record); err != nil {
		return err
	}

	if b.logger != nil {
		b.logger.Debugf("# <DiskRecord(disk=%s,off=%d,size=%d)>",
			diskID, offset, len(data))
	}

	return nil
}

func (b *CdpJournalBuilder) AddSharedMemoryRecord(
	devices []biotrkmeta.ProtectedDevice,
	shmIOs []*ioctl.IoRecord,
) (bool, error) {

	if len(shmIOs) == 0 {
		return false, nil
	}

	var containsRealtime bool
	records := make([]*CdpRecord, 0, len(shmIOs))

	// 1. shmIOs 返回结果改为按物理磁盘维度组织。
	// 2. 当受保护对象为卷时，根据 biotrkmeta.ProtectedDevice 中记录的
	//    ProtectedExtent（卷在物理磁盘上的区间）将 ShmIO 归类到对应卷。
	// 3. 当发生分区表修改（Type=1）时，读取修改后的分区表，重新计算卷与磁盘区间映射，
	//    并仅保留落在受保护区间内的 IO。

	for _, io := range shmIOs {
		if io == nil {
			continue
		}

		if b.logger != nil {
			b.logger.Debugf("# ShmIO(disk=%s,off=%d,size=%d,type=%d,consistency=%d)",
				io.Header.DiskID, io.Header.Offset, io.Header.Length,
				io.Header.Type, io.Header.Consistency)
		}

		if io.Header.Consistency == 1 {
			containsRealtime = true
		}

		if b.realtime && io.Header.Consistency == 0 {
			return containsRealtime,
				errors.New("inconsistent I/O during realtime phase")
		}

		// ── 分区表修改 ──
		if io.Header.Type == 1 {
			diskPath, err := io.Header.DiskID.DevicePath()
			if err != nil {
				return containsRealtime,
					errors.Wrap(os.ErrNotExist, diskPath)
			}

			// 读取修改后的分区表数据
			headers, err := biotrkmeta.ReadDiskPartTableData(diskPath)
			if err != nil {
				return containsRealtime, err
			}

			// 按受保护设备归类分区表头数据
			// 例如：MBR 主引导记录偏移 0、各 EBR、GPT 头等
			// 只有落在受保护 Extent 内的分区表头才生成记录
			byDevice := partitionHeadersBelongToDevice(devices, io.Header.DiskID, headers)

			for deviceID, deviceHeaders := range byDevice {
				for _, h := range deviceHeaders {
					start := uint64(h.Offset)
					record, err := b.generateRecord(
						deviceID,
						start,
						h.Data,
						io.Header.Timestamp,
					)
					if err != nil {
						return containsRealtime, err
					}
					records = append(records, record)
				}
			}

			continue
		}

		// ── 普通 IO：按卷/磁盘归类 ──
		if io.Header.Length == 0 {
			continue
		}

		// 将 IO 归类到其所属的受保护设备（磁盘或卷）
		deviceID := classifyShmIO(devices, io)
		if deviceID == "" {
			continue
		}

		record, err := b.generateRecord(
			deviceID,
			io.Header.Offset,
			io.Data,
			io.Header.Timestamp,
		)
		if err != nil {
			return containsRealtime, err
		}

		records = append(records, record)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return containsRealtime, b.append(records...)
}

func (b *CdpJournalBuilder) Build() *CdpJournal {

	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.packetComplete.CompareAndSwap(false, true) {
		return nil
	}

	records := append([]byte(nil), b.buf.Bytes()...)

	journal := &CdpJournal{
		ScheduleId:               b.schedule,
		TaskId:                   b.task,
		Mask:                     b.mask,
		MinTimestampInCdpRecords: b.minTimestamp,
		MaxTimestampInCdpRecords: b.maxTimestamp,
		MinSequenceInCdpRecords:  b.minSequence,
		MaxSequenceInCdpRecords:  b.maxSequence,
		CdpRecordsRawBytes:       b.rawTotalBytes,
		CdpRecords:               records,
	}

	journal.CdpRecordsEncodedBytes = uint64(len(records))

	WriteJournalChecksum(journal)

	b.reset()

	return journal
}

func (b *CdpJournalBuilder) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.reset()
}

func (b *CdpJournalBuilder) reset() {

	b.buf.Reset()

	b.minSequence = 0
	b.maxSequence = 0

	b.minTimestamp = 0
	b.maxTimestamp = 0

	b.rawTotalBytes = 0

	b.packetComplete.Store(false)

	if b.logger != nil {
		b.logger.Debugf("********** %s reset **********", b)
	}
}

func (b *CdpJournalBuilder) append(records ...*CdpRecord) error {

	if b.packetComplete.Load() {
		return errors.New("journal already built")
	}

	for _, record := range records {

		if record.Header.Mask != b.mask {
			return errors.Errorf(
				"mask mismatch: expect %s got %s",
				b.mask,
				record.Header.Mask,
			)
		}

		seq := record.Header.Sequence

		if b.maxSequence == 0 {

			if seq == 0 {
				return errors.New("invalid sequence 0")
			}

			b.minSequence = seq
			b.maxSequence = seq

			if b.realtime {

				ts := record.Header.Timestamp

				if ts == 0 {
					return errors.New("invalid timestamp 0")
				}

				b.minTimestamp = ts
				b.maxTimestamp = ts
			}

		} else {

			if seq != b.maxSequence+1 {
				return errors.Errorf(
					"sequence not continuous: got %d want %d",
					seq,
					b.maxSequence+1,
				)
			}

			b.maxSequence = seq

			if b.realtime {

				ts := record.Header.Timestamp

				if ts < b.maxTimestamp {
					return errors.New("timestamp rollback")
				}

				b.maxTimestamp = ts
			}
		}

		if err := record.Pack(&b.buf); err != nil {
			return err
		}

		b.rawTotalBytes += record.Header.BinaryStructSize() + hkc.ClusterHeaderSize + record.Data.RawSize
	}

	return nil
}

func (b *CdpJournalBuilder) generateRecord(
	deviceID string,
	offset uint64,
	data []byte,
	timestamp uint64,
) (*CdpRecord, error) {

	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	cluster, err := hkc.CreateCluster(
		offset,
		data,
		b.enableCompress,
		b.enableEncrypt,
		b.enableChecksum,
	)
	if err != nil {
		return nil, err
	}

	record := &CdpRecord{
		Data: *cluster,
	}

	record.Header.Mask = b.mask
	record.Header.DeviceID = deviceID
	record.Header.DeviceIDLen = uint32(len(record.Header.DeviceID))
	record.Header.Sequence = b.sequenceGenerator.Get()

	if b.realtime {
		record.Header.Timestamp = timestamp
	}

	if b.logger != nil {
		b.logger.Debugf("+ %s", record)
	}

	return record, nil
}
