package x2xlib

import (
	"encoding/json"
	"os"

	"github.com/pkg/errors"
)

func addVDL(index string, v *vdl) (err error) {
	if v == nil {
		return errors.New("vdl is nil")
	}
	if err = v.check(); err != nil {
		return err
	}

	vdls, err := listVDL(index)
	if err != nil {
		return err
	}
	for _, oldVdl := range vdls {
		if oldVdl.Id == v.Id {
			return nil
		}
	}

	vdls = append(vdls, v)
	data, err := json.MarshalIndent(vdls, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(index, data, 0o644)
}

func delVDL(index string, id string) (err error) {
	vdls, err := listVDL(index)
	if err != nil {
		return err
	}

	existed := false
	vdlsNew := make([]*vdl, 0)
	for _, oldVdl := range vdls {
		if oldVdl.Id == id {
			existed = true
			continue
		}
		vdlsNew = append(vdlsNew, oldVdl)
	}

	if !existed {
		return nil
	}

	data, err := json.MarshalIndent(vdlsNew, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(index, data, 0o644)
}

func listVDL(index string) (vdls []*vdl, err error) {
	vdls = make([]*vdl, 0)
	data, err := os.ReadFile(index)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &vdls); err != nil {
		return nil, err
	}
	return vdls, nil
}
