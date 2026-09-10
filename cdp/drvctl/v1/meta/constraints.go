package meta

const signature = "BIOTRKMETA      "

const DefaultFirstDiskBitmapStart = 1 << 20

const defaultBytesPerBit = 4 << 20

const defaultMaxDiskCount = 64

const defaultMaxDiskSize = 64 << 40

const defaultPerm = 0o600

var effectMaxDiskCountArr = []uint32{16, 32, 64, 128}

var effectMaxDiskSizeArr = []uint64{64 << 40, 128 << 40, 512 << 40}
