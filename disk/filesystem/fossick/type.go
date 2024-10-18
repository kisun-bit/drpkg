package fossick

type Filesystem string

func (fs_ Filesystem) String() string {
	return string(fs_)
}

type Block struct {
	Offset  int64
	Size    int64 `struc:"int64,sizeof=Payload"`
	Payload []byte
}
