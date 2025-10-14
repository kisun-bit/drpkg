package image

type Image struct {
	Path string
}

func Open(path string) (*Image, error) {
	if err := checkQemuTool(); err != nil {
		return nil, err
	}

}

func (img *Image) ReadAt(b []byte, off int64) (n int, err error) {

}

func (img *Image) WriteAt(b []byte, off int64) (n int, err error) {

}

func (img *Image) Sync() error {

}

func (img *Image) Close() error {

}
