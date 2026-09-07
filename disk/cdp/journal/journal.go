package journal

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/transfer"
	"github.com/kisun-bit/drpkg/disk/image/hkc"
	"github.com/kisun-bit/drpkg/logger"
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
	guid ioctl.DiskGuid,
	offset uint64,
	data []byte,
) error {

	if len(data) == 0 {
		return nil
	}

	record, err := b.generateRecord(guid, offset, data, 0)
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
			guid, offset, len(data))
	}

	return nil
}

func (b *CdpJournalBuilder) AddSharedMemoryRecord(
	disks []ioctl.ProtectDisk,
	shmIOs []*transfer.Io,
) (bool, error) {

	if len(shmIOs) == 0 {
		return false, nil
	}

	var containsRealtime bool
	records := make([]*CdpRecord, 0, len(shmIOs))

	for _, io := range shmIOs {
		if io == nil {
			continue
		}

		if b.logger != nil {
			b.logger.Debugf("# %s", io)
		}

		if io.Header.Consistency == 1 {
			containsRealtime = true
		}

		if b.realtime && io.Header.Consistency == 0 {
			return containsRealtime,
				errors.New("inconsistent I/O during realtime phase")
		}

		// 分区表记录
		if io.Header.Type == 1 {

			diskPath, err := ioctl.GetDiskPathByGuid(io.Header.DiskId)
			if err != nil {
				return containsRealtime,
					errors.Wrap(os.ErrNotExist, diskPath)
			}

			// 不能忽略坏块的错误，否则可能导致后续恢复异常
			headers, err := ioctl.ReadDiskPartTableData(diskPath)
			if err != nil {
				return containsRealtime, err
			}

			regions := ioctl.DiskProtectRegions(disks, io.Header.DiskId)

			for _, h := range headers {

				start := uint64(h.Offset)
				size := uint64(len(h.Data))

				if !ioctl.IsBlockProtected(regions, start, size) {
					logger.Warnf(
						"partition table data out of bounds (offset=%d,size=%d)",
						start,
						size,
					)
					continue
				}

				record, err := b.generateRecord(
					io.Header.DiskId,
					start,
					h.Data,
					io.Header.Timestamp,
				)
				if err != nil {
					return containsRealtime, err
				}

				records = append(records, record)
			}

			continue
		}

		if io.Header.Length == 0 {
			continue
		}

		record, err := b.generateRecord(
			io.Header.DiskId,
			io.Header.Offset,
			io.Buffer,
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
	guid ioctl.DiskGuid,
	offset uint64,
	data []byte,
	timestamp uint64,
) (*CdpRecord, error) {

	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	sguid, err := ioctl.StorageGuid(guid)
	if err != nil {
		return nil, err
	}

	if len(sguid) == 0 {
		return nil, errors.New("invalid storage guid")
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
	record.Header.DiskGuid = sguid
	record.Header.DiskGuidLen = uint32(len(sguid))
	record.Header.Sequence = b.sequenceGenerator.Get()

	if b.realtime {
		record.Header.Timestamp = timestamp
	}

	if b.logger != nil {
		b.logger.Debugf("+ %s", record)
	}

	return record, nil
}
