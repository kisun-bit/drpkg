package lvm2meta

import (
	"encoding/binary"
	"github.com/pkg/errors"
	"io"
)

func ReadDataAreaDescriptorList(reader io.Reader) ([]DataAreaDescriptor, error) {
	var res []DataAreaDescriptor
	for {
		var desc DataAreaDescriptor
		err := binary.Read(reader, binary.LittleEndian, &desc)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to parse data area descriptor")
		}
		if desc.Offset == 0 {
			break
		}
		res = append(res, desc)
	}
	return res, nil
}
