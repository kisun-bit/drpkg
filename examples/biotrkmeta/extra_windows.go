package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/lunixbochs/struc"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func DisableVSS() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	srv, err := m.OpenService("vss")
	if err != nil {
		return err
	}
	defer srv.Close()

	_, err = srv.Control(svc.Stop)
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}

	oldCfg, err := srv.Config()
	if err != nil {
		return err
	}
	if oldCfg.StartType == windows.SERVICE_DISABLED {
		return nil
	}
	oldCfg.StartType = windows.SERVICE_DISABLED

	if err = srv.UpdateConfig(oldCfg); err != nil {
		return err
	}

	return nil
}

func ConfigRegistry(metaFile string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, "SYSTEM\\CurrentControlSet\\Services\\biotrk\\Parameters", registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	var meta MetadataRegions
	es, err := extend.FileDiskExtents(metaFile)
	if err != nil {
		return err
	}
	for _, v := range es {
		meta.Count++
		id, e := extend.WindowsDiskIDFromPath(v.Disk)
		if e != nil {
			return e
		}
		meta.Regions = append(meta.Regions, MetadataRegion{
			DiskID: id,
			Start:  uint64(v.Start),
			Size:   uint64(v.Size),
		})
	}

	var buf bytes.Buffer
	if err = struc.Pack(&buf, &meta); err != nil {
		return err
	}
	fragmentsVal := buf.Bytes()

	t := tablewriter.NewWriter(os.Stdout)
	t.SetHeader([]string{"Number", "Disk", "Start (bytes)", "Length (bytes)"})
	for i, v := range meta.Regions {
		line := []string{strconv.Itoa(i), strconv.Itoa(int(v.DiskID)), strconv.FormatUint(v.Start, 10), strconv.FormatUint(v.Size, 10)}
		t.Append(line)
	}
	fmt.Printf("Print regions of cdp meta file:\n")
	t.Render()

	fmt.Print("Print REGKEY(fragments):\n", hex.Dump(fragmentsVal))
	if err = k.SetBinaryValue("fragments", fragmentsVal); err != nil {
		return err
	}

	return nil
}

func customFile(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err = syscall.SetFileAttributes(ptr, syscall.FILE_ATTRIBUTE_SYSTEM|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		return err
	}
	return nil
}
