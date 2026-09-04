package lvm2meta

import (
	"encoding/binary"
	"github.com/pkg/errors"
	"io"
)

func ReadRawLocationDescriptorList(reader io.Reader) ([]RawLocationDescriptor, error) {
	var res []RawLocationDescriptor
	for {
		var loc RawLocationDescriptor
		err := binary.Read(reader, binary.LittleEndian, &loc)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to parse raw location descriptor")
		}
		if loc.Offset == 0 {
			break
		}
		res = append(res, loc)
	}
	return res, nil
}
