package xfs

import "unsafe"

type BtreeBNOHeader interface {
	GetMagic() [4]byte
	GetLevel() uint16
	GetNumrecs() uint16
	GetLeftsib() uint64
	GetRightsib() uint64
	GetBlkno() uint64
	GetLsn() uint64
	GetUUID() [16]byte
	GetOwner() uint64
	GetCRC() uint64
	GetPad() uint32
	Size() uint32
}

type BtreeBlockCom struct {
	Magic   [4]byte `struc:"big"`
	Level   uint16  `struc:"big"`
	Numrecs uint16  `struc:"big"`
}

type BtreeBlockS struct {
	BtreeBlockCom
	BtreeBlockShdr
}

type BtreeBlockL struct {
	BtreeBlockCom
	BtreeBlockLhdr
}

type BtreeBlockShdr struct {
	Leftsib  uint32 `struc:"big"`
	Rightsib uint32 `struc:"big"`
	Blkno    uint64 `struc:"big"`
	Lsn      uint64 `struc:"big"`
	UUID     [16]byte
	Owner    uint32 `struc:"big"`
	CRC      uint32 `struc:"big"`
}

type BtreeBlockLhdr struct {
	Leftsib  uint64 `struc:"big"`
	Rightsib uint64 `struc:"big"`
	Blkno    uint64 `struc:"big"`
	Lsn      uint64 `struc:"big"`
	UUID     [16]byte
	Owner    uint64 `struc:"big"`
	CRC      uint32 `struc:"big"`
	Pad      uint32 `struc:"big"`
}

type AllocRec struct {
	StartBlock uint32 `struc:"big"`
	BlockCount uint32 `struc:"big"`
}

type AllocPtr struct {
	Ptr uint32 `struc:"big"`
}

func (xs BtreeBlockS) GetMagic() [4]byte {
	return xs.Magic
}

func (xs BtreeBlockS) GetLevel() uint16 {
	return xs.Level
}

func (xs BtreeBlockS) GetNumrecs() uint16 {
	return xs.Numrecs
}

func (xs BtreeBlockS) GetLeftsib() uint64 {
	return uint64(xs.Leftsib)
}

func (xs BtreeBlockS) GetRightsib() uint64 {
	return uint64(xs.Rightsib)
}

func (xs BtreeBlockS) GetBlkno() uint64 {
	return xs.Blkno
}

func (xs BtreeBlockS) GetLsn() uint64 {
	return xs.Lsn
}

func (xs BtreeBlockS) GetUUID() [16]byte {
	return xs.UUID
}

func (xs BtreeBlockS) GetOwner() uint64 {
	return uint64(xs.Owner)
}

func (xs BtreeBlockS) GetCRC() uint64 {
	return uint64(xs.CRC)
}

func (xs BtreeBlockS) GetPad() uint32 {
	return 0
}

func (xs BtreeBlockS) Size() uint32 {
	return uint32(unsafe.Sizeof(xs))
}

func (xl BtreeBlockL) GetMagic() [4]byte {
	return xl.Magic
}

func (xl BtreeBlockL) GetLevel() uint16 {
	return xl.Level
}

func (xl BtreeBlockL) GetNumrecs() uint16 {
	return xl.Numrecs
}

func (xl BtreeBlockL) GetLeftsib() uint64 {
	return xl.Leftsib
}

func (xl BtreeBlockL) GetRightsib() uint64 {
	return xl.Rightsib
}

func (xl BtreeBlockL) GetBlkno() uint64 {
	return xl.Blkno
}

func (xl BtreeBlockL) GetLsn() uint64 {
	return xl.Lsn
}

func (xl BtreeBlockL) GetUUID() [16]byte {
	return xl.UUID
}

func (xl BtreeBlockL) GetOwner() uint64 {
	return xl.Owner
}

func (xl BtreeBlockL) GetCRC() uint64 {
	return uint64(xl.CRC)
}

func (xl BtreeBlockL) GetPad() uint32 {
	return xl.Pad
}

func (xl BtreeBlockL) Size() uint32 {
	return uint32(unsafe.Sizeof(xl))
}
