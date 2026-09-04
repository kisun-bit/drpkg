//go:build windows

package vss

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// =============================================================================
// HRESULT 常量与错误处理
// =============================================================================

// HRESULT is a custom type for the windows api HRESULT type.
type hresult uint

// HRESULT constant values for VSS api.
const (
	sOK                                          hresult = 0x00000000
	sFalse                                       hresult = 0x00000001
	eAccessDenied                                 hresult = 0x80070005
	eOutOfMemory                                  hresult = 0x8007000E
	eInvalidArg                                   hresult = 0x80070057
	vssEBadState                                  hresult = 0x80042301
	vssEUnexpected                                hresult = 0x80042302
	vssEProviderAlreadyRegistered                 hresult = 0x80042303
	vssEProviderNotRegistered                     hresult = 0x80042304
	vssEProviderVeto                              hresult = 0x80042306
	vssEProviderInUse                             hresult = 0x80042307
	vssEObjectNotFound                            hresult = 0x80042308
	vssEVolumeNotSupported                        hresult = 0x8004230C
	vssEVolumeNotSupportedByProvider              hresult = 0x8004230E
	vssEObjectAlreadyExists                       hresult = 0x8004230D
	vssEUnexpectedProviderError                   hresult = 0x8004230F
	vssECorruptXMLDocument                        hresult = 0x80042310
	vssEInvalidXMLDocument                        hresult = 0x80042311
	vssEMaximumNumberOfVolumesReached             hresult = 0x80042312
	vssEFlushWritesTimeout                        hresult = 0x80042313
	vssEHoldWritesTimeout                         hresult = 0x80042314
	vssEUnexpectedWriterError                     hresult = 0x80042315
	vssESnapshotSetInProgress                     hresult = 0x80042316
	vssEMaximumNumberOfSnapshotsReached           hresult = 0x80042317
	vssEWriterInfrastructure                      hresult = 0x80042318
	vssEWriterNotResponding                       hresult = 0x80042319
	vssEWriterAlreadySubscribed                   hresult = 0x8004231A
	vssEUnsupportedContext                        hresult = 0x8004231B
	vssEVolumeInUse                               hresult = 0x8004231D
	vssEMaximumDiffareaAssociationsReached        hresult = 0x8004231E
	vssEInsufficientStorage                       hresult = 0x8004231F
	vssENoSnapshotsImported                       hresult = 0x80042320
	vssESomeSnapshotsNotImported                  hresult = 0x80042321
	vssEMaximumNumberOfRemoteMachinesReached      hresult = 0x80042322
	vssERemoteServerUnavailable                   hresult = 0x80042323
	vssERemoteServerUnsupported                   hresult = 0x80042324
	vssERevertInProgress                          hresult = 0x80042325
	vssERevertVolumeLost                          hresult = 0x80042326
	vssERebootRequired                            hresult = 0x80042327
	vssETransactionFreezeTimeout                  hresult = 0x80042328
	vssETransactionThawTimeout                    hresult = 0x80042329
	vssEVolumeNotLocal                            hresult = 0x8004232D
	vssEClusterTimeout                            hresult = 0x8004232E
	vssEWritererrorInconsistentsnapshot           hresult = 0x800423F0
	vssEWritererrorOutofresources                 hresult = 0x800423F1
	vssEWritererrorTimeout                        hresult = 0x800423F2
	vssEWritererrorRetryable                      hresult = 0x800423F3
	vssEWritererrorNonretryable                   hresult = 0x800423F4
	vssEWritererrorRecoveryFailed                 hresult = 0x800423F5
	vssEBreakRevertIDFailed                       hresult = 0x800423F6
	vssELegacyProvider                            hresult = 0x800423F7
	vssEMissingDisk                               hresult = 0x800423F8
	vssEMissingHiddenVolume                       hresult = 0x800423F9
	vssEMissingVolume                             hresult = 0x800423FA
	vssEAutorecoveryFailed                        hresult = 0x800423FB
	vssEDynamicDiskError                          hresult = 0x800423FC
	vssENontransportableBCD                       hresult = 0x800423FD
	vssECannotRevertDiskid                        hresult = 0x800423FE
	vssEResyncInProgress                          hresult = 0x800423FF
	vssEClusterError                              hresult = 0x80042400
	vssEUnselectedVolume                          hresult = 0x8004232A
	vssESnapshotNotInSet                          hresult = 0x8004232B
	vssENestedVolumeLimit                         hresult = 0x8004232C
	vssENotSupported                              hresult = 0x8004232F
	vssEWritererrorPartialFailure                 hresult = 0x80042336
	vssEWriterStatusNotAvailable                  hresult = 0x80042409
)

var hresultToString = map[hresult]string{
	sOK:                                          "S_OK",
	eAccessDenied:                                "E_ACCESSDENIED",
	eOutOfMemory:                                 "E_OUTOFMEMORY",
	eInvalidArg:                                  "E_INVALIDARG",
	vssEBadState:                                 "VSS_E_BAD_STATE",
	vssEUnexpected:                               "VSS_E_UNEXPECTED",
	vssEProviderAlreadyRegistered:                "VSS_E_PROVIDER_ALREADY_REGISTERED",
	vssEProviderNotRegistered:                    "VSS_E_PROVIDER_NOT_REGISTERED",
	vssEProviderVeto:                             "VSS_E_PROVIDER_VETO",
	vssEProviderInUse:                            "VSS_E_PROVIDER_IN_USE",
	vssEObjectNotFound:                           "VSS_E_OBJECT_NOT_FOUND",
	vssEVolumeNotSupported:                       "VSS_E_VOLUME_NOT_SUPPORTED",
	vssEVolumeNotSupportedByProvider:             "VSS_E_VOLUME_NOT_SUPPORTED_BY_PROVIDER",
	vssEObjectAlreadyExists:                      "VSS_E_OBJECT_ALREADY_EXISTS",
	vssEUnexpectedProviderError:                  "VSS_E_UNEXPECTED_PROVIDER_ERROR",
	vssECorruptXMLDocument:                       "VSS_E_CORRUPT_XML_DOCUMENT",
	vssEInvalidXMLDocument:                       "VSS_E_INVALID_XML_DOCUMENT",
	vssEMaximumNumberOfVolumesReached:            "VSS_E_MAXIMUM_NUMBER_OF_VOLUMES_REACHED",
	vssEFlushWritesTimeout:                       "VSS_E_FLUSH_WRITES_TIMEOUT",
	vssEHoldWritesTimeout:                        "VSS_E_HOLD_WRITES_TIMEOUT",
	vssEUnexpectedWriterError:                    "VSS_E_UNEXPECTED_WRITER_ERROR",
	vssESnapshotSetInProgress:                    "VSS_E_SNAPSHOT_SET_IN_PROGRESS",
	vssEMaximumNumberOfSnapshotsReached:          "VSS_E_MAXIMUM_NUMBER_OF_SNAPSHOTS_REACHED",
	vssEWriterInfrastructure:                     "VSS_E_WRITER_INFRASTRUCTURE",
	vssEWriterNotResponding:                      "VSS_E_WRITER_NOT_RESPONDING",
	vssEWriterAlreadySubscribed:                  "VSS_E_WRITER_ALREADY_SUBSCRIBED",
	vssEUnsupportedContext:                       "VSS_E_UNSUPPORTED_CONTEXT",
	vssEVolumeInUse:                              "VSS_E_VOLUME_IN_USE",
	vssEMaximumDiffareaAssociationsReached:       "VSS_E_MAXIMUM_DIFFAREA_ASSOCIATIONS_REACHED",
	vssEInsufficientStorage:                      "VSS_E_INSUFFICIENT_STORAGE",
	vssENoSnapshotsImported:                      "VSS_E_NO_SNAPSHOTS_IMPORTED",
	vssESomeSnapshotsNotImported:                 "VSS_E_SOME_SNAPSHOTS_NOT_IMPORTED",
	vssEMaximumNumberOfRemoteMachinesReached:     "VSS_E_MAXIMUM_NUMBER_OF_REMOTE_MACHINES_REACHED",
	vssERemoteServerUnavailable:                  "VSS_E_REMOTE_SERVER_UNAVAILABLE",
	vssERemoteServerUnsupported:                  "VSS_E_REMOTE_SERVER_UNSUPPORTED",
	vssERevertInProgress:                         "VSS_E_REVERT_IN_PROGRESS",
	vssERevertVolumeLost:                         "VSS_E_REVERT_VOLUME_LOST",
	vssERebootRequired:                           "VSS_E_REBOOT_REQUIRED",
	vssETransactionFreezeTimeout:                 "VSS_E_TRANSACTION_FREEZE_TIMEOUT",
	vssETransactionThawTimeout:                   "VSS_E_TRANSACTION_THAW_TIMEOUT",
	vssEVolumeNotLocal:                           "VSS_E_VOLUME_NOT_LOCAL",
	vssEClusterTimeout:                           "VSS_E_CLUSTER_TIMEOUT",
	vssEWritererrorInconsistentsnapshot:          "VSS_E_WRITERERROR_INCONSISTENTSNAPSHOT",
	vssEWritererrorOutofresources:                "VSS_E_WRITERERROR_OUTOFRESOURCES",
	vssEWritererrorTimeout:                       "VSS_E_WRITERERROR_TIMEOUT",
	vssEWritererrorRetryable:                     "VSS_E_WRITERERROR_RETRYABLE",
	vssEWritererrorNonretryable:                  "VSS_E_WRITERERROR_NONRETRYABLE",
	vssEWritererrorRecoveryFailed:                "VSS_E_WRITERERROR_RECOVERY_FAILED",
	vssEBreakRevertIDFailed:                      "VSS_E_BREAK_REVERT_ID_FAILED",
	vssELegacyProvider:                           "VSS_E_LEGACY_PROVIDER",
	vssEMissingDisk:                              "VSS_E_MISSING_DISK",
	vssEMissingHiddenVolume:                      "VSS_E_MISSING_HIDDEN_VOLUME",
	vssEMissingVolume:                            "VSS_E_MISSING_VOLUME",
	vssEAutorecoveryFailed:                       "VSS_E_AUTORECOVERY_FAILED",
	vssEDynamicDiskError:                         "VSS_E_DYNAMIC_DISK_ERROR",
	vssENontransportableBCD:                      "VSS_E_NONTRANSPORTABLE_BCD",
	vssECannotRevertDiskid:                       "VSS_E_CANNOT_REVERT_DISKID",
	vssEResyncInProgress:                         "VSS_E_RESYNC_IN_PROGRESS",
	vssEClusterError:                             "VSS_E_CLUSTER_ERROR",
	vssEUnselectedVolume:                         "VSS_E_UNSELECTED_VOLUME",
	vssESnapshotNotInSet:                         "VSS_E_SNAPSHOT_NOT_IN_SET",
	vssENestedVolumeLimit:                        "VSS_E_NESTED_VOLUME_LIMIT",
	vssENotSupported:                             "VSS_E_NOT_SUPPORTED",
	vssEWritererrorPartialFailure:                "VSS_E_WRITERERROR_PARTIAL_FAILURE",
	vssEWriterStatusNotAvailable:                 "VSS_E_WRITER_STATUS_NOT_AVAILABLE",
}

func (h hresult) str() string {
	if i, ok := hresultToString[h]; ok {
		return i
	}
	return "UNKNOWN"
}

func (h hresult) Error() string {
	return h.str()
}

// vssError 封装 VSS API 返回的错误。
type vssError struct {
	text    string
	hresult hresult
}

func newVssError(text string, h hresult) error {
	return &vssError{text: text, hresult: h}
}

func newVssErrorIfNotOK(text string, h hresult) error {
	if h != sOK {
		return newVssError(text, h)
	}
	return nil
}

func (e *vssError) Error() string {
	return fmt.Sprintf("VSS error: %s: %s (%#x)", e.text, e.hresult.str(), uint(e.hresult))
}

func (e *vssError) Unwrap() error {
	return e.hresult
}

// vssTextError 封装 VSS 文本错误。
type vssTextError struct {
	text string
}

func newVssTextError(text string) error {
	return &vssTextError{text: text}
}

func (e *vssTextError) Error() string {
	return fmt.Sprintf("VSS error: %s", e.text)
}

// =============================================================================
// VSS 上下文与备份类型常量
// =============================================================================

type vssContext uint

const (
	vssCtxBackup vssContext = iota
	vssCtxFileShareBackup
	vssCtxNasRollback
	vssCtxAppRollback
	vssCtxClientAccessible
	vssCtxClientAccessibleWriters
	vssCtxAll
)

type vssBackupType uint

const (
	vssBTUndefined vssBackupType = iota
	vssBTFull
	vssBTIncremental
	vssBTDifferential
	vssBTLog
	vssBTCopy
	vssBTOther
)

type vssObjectType uint

const (
	vssObjectUnknown vssObjectType = iota
	vssObjectNone
	vssObjectSnapshotSet
	vssObjectSnapshot
	vssObjectProvider
	vssObjectTypeCount
)

// =============================================================================
// IVssBackupComponents COM 接口
// =============================================================================

var uuidIVss = ole.NewGUID("{665c1d5f-c218-414d-a05d-7fef5f9d5c86}")

type iVssBackupComponents struct {
	ole.IUnknown
}

type iVssBackupComponentsVTable struct {
	ole.IUnknownVtbl
	getWriterComponentsCount      uintptr
	getWriterComponents           uintptr
	initializeForBackup           uintptr
	setBackupState                uintptr
	initializeForRestore          uintptr
	setRestoreState               uintptr
	gatherWriterMetadata          uintptr
	getWriterMetadataCount        uintptr
	getWriterMetadata             uintptr
	freeWriterMetadata            uintptr
	addComponent                  uintptr
	prepareForBackup              uintptr
	abortBackup                   uintptr
	gatherWriterStatus            uintptr
	getWriterStatusCount          uintptr
	freeWriterStatus              uintptr
	getWriterStatus               uintptr
	setBackupSucceeded            uintptr
	setBackupOptions              uintptr
	setSelectedForRestore         uintptr
	setRestoreOptions             uintptr
	setAdditionalRestores         uintptr
	setPreviousBackupStamp        uintptr
	saveAsXML                     uintptr
	backupComplete                uintptr
	addAlternativeLocationMapping uintptr
	addRestoreSubcomponent        uintptr
	setFileRestoreStatus          uintptr
	addNewTarget                  uintptr
	setRangesFilePath             uintptr
	preRestore                    uintptr
	postRestore                   uintptr
	setContext                    uintptr
	startSnapshotSet              uintptr
	addToSnapshotSet              uintptr
	doSnapshotSet                 uintptr
	deleteSnapshots               uintptr
	importSnapshots               uintptr
	breakSnapshotSet              uintptr
	getSnapshotProperties         uintptr
	query                         uintptr
	isVolumeSupported             uintptr
	disableWriterClasses          uintptr
	enableWriterClasses           uintptr
	disableWriterInstances        uintptr
	exposeSnapshot                uintptr
	revertToSnapshot              uintptr
	queryRevertStatus             uintptr
}

func (vss *iVssBackupComponents) getVTable() *iVssBackupComponentsVTable {
	return (*iVssBackupComponentsVTable)(unsafe.Pointer(vss.RawVTable))
}

func (vss *iVssBackupComponents) AbortBackup() error {
	result, _, _ := syscall.Syscall(vss.getVTable().abortBackup, 1,
		uintptr(unsafe.Pointer(vss)), 0, 0)
	return newVssErrorIfNotOK("AbortBackup() failed", hresult(result))
}

func (vss *iVssBackupComponents) InitializeForBackup() error {
	result, _, _ := syscall.Syscall(vss.getVTable().initializeForBackup, 2,
		uintptr(unsafe.Pointer(vss)), 0, 0)
	return newVssErrorIfNotOK("InitializeForBackup() failed", hresult(result))
}

func (vss *iVssBackupComponents) SetContext(context vssContext) error {
	result, _, _ := syscall.Syscall(vss.getVTable().setContext, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(context), 0)
	return newVssErrorIfNotOK("SetContext() failed", hresult(result))
}

func (vss *iVssBackupComponents) GatherWriterMetadata() (*iVSSAsync, error) {
	var oleIUnknown *ole.IUnknown
	result, _, _ := syscall.Syscall(vss.getVTable().gatherWriterMetadata, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&oleIUnknown)), 0)
	err := newVssErrorIfNotOK("GatherWriterMetadata() failed", hresult(result))
	return convertToVSSAsync(oleIUnknown, err)
}

func convertToVSSAsync(oleIUnknown *ole.IUnknown, err error) (*iVSSAsync, error) {
	if err != nil {
		return nil, err
	}
	comInterface, err := queryInterface(oleIUnknown, uiidIVSSAsync)
	if err != nil {
		return nil, err
	}
	return (*iVSSAsync)(unsafe.Pointer(comInterface)), nil
}

func (vss *iVssBackupComponents) IsVolumeSupported(providerID *ole.GUID, volumeName string) (bool, error) {
	volumeNamePointer, err := syscall.UTF16PtrFromString(volumeName)
	if err != nil {
		return false, err
	}
	var isSupportedRaw uint32
	var result uintptr

	if runtime.GOARCH == "386" {
		id := (*[4]uintptr)(unsafe.Pointer(providerID))
		result, _, _ = syscall.Syscall9(vss.getVTable().isVolumeSupported, 7,
			uintptr(unsafe.Pointer(vss)), id[0], id[1], id[2], id[3],
			uintptr(unsafe.Pointer(volumeNamePointer)), uintptr(unsafe.Pointer(&isSupportedRaw)), 0, 0)
	} else {
		result, _, _ = syscall.Syscall6(vss.getVTable().isVolumeSupported, 4,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(providerID)),
			uintptr(unsafe.Pointer(volumeNamePointer)), uintptr(unsafe.Pointer(&isSupportedRaw)), 0, 0)
	}
	return isSupportedRaw != 0, newVssErrorIfNotOK("IsVolumeSupported() failed", hresult(result))
}

func (vss *iVssBackupComponents) StartSnapshotSet() (ole.GUID, error) {
	var snapshotSetID ole.GUID
	result, _, _ := syscall.Syscall(vss.getVTable().startSnapshotSet, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&snapshotSetID)), 0)
	return snapshotSetID, newVssErrorIfNotOK("StartSnapshotSet() failed", hresult(result))
}

func (vss *iVssBackupComponents) AddToSnapshotSet(volumeName string, providerID *ole.GUID, idSnapshot *ole.GUID) error {
	volumeNamePointer, err := syscall.UTF16PtrFromString(volumeName)
	if err != nil {
		return err
	}
	var result uintptr

	if runtime.GOARCH == "386" {
		id := (*[4]uintptr)(unsafe.Pointer(providerID))
		result, _, _ = syscall.Syscall9(vss.getVTable().addToSnapshotSet, 7,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(volumeNamePointer)),
			id[0], id[1], id[2], id[3], uintptr(unsafe.Pointer(idSnapshot)), 0, 0)
	} else {
		result, _, _ = syscall.Syscall6(vss.getVTable().addToSnapshotSet, 4,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(volumeNamePointer)),
			uintptr(unsafe.Pointer(providerID)), uintptr(unsafe.Pointer(idSnapshot)), 0, 0)
	}
	return newVssErrorIfNotOK("AddToSnapshotSet() failed", hresult(result))
}

func (vss *iVssBackupComponents) PrepareForBackup() (*iVSSAsync, error) {
	var oleIUnknown *ole.IUnknown
	result, _, _ := syscall.Syscall(vss.getVTable().prepareForBackup, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&oleIUnknown)), 0)
	err := newVssErrorIfNotOK("PrepareForBackup() failed", hresult(result))
	return convertToVSSAsync(oleIUnknown, err)
}

func apiBoolToInt(input bool) uint {
	if input {
		return 1
	}
	return 0
}

func (vss *iVssBackupComponents) SetBackupState(selectComponents bool,
	backupBootableSystemState bool, backupType vssBackupType, partialFileSupport bool) error {
	selectComponentsVal := apiBoolToInt(selectComponents)
	backupBootableSystemStateVal := apiBoolToInt(backupBootableSystemState)
	partialFileSupportVal := apiBoolToInt(partialFileSupport)

	result, _, _ := syscall.Syscall6(vss.getVTable().setBackupState, 5,
		uintptr(unsafe.Pointer(vss)), uintptr(selectComponentsVal),
		uintptr(backupBootableSystemStateVal), uintptr(backupType), uintptr(partialFileSupportVal), 0)
	return newVssErrorIfNotOK("SetBackupState() failed", hresult(result))
}

func (vss *iVssBackupComponents) DoSnapshotSet() (*iVSSAsync, error) {
	var oleIUnknown *ole.IUnknown
	result, _, _ := syscall.Syscall(vss.getVTable().doSnapshotSet, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&oleIUnknown)), 0)
	err := newVssErrorIfNotOK("DoSnapshotSet() failed", hresult(result))
	return convertToVSSAsync(oleIUnknown, err)
}

func (vss *iVssBackupComponents) DeleteSnapshots(snapshotID ole.GUID) (int32, ole.GUID, error) {
	var deletedSnapshots int32
	var nondeletedSnapshotID ole.GUID
	var result uintptr

	if runtime.GOARCH == "386" {
		id := (*[4]uintptr)(unsafe.Pointer(&snapshotID))
		result, _, _ = syscall.Syscall9(vss.getVTable().deleteSnapshots, 9,
			uintptr(unsafe.Pointer(vss)), id[0], id[1], id[2], id[3],
			uintptr(vssObjectSnapshot), uintptr(1), uintptr(unsafe.Pointer(&deletedSnapshots)),
			uintptr(unsafe.Pointer(&nondeletedSnapshotID)),
		)
	} else {
		result, _, _ = syscall.Syscall6(vss.getVTable().deleteSnapshots, 6,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&snapshotID)),
			uintptr(vssObjectSnapshot), uintptr(1), uintptr(unsafe.Pointer(&deletedSnapshots)),
			uintptr(unsafe.Pointer(&nondeletedSnapshotID)))
	}
	return deletedSnapshots, nondeletedSnapshotID, newVssErrorIfNotOK("DeleteSnapshots() failed", hresult(result))
}

func (vss *iVssBackupComponents) GetSnapshotProperties(snapshotID ole.GUID, properties *vssSnapshotProperties) error {
	var result uintptr

	if runtime.GOARCH == "386" {
		id := (*[4]uintptr)(unsafe.Pointer(&snapshotID))
		result, _, _ = syscall.Syscall6(vss.getVTable().getSnapshotProperties, 6,
			uintptr(unsafe.Pointer(vss)), id[0], id[1], id[2], id[3],
			uintptr(unsafe.Pointer(properties)))
	} else {
		result, _, _ = syscall.Syscall(vss.getVTable().getSnapshotProperties, 3,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&snapshotID)),
			uintptr(unsafe.Pointer(properties)))
	}
	return newVssErrorIfNotOK("GetSnapshotProperties() failed", hresult(result))
}

func (vss *iVssBackupComponents) BackupComplete() (*iVSSAsync, error) {
	var oleIUnknown *ole.IUnknown
	result, _, _ := syscall.Syscall(vss.getVTable().backupComplete, 2,
		uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(&oleIUnknown)), 0)
	err := newVssErrorIfNotOK("BackupComplete() failed", hresult(result))
	return convertToVSSAsync(oleIUnknown, err)
}

func (vss *iVssBackupComponents) Query(queriedObjectType vssObjectType) (*iVssEnumObject, error) {
	var enum *iVssEnumObject
	var result uintptr

	if runtime.GOARCH == "386" {
		id := (*[4]uintptr)(unsafe.Pointer(ole.IID_NULL))
		result, _, _ = syscall.Syscall9(vss.getVTable().query, 7,
			uintptr(unsafe.Pointer(vss)), id[0], id[1], id[2], id[3],
			uintptr(vssObjectNone), uintptr(queriedObjectType), uintptr(unsafe.Pointer(&enum)), 0)
	} else {
		result, _, _ = syscall.Syscall6(vss.getVTable().query, 5,
			uintptr(unsafe.Pointer(vss)), uintptr(unsafe.Pointer(ole.IID_NULL)),
			uintptr(vssObjectNone), uintptr(queriedObjectType), uintptr(unsafe.Pointer(&enum)), 0)
	}
	return enum, newVssErrorIfNotOK("Query() failed", hresult(result))
}

// =============================================================================
// vssSnapshotProperties
// =============================================================================

type vssSnapshotProperties struct {
	snapshotID           ole.GUID
	snapshotSetID        ole.GUID
	snapshotsCount       uint32
	snapshotDeviceObject *uint16
	originalVolumeName   *uint16
	originatingMachine   *uint16
	serviceMachine       *uint16
	exposedName          *uint16
	exposedPath          *uint16
	providerID           ole.GUID
	snapshotAttributes   uint32
	creationTimestamp    uint64
	status               uint
}

func (p *vssSnapshotProperties) getSnapshotDeviceObject() string {
	return ole.UTF16PtrToString(p.snapshotDeviceObject)
}

func (p *vssSnapshotProperties) getOriginalVolumeName() string {
	return ole.UTF16PtrToString(p.originalVolumeName)
}

func vssFreeSnapshotProperties(properties *vssSnapshotProperties) error {
	proc, err := findVssProc("VssFreeSnapshotProperties")
	if err != nil {
		return err
	}
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(properties)))
	return nil
}

// =============================================================================
// vssProviderProperties
// =============================================================================

type vssProviderProperties struct {
	providerID        ole.GUID
	providerName      *uint16
	providerType      uint32
	providerVersion   *uint16
	providerVersionID ole.GUID
	classID           ole.GUID
}

func vssFreeProviderProperties(p *vssProviderProperties) {
	ole.CoTaskMemFree(uintptr(unsafe.Pointer(p.providerName)))
	p.providerName = nil
	ole.CoTaskMemFree(uintptr(unsafe.Pointer(p.providerVersion)))
	p.providerVersion = nil
}

// =============================================================================
// IVSSAsync
// =============================================================================

var uiidIVSSAsync = ole.NewGUID("{507C37B4-CF5B-4e95-B0AF-14EB9767467E}")

type iVSSAsync struct {
	ole.IUnknown
}

type iVSSAsyncVTable struct {
	ole.IUnknownVtbl
	cancel      uintptr
	wait        uintptr
	queryStatus uintptr
}

const (
	vssSAsyncPending  = 0x00042309
	vssSAsyncFinished = 0x0004230A
	vssSAsyncCanceled = 0x0004230B
)

func (vssAsync *iVSSAsync) getVTable() *iVSSAsyncVTable {
	return (*iVSSAsyncVTable)(unsafe.Pointer(vssAsync.RawVTable))
}

func (vssAsync *iVSSAsync) Cancel() hresult {
	result, _, _ := syscall.Syscall(vssAsync.getVTable().cancel, 1,
		uintptr(unsafe.Pointer(vssAsync)), 0, 0)
	return hresult(result)
}

func (vssAsync *iVSSAsync) Wait(millis uint32) hresult {
	result, _, _ := syscall.Syscall(vssAsync.getVTable().wait, 2,
		uintptr(unsafe.Pointer(vssAsync)), uintptr(millis), 0)
	return hresult(result)
}

func (vssAsync *iVSSAsync) QueryStatus() (hresult, uint32) {
	var state uint32
	result, _, _ := syscall.Syscall(vssAsync.getVTable().queryStatus, 3,
		uintptr(unsafe.Pointer(vssAsync)), uintptr(unsafe.Pointer(&state)), 0)
	return hresult(result), state
}

func (vssAsync *iVSSAsync) WaitUntilAsyncFinished(timeout time.Duration) error {
	const maxTimeout = math.MaxInt32 * time.Millisecond
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	h := vssAsync.Wait(uint32(timeout.Milliseconds()))
	if err := newVssErrorIfNotOK("Wait() failed", h); err != nil {
		vssAsync.Cancel()
		return err
	}

	h, state := vssAsync.QueryStatus()
	if err := newVssErrorIfNotOK("QueryStatus() failed", h); err != nil {
		vssAsync.Cancel()
		return err
	}

	if state == vssSAsyncCanceled {
		return newVssTextError("async operation cancelled")
	}

	if state == vssSAsyncPending {
		vssAsync.Cancel()
		return newVssTextError("async operation pending")
	}

	if state != vssSAsyncFinished {
		if err := newVssErrorIfNotOK("async operation failed", hresult(state)); err != nil {
			return err
		}
	}

	return nil
}

// =============================================================================
// IVSSAdmin / IVssEnumObject
// =============================================================================

var (
	uiidIVSSAdmin       = ole.NewGUID("{77ED5996-2F63-11d3-8A39-00C04F72D8E3}")
	clsidVssCoordinator = ole.NewGUID("{E579AB5F-1CC4-44b4-BED9-DE0991FF0623}")
)

type iVSSAdmin struct {
	ole.IUnknown
}

type iVSSAdminVTable struct {
	ole.IUnknownVtbl
	registerProvider            uintptr
	unregisterProvider          uintptr
	queryProviders              uintptr
	abortAllSnapshotsInProgress uintptr
}

func (vssAdmin *iVSSAdmin) getVTable() *iVSSAdminVTable {
	return (*iVSSAdminVTable)(unsafe.Pointer(vssAdmin.RawVTable))
}

func (vssAdmin *iVSSAdmin) QueryProviders() (*iVssEnumObject, error) {
	var enum *iVssEnumObject
	result, _, _ := syscall.Syscall(vssAdmin.getVTable().queryProviders, 2,
		uintptr(unsafe.Pointer(vssAdmin)), uintptr(unsafe.Pointer(&enum)), 0)
	return enum, newVssErrorIfNotOK("QueryProviders() failed", hresult(result))
}

type iVssEnumObject struct {
	ole.IUnknown
}

type iVssEnumObjectVTable struct {
	ole.IUnknownVtbl
	next  uintptr
	skip  uintptr
	reset uintptr
	clone uintptr
}

func (vssEnum *iVssEnumObject) getVTable() *iVssEnumObjectVTable {
	return (*iVssEnumObjectVTable)(unsafe.Pointer(vssEnum.RawVTable))
}

func (vssEnum *iVssEnumObject) Next(count uint, props unsafe.Pointer) (uint, error) {
	var fetched uint32
	result, _, _ := syscall.Syscall6(vssEnum.getVTable().next, 4,
		uintptr(unsafe.Pointer(vssEnum)), uintptr(count), uintptr(props),
		uintptr(unsafe.Pointer(&fetched)), 0, 0)
	if hresult(result) == sFalse {
		return uint(fetched), nil
	}
	return uint(fetched), newVssErrorIfNotOK("Next() failed", hresult(result))
}

// =============================================================================
// 内部快照结构
// =============================================================================

type internalSnapshot struct {
	iVssBackupComponents *iVssBackupComponents
	snapshotID           ole.GUID
	snapshotSetID        ole.GUID
	snapshotProperties   vssSnapshotProperties
	snapshotDeviceObject string
	volumeName           string
	creationTime         time.Time
	timeout              time.Duration
}

func (p *internalSnapshot) getSnapshotDeviceObject() string {
	return p.snapshotDeviceObject
}

// =============================================================================
// COM 初始化
// =============================================================================

func initializeVssCOMInterface() (*ole.IUnknown, error) {
	vssInstance, err := loadIVssBackupComponentsConstructor()
	if err != nil {
		return nil, err
	}

	if err = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		if oleErr, ok := err.(*ole.OleError); !ok || hresult(oleErr.Code()) != sFalse {
			return nil, err
		}
	}

	if err = ole.CoInitializeSecurity(
		-1,   // Default COM authentication service
		6,    // RPC_C_AUTHN_LEVEL_PKT_PRIVACY
		3,    // RPC_C_IMP_LEVEL_IMPERSONATE
		0x20, // EOAC_STATIC_CLOAKING
	); err != nil {
		return nil, newVssError(
			"Failed to initialize security for VSS request",
			hresult(err.(*ole.OleError).Code()))
	}

	var oleIUnknown *ole.IUnknown
	result, _, _ := vssInstance.Call(uintptr(unsafe.Pointer(&oleIUnknown)))
	h := hresult(result)

	switch h {
	case sOK:
	case eAccessDenied:
		return oleIUnknown, newVssError(
			"The caller does not have sufficient backup privileges or is not an administrator", h)
	default:
		return oleIUnknown, newVssError("Failed to create VSS instance", h)
	}

	if oleIUnknown == nil {
		return nil, newVssError("Failed to initialize COM interface", h)
	}

	return oleIUnknown, nil
}

// =============================================================================
// 公开 API 实现
// =============================================================================

var errNotSupported = errors.New("VSS snapshots are only supported on Windows")

// hasSufficientPrivileges 检查 VSS 权限。
func hasSufficientPrivileges() error {
	oleIUnknown, err := initializeVssCOMInterface()
	if oleIUnknown != nil {
		oleIUnknown.Release()
	}
	return err
}

// HasSufficientPrivileges 检查当前进程是否具有使用 VSS 的权限。
func HasSufficientPrivileges() error {
	return hasSufficientPrivileges()
}

// GetVolumeNameForMountPoint 返回指定挂载点对应的卷 GUID 路径。
func GetVolumeNameForMountPoint(mountPoint string) (string, error) {
	if mountPoint != "" && mountPoint[len(mountPoint)-1] != filepath.Separator {
		mountPoint += string(filepath.Separator)
	}

	mountPointPointer, err := syscall.UTF16PtrFromString(mountPoint)
	if err != nil {
		return mountPoint, err
	}

	volumeNameBuffer := make([]uint16, 50)
	if err := windows.GetVolumeNameForVolumeMountPoint(
		mountPointPointer, &volumeNameBuffer[0], 50); err != nil {
		return mountPoint, err
	}

	return syscall.UTF16ToString(volumeNameBuffer), nil
}

// CreateSnapshot 为指定卷创建一个 VSS 快照。
func CreateSnapshot(volume string, timeout time.Duration) (*Snapshot, error) {
	snapshots, err := createSnapshots([]string{volume}, timeout)
	if err != nil {
		return nil, err
	}
	return snapshots[0], nil
}

// CreateSnapshots 为多个卷批量创建 VSS 快照。
func CreateSnapshots(volumes []string, timeout time.Duration) ([]*Snapshot, error) {
	return createSnapshots(volumes, timeout)
}

func createSnapshots(volumes []string, timeout time.Duration) ([]*Snapshot, error) {
	if len(volumes) == 0 {
		return nil, errors.New("no volumes specified")
	}

	is64Bit, err := isRunningOn64BitWindows()
	if err != nil {
		return nil, fmt.Errorf("failed to detect windows architecture: %w", err)
	}

	if (is64Bit && runtime.GOARCH != "amd64") || (!is64Bit && runtime.GOARCH != "386") {
		return nil, fmt.Errorf("executables compiled for %s can't use VSS on other architectures. "+
			"Please use an executable compiled for your platform.", runtime.GOARCH)
	}

	deadline := time.Now().Add(timeout)

	oleIUnknown, err := initializeVssCOMInterface()
	if oleIUnknown != nil {
		defer oleIUnknown.Release()
	}
	if err != nil {
		return nil, err
	}

	comInterface, err := queryInterface(oleIUnknown, uuidIVss)
	if err != nil {
		return nil, err
	}

	iVssBackupComponents := (*iVssBackupComponents)(unsafe.Pointer(comInterface))

	if err := iVssBackupComponents.InitializeForBackup(); err != nil {
		iVssBackupComponents.Release()
		return nil, err
	}

	if err := iVssBackupComponents.SetContext(vssCtxBackup); err != nil {
		iVssBackupComponents.Release()
		return nil, err
	}

	if err := iVssBackupComponents.SetBackupState(false, false, vssBTCopy, false); err != nil {
		iVssBackupComponents.Release()
		return nil, err
	}

	if err := callAsyncFunctionAndWait(iVssBackupComponents.GatherWriterMetadata,
		"GatherWriterMetadata", deadline); err != nil {
		iVssBackupComponents.Release()
		return nil, err
	}

	providerID := ole.IID_NULL

	for _, volume := range volumes {
		if isSupported, err := iVssBackupComponents.IsVolumeSupported(providerID, volume); err != nil {
			iVssBackupComponents.Release()
			return nil, err
		} else if !isSupported {
			iVssBackupComponents.Release()
			return nil, fmt.Errorf("snapshots are not supported for volume %s", volume)
		}
	}

	const retryStartSnapshotSetSleep = 5 * time.Second
	var snapshotSetID ole.GUID
	for {
		var err error
		snapshotSetID, err = iVssBackupComponents.StartSnapshotSet()
		if errors.Is(err, vssESnapshotSetInProgress) && time.Now().Add(-retryStartSnapshotSetSleep).Before(deadline) {
			time.Sleep(retryStartSnapshotSetSleep)
			continue
		}
		if err != nil {
			iVssBackupComponents.Release()
			return nil, err
		}
		break
	}

	// 记录每个 volume 对应的 snapshotSetID
	volumeSnapshotIDs := make(map[string]ole.GUID)
	for _, volume := range volumes {
		var volSnapshotID ole.GUID
		if err := iVssBackupComponents.AddToSnapshotSet(volume, providerID, &volSnapshotID); err != nil {
			iVssBackupComponents.Release()
			return nil, err
		}
		volumeSnapshotIDs[volume] = volSnapshotID
	}

	if err := callAsyncFunctionAndWait(iVssBackupComponents.PrepareForBackup,
		"PrepareForBackup", deadline); err != nil {
		iVssBackupComponents.AbortBackup()
		iVssBackupComponents.Release()
		return nil, err
	}

	if err := callAsyncFunctionAndWait(iVssBackupComponents.DoSnapshotSet,
		"DoSnapshotSet", deadline); err != nil {
		_ = iVssBackupComponents.AbortBackup()
		iVssBackupComponents.Release()
		return nil, err
	}

	var snapshots []*Snapshot
	for _, volume := range volumes {
		volSnapshotID := volumeSnapshotIDs[volume]

		var props vssSnapshotProperties
		if err := iVssBackupComponents.GetSnapshotProperties(volSnapshotID, &props); err != nil {
			_ = iVssBackupComponents.AbortBackup()
			iVssBackupComponents.Release()
			return nil, err
		}

		creationTime := time.Unix(0, int64(props.creationTimestamp)*100)

		internal := &internalSnapshot{
			iVssBackupComponents: iVssBackupComponents,
			snapshotID:           volSnapshotID,
			snapshotSetID:        snapshotSetID,
			snapshotProperties:   props,
			snapshotDeviceObject: props.getSnapshotDeviceObject(),
			volumeName:           volume,
			creationTime:         creationTime,
			timeout:              time.Until(deadline),
		}

		snapshots = append(snapshots, &Snapshot{
			deviceObject: internal.snapshotDeviceObject,
			volumeName:   internal.volumeName,
			creationTime: internal.creationTime,
		})
	}

	return snapshots, nil
}

// Delete 删除快照并释放资源。
func (s *Snapshot) Delete() error {
	// 此处需要 internalSnapshot 引用，但 Snapshot 是公开类型。
	// DeleteSnapshot 通过 ID 删除是更好的方式。
	// 对于 CreateSnapshot 返回的快照，调用方应使用返回的 Snapshot 对象。
	return errors.New("Delete() not yet implemented for single snapshot; use DeleteSnapshot instead")
}

// DeleteSnapshot 根据快照 ID 删除快照。
func DeleteSnapshot(snapshotID string) error {
	oleIUnknown, err := initializeVssCOMInterface()
	if oleIUnknown != nil {
		defer oleIUnknown.Release()
	}
	if err != nil {
		return err
	}

	comInterface, err := queryInterface(oleIUnknown, uuidIVss)
	if err != nil {
		return err
	}

	iVssBackupComponents := (*iVssBackupComponents)(unsafe.Pointer(comInterface))
	defer iVssBackupComponents.Release()

	if err := iVssBackupComponents.InitializeForBackup(); err != nil {
		return err
	}

	id, err := ole.CLSIDFromString(snapshotID)
	if err != nil {
		return fmt.Errorf("invalid snapshot ID: %w", err)
	}

	deleted, _, err := iVssBackupComponents.DeleteSnapshots(*id)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return errors.New("no snapshots were deleted")
	}
	return nil
}

// QuerySnapshots 查询系统中所有已有的 VSS 快照。
func QuerySnapshots() ([]SnapshotInfo, error) {
	oleIUnknown, err := initializeVssCOMInterface()
	if oleIUnknown != nil {
		defer oleIUnknown.Release()
	}
	if err != nil {
		return nil, err
	}

	comInterface, err := queryInterface(oleIUnknown, uuidIVss)
	if err != nil {
		return nil, err
	}

	iVssBackupComponents := (*iVssBackupComponents)(unsafe.Pointer(comInterface))
	defer iVssBackupComponents.Release()

	if err := iVssBackupComponents.InitializeForBackup(); err != nil {
		return nil, err
	}

	if err := iVssBackupComponents.SetContext(vssCtxBackup); err != nil {
		return nil, err
	}

	enum, err := iVssBackupComponents.Query(vssObjectSnapshot)
	if err != nil {
		return nil, err
	}
	defer enum.Release()

	var result []SnapshotInfo

	for {
		var props struct {
			objectType uint32
			snapshot   vssSnapshotProperties
		}
		count, err := enum.Next(1, unsafe.Pointer(&props))
		if err != nil {
			return nil, err
		}
		if count == 0 {
			break
		}

		creationTime := time.Unix(0, int64(props.snapshot.creationTimestamp)*100)

		result = append(result, SnapshotInfo{
			SnapshotID:    props.snapshot.snapshotID.String(),
			SnapshotSetID: props.snapshot.snapshotSetID.String(),
			VolumeName:    props.snapshot.getOriginalVolumeName(),
			DeviceObject:  props.snapshot.getSnapshotDeviceObject(),
			CreationTime:  creationTime,
			Attributes:    props.snapshot.snapshotAttributes,
		})

		_ = vssFreeSnapshotProperties(&props.snapshot)
	}

	return result, nil
}

// =============================================================================
// 辅助函数
// =============================================================================

type asyncCallFunc func() (*iVSSAsync, error)

func callAsyncFunctionAndWait(function asyncCallFunc, name string, deadline time.Time) error {
	iVssAsync, err := function()
	if err != nil {
		return err
	}

	if iVssAsync == nil {
		return fmt.Errorf("VSS error: %s() returned nil", name)
	}

	timeout := time.Until(deadline)
	if timeout <= 0 {
		return fmt.Errorf("VSS error: %s() deadline exceeded", name)
	}

	err = iVssAsync.WaitUntilAsyncFinished(timeout)
	iVssAsync.Release()
	return err
}

func getProviderID(provider string) (*ole.GUID, error) {
	providerLower := strings.ToLower(provider)
	switch providerLower {
	case "":
		return ole.IID_NULL, nil
	case "ms":
		return ole.NewGUID("{b5946137-7b9f-4925-af80-51abd60b20d5}"), nil
	}

	comInterface, err := ole.CreateInstance(clsidVssCoordinator, uiidIVSSAdmin)
	if err != nil {
		return nil, err
	}
	defer comInterface.Release()

	vssAdmin := (*iVSSAdmin)(unsafe.Pointer(comInterface))

	enum, err := vssAdmin.QueryProviders()
	if err != nil {
		return nil, err
	}
	defer enum.Release()

	id := ole.NewGUID(provider)

	var props struct {
		objectType uint32
		provider   vssProviderProperties
	}
	for {
		count, err := enum.Next(1, unsafe.Pointer(&props))
		if err != nil {
			return nil, err
		}
		if count < 1 {
			return nil, fmt.Errorf("invalid VSS provider %q", provider)
		}

		name := ole.UTF16PtrToString(props.provider.providerName)
		vssFreeProviderProperties(&props.provider)

		if id != nil && *id == props.provider.providerID ||
			id == nil && providerLower == strings.ToLower(name) {
			return &props.provider.providerID, nil
		}
	}
}

func loadIVssBackupComponentsConstructor() (*windows.LazyProc, error) {
	createInstanceName := "?CreateVssBackupComponents@@YAJPEAPEAVIVssBackupComponents@@@Z"
	if runtime.GOARCH == "386" {
		createInstanceName = "?CreateVssBackupComponents@@YGJPAPAVIVssBackupComponents@@@Z"
	}
	return findVssProc(createInstanceName)
}

func findVssProc(procName string) (*windows.LazyProc, error) {
	vssDll := windows.NewLazySystemDLL("VssApi.dll")
	err := vssDll.Load()
	if err != nil {
		return &windows.LazyProc{}, err
	}
	proc := vssDll.NewProc(procName)
	err = proc.Find()
	if err != nil {
		return &windows.LazyProc{}, err
	}
	return proc, nil
}

func queryInterface(oleIUnknown *ole.IUnknown, guid *ole.GUID) (*interface{}, error) {
	var ivss *interface{}
	result, _, _ := syscall.Syscall(oleIUnknown.VTable().QueryInterface, 3,
		uintptr(unsafe.Pointer(oleIUnknown)), uintptr(unsafe.Pointer(guid)),
		uintptr(unsafe.Pointer(&ivss)))
	if result != 0 {
		return nil, newVssError("QueryInterface failed", hresult(result))
	}
	return ivss, nil
}

func isRunningOn64BitWindows() (bool, error) {
	if runtime.GOARCH == "amd64" {
		return true, nil
	}

	isWow64 := false
	err := windows.IsWow64Process(windows.CurrentProcess(), &isWow64)
	if err != nil {
		return false, err
	}

	return isWow64, nil
}

func enumerateMountedFolders(volume string) ([]string, error) {
	var mountedFolders []string

	volumeNamePointer, err := syscall.UTF16PtrFromString(volume)
	if err != nil {
		return mountedFolders, err
	}

	volumeMountPointBuffer := make([]uint16, windows.MAX_LONG_PATH)
	handle, err := windows.FindFirstVolumeMountPoint(volumeNamePointer, &volumeMountPointBuffer[0],
		windows.MAX_LONG_PATH)
	if err != nil {
		return mountedFolders, nil
	}

	defer windows.FindVolumeMountPointClose(handle)

	volumeMountPoint := syscall.UTF16ToString(volumeMountPointBuffer)
	mountedFolders = append(mountedFolders, cleanupVolumeMountPoint(volume, volumeMountPoint))

	for {
		err = windows.FindNextVolumeMountPoint(handle, &volumeMountPointBuffer[0],
			windows.MAX_LONG_PATH)
		if err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			return mountedFolders,
				fmt.Errorf("FindNextVolumeMountPoint() failed: %w", err)
		}

		volumeMountPoint := syscall.UTF16ToString(volumeMountPointBuffer)
		mountedFolders = append(mountedFolders, cleanupVolumeMountPoint(volume, volumeMountPoint))
	}

	return mountedFolders, nil
}

func cleanupVolumeMountPoint(volume, mountPoint string) string {
	return strings.ToLower(filepath.Join(volume, mountPoint) + string(filepath.Separator))
}