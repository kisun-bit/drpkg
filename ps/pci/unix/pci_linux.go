package unix

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/ps/pci/ids"
)

const (
	pciPath = "/sys/bus/pci/devices"
)

type bus struct {
	Devices []string
}

// ErrBadWidth indicates a bad data which was selected.
var ErrBadWidth = errors.New("bad width")

// PCI is a PCI device. We will fill this in as we add options.
// For now it just holds two uint16 per the PCI spec.
type PCI struct {
	Addr     string
	Vendor   uint16
	Device   uint16
	Class    uint32
	ModAlias string
	Driver   string

	VendorName      string
	DeviceName      string
	ClassDetailName string

	Latency   byte
	IRQPin    byte
	IRQLine   uint
	Bridge    bool
	FullPath  string
	ExtraInfo []string
	Config    []byte
	// The rest only gets filled in config space is read.
	// Type 0
	Control  Control
	Status   Status
	Resource string `pci:"resource"`
	BARS     []BAR  `json:",omitempty"`

	// Type 1
	Primary     uint8
	Secondary   uint8
	Subordinate uint8
	SecLatency  string
	IO          BAR
	Mem         BAR
	PrefMem     BAR
}

// String concatenates PCI address, Vendor, and Device and other information
// to make a useful display for the user.
func (p *PCI) String() string {
	return strings.Join(
		append([]string{fmt.Sprintf("%s: %v: %v %v | ", p.Addr, p.ClassDetailName, p.VendorName, p.DeviceName)}, p.ExtraInfo...),
		"\n")
}

// SetVendorDeviceName changes VendorName and DeviceName from a name to a number,
// if possible.
func (p *PCI) SetVendorDeviceName(ids []Vendor) {
	p.VendorName, p.DeviceName = Lookup(ids, p.Vendor, p.Device)
}

// ReadConfig reads the config space.
func (p *PCI) ReadConfig() error {
	dev := filepath.Join(p.FullPath, "config")
	c, err := os.ReadFile(dev)
	if err != nil {
		return err
	}
	p.Config = c
	p.Control = Control(binary.LittleEndian.Uint16(c[4:6]))
	p.Status = Status(binary.LittleEndian.Uint16(c[6:8]))
	return nil
}

func (p *PCI) InstallDriver() bool {
	return p.Driver != ""
}

// Compatible CompatSystem 是否兼容系统
// 见：https://github.com/torvalds/linux/blob/b7f94fcf55469ad3ef8a74c35b488dbfa314d1bb/drivers/pci/pci.h#L280
// /**
// * pci_match_one_device - Tell if a PCI device structure has a matching
// *			  PCI device id structure
// * @id: single PCI device id structure to match
// * @dev: the PCI device structure to match against
// *
// * Returns the matching pci_device_id structure or %NULL if there is no match.
// */
// static inline const struct pci_device_id *
// pci_match_one_device(const struct pci_device_id *id, const struct pci_dev *dev)
//
//	{
//	 	if ((id->vendor == PCI_ANY_ID || id->vendor == dev->vendor) &&
//	 	    (id->device == PCI_ANY_ID || id->device == dev->device) &&
//	 	    (id->subvendor == PCI_ANY_ID || id->subvendor == dev->subsystem_vendor) &&
//	 	    (id->subdevice == PCI_ANY_ID || id->subdevice == dev->subsystem_device) &&
//	 	    !((id->class ^ dev->class) & id->class_mask))
//	 		return id;
//	 	return NULL;
//	}
func (p *PCI) Compatible(chrootDir, kernel string) bool {
	// TODO.
	return false
}

type barreg struct {
	offset int64
	*os.File
}

func (r *barreg) Read(b []byte) (int, error) {
	return r.ReadAt(b, r.offset)
}

func (r *barreg) Write(b []byte) (int, error) {
	return r.WriteAt(b, r.offset)
}

// ReadConfigRegister reads a configuration register of size 8, 16, 32, or 64.
// It will only work on little-endian machines.
func (p *PCI) ReadConfigRegister(offset, size int64) (uint64, error) {
	dev := filepath.Join(p.FullPath, "config")
	f, err := os.Open(dev)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var reg uint64
	r := &barreg{offset: offset, File: f}
	switch size {
	default:
		return 0, fmt.Errorf("ReadConfigRegister@%#x width of %d: only options are 9, 16, 32, 64:%w", offset, size, ErrBadWidth)
	case 64:
		err = binary.Read(r, binary.LittleEndian, &reg)
	case 32:
		var val uint32
		err = binary.Read(r, binary.LittleEndian, &val)
		reg = uint64(val)
	case 16:
		var val uint16
		err = binary.Read(r, binary.LittleEndian, &val)
		reg = uint64(val)
	case 8:
		var val uint8
		err = binary.Read(r, binary.LittleEndian, &val)
		reg = uint64(val)
	}
	return reg, err
}

// WriteConfigRegister writes a configuration register of size 8, 16, 32, or 64.
// It will only work on little-endian machines.
func (p *PCI) WriteConfigRegister(offset, size int64, val uint64) error {
	f, err := os.OpenFile(filepath.Join(p.FullPath, "config"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	w := &barreg{offset: offset, File: f}
	switch size {
	default:
		return fmt.Errorf("WriteConfigRegister@%#x width of %d: only options are 8, 16, 32, 64:%w", offset, size, ErrBadWidth)
	case 64:
		err = binary.Write(w, binary.LittleEndian, &val)
	case 32:
		if val > math.MaxUint32 {
			return fmt.Errorf("%x:%w", val, strconv.ErrRange)
		}
		v := uint32(val)
		err = binary.Write(w, binary.LittleEndian, &v)
	case 16:
		if val > math.MaxUint16 {
			return fmt.Errorf("%x:%w", val, strconv.ErrRange)
		}
		v := uint16(val)
		err = binary.Write(w, binary.LittleEndian, &v)
	case 8:
		if val > math.MaxUint8 {
			return fmt.Errorf("%x:%w", val, strconv.ErrRange)
		}
		v := uint8(val)
		err = binary.Write(w, binary.LittleEndian, &v)
	}
	return err
}

// Read implements the BusReader interface for type bus. Iterating over each
// PCI bus device, and applying optional Filters to it.
func (bus *bus) Read(filters ...Filter) (Devices, error) {
	devices := make(Devices, 0, len(bus.Devices))
iter:
	for _, d := range bus.Devices {
		p, err := OnePCI(d)
		if err != nil {
			return nil, err
		}
		for _, f := range filters {
			if !f(p) {
				continue iter
			}
		}
		// In the older versions of this package, reading was conditional.
		// There is no harm done, and little performance lost, in just reading it.
		// It's less than a millisecond.
		// In all cases, the first 64 bits are visible, so setting vendor
		// and device names is also no problem. If we can't read any bytes
		// at all, that indicates a problem and it's worth passing that problem
		// up to higher levels.
		if err := p.ReadConfig(); err != nil {
			return nil, err
		}
		p.SetVendorDeviceName(ids.IDs)

		c := p.Config
		// Fill in whatever random stuff we can, from the base config.
		p.Latency = c[LatencyTimer]
		if c[HeaderType]&HeaderTypeMask == HeaderTypeBridge {
			p.Bridge = true
		}
		p.IRQPin = c[IRQPin]
		p.Primary = c[Primary]
		p.Secondary = c[Secondary]
		p.Subordinate = c[Subordinate]
		p.SecLatency = fmt.Sprintf("%02x", c[SecondaryLatency])

		devices = append(devices, p)
	}
	return devices, nil
}

func ListPCI(filters ...Filter) (hcs []*PCI, err error) {
	busReader, err := NewBusReader()
	if err != nil {
		return nil, err
	}
	devList, err := busReader.Read(filters...)
	if err != nil {
		return nil, err
	}
	for _, dev := range devList {
		hcs = append(hcs, dev)
	}
	return hcs, nil
}

// OnePCI takes the name of a directory containing linux-style
// PCI files and returns a filled-in *PCI.
func OnePCI(dir string) (*PCI, error) {
	pci := PCI{
		Addr:     filepath.Base(dir),
		FullPath: dir,
	}
	var err error
	var n uint64

	if n, err = readUint(dir, "vendor", 16, 16); err != nil {
		return nil, err
	}
	pci.Vendor = uint16(n)
	if n, err = readUint(dir, "device", 16, 16); err != nil {
		return nil, err
	}
	pci.Device = uint16(n)
	if n, err = readUint(dir, "class", 16, 24); err != nil {
		return nil, err
	}
	pci.Class = uint32(n)
	if n, err = readUint(dir, "irq", 0, 0); err != nil {
		return nil, err
	}
	pci.IRQLine = uint(n)
	if pci.Resource, err = readString(dir, "resource"); err != nil {
		return nil, err
	}
	if pci.ModAlias, err = readString(dir, "modalias"); err != nil {
		return nil, err
	}
	pci.Driver, _ = getDriver(dir)
	pci.VendorName, pci.DeviceName = fmt.Sprintf("%04x", pci.Vendor), fmt.Sprintf("%04x", pci.Device)
	pci.ClassDetailName = "ClassUnknown"
	if nm, ok := ids.ClassDetailNames[pci.Class]; ok {
		pci.ClassDetailName = nm
	}

	for i, r := range strings.Split(pci.Resource, "\n") {
		b, l, a, err := BaseLimType(r)
		// It's not clear how this can happen, if ever; could someone
		// hotunplug a device while we are scanning?
		if err != nil {
			return nil, fmt.Errorf("scanning resource %d(%s): %w", i, dir, err)
		}
		if b == 0 {
			continue
		}
		nb := BAR{
			Index: i,
			Base:  b,
			Lim:   l,
			Attr:  a,
		}
		switch i {
		case 13:
			pci.IO = nb
		case 14:
			pci.Mem = nb
		case 15:
			pci.PrefMem = nb
		default:
			pci.BARS = append(pci.BARS, nb)
		}
	}
	return &pci, nil
}

// BaseLimType parses a Linux resource string into base, limit, and attributes.
// The string must have three hex fields.
// Gaul was divided into three parts.
// So are the BARs.
func BaseLimType(bar string) (uint64, uint64, uint64, error) {
	f := strings.Fields(bar)
	if len(f) != 3 {
		return 0, 0, 0, fmt.Errorf("bar %q should have 3 fields", bar)
	}
	// They must all be parseable hex numbers.
	var vals [3]uint64
	for i, ff := range f {
		var err error
		if vals[i], err = strconv.ParseUint(ff, 0, 0); err != nil {
			return 0, 0, 0, err
		}
	}
	return vals[0], vals[1], vals[2], nil
}

// NewBusReader returns a BusReader, given a ...glob to match PCI devices against.
// If it can't glob in pciPath/g then it returns an error.
// For convenience, we use * as the glob if none are supplied.
func NewBusReader(globs ...string) (BusReader, error) {
	if len(globs) == 0 {
		globs = []string{"*"}
	}
	var exp []string
	for _, g := range globs {
		gg, err := filepath.Glob(filepath.Join(pciPath, g))
		if err != nil {
			return nil, err
		}
		exp = append(exp, gg...)
	}
	// uniq
	u := map[string]struct{}{}
	for _, e := range exp {
		u[e] = struct{}{}
	}
	exp = []string{}
	for v := range u {
		exp = append(exp, v)
	}
	// sort. This might even sort like a shell would do it.
	sort.Strings(exp)
	return &bus{Devices: exp}, nil
}

func getDriver(dir string) (string, error) {
	path := filepath.Join(dir, "driver")
	link, err := filepath.EvalSymlinks(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("unable to evaluate driver symlink '%s': %s", path, err)
	}
	if link == "" {
		return "", nil
	}
	baseName := filepath.Base(link)
	return baseName, nil
}

func readUint(dir, file string, base, bits int) (uint64, error) {
	s, err := readString(dir, file)
	if err != nil {
		return 0, err
	}
	s = strings.TrimPrefix(s, "0x")
	return strconv.ParseUint(s, base, bits)
}

func readString(dir, file string) (string, error) {
	s, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(s), "\n"), nil
}

// Devices contains a slice of one or more PCI devices
type Devices []*PCI

// Print prints information to an io.Writer
func (d Devices) Print(o io.Writer, verbose, confSize int) error {
	for _, pci := range d {
		if _, err := fmt.Fprintf(o, "%s\n", pci.String()); err != nil {
			return err
		}
		var extraNL bool
		// Make sure we have read enough config space to satisfy the verbose and confSize requests.
		// If len(pci.Config) is > 64, that's the only test we need.
		if (verbose > 1 || confSize > 64) && len(pci.Config) < 256 {
			return os.ErrPermission
		}
		if verbose >= 1 {
			c := pci.Config
			if _, err := fmt.Fprintf(o, "\tControl: %s\n\tStatus: %s\n\tLatency: %d", pci.Control.String(), pci.Status.String(), pci.Latency); err != nil {
				return err
			}
			if pci.Bridge {
				// Bus: primary=00, secondary=05, subordinate=0c, sec-latency=0
				// I/O behind bridge: 00002000-00002fff [size=4K]
				// Memory behind bridge: f0000000-f1ffffff [size=32M]
				// Prefetchable memory behind bridge: 00000000f2900000-00000000f29fffff [size=1M]
				if _, err := fmt.Fprintf(o, ", Cache Line Size: %d bytes", c[CacheLineSize]); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(o, "\n\tBus: primary=%02x, secondary=%02x, subordinate=%02x, sec-latency=%s",
					pci.Primary, pci.Secondary, pci.Subordinate, pci.SecLatency); err != nil {
					return err
				}
				// I hate this code.
				// I miss Rust tuples at times.
				for _, e := range []struct {
					h, f string
					b, l uint64
				}{
					{h: "\n\tI/O behind bridge: ", f: "%#08x-%#08x [size=%#x]", b: pci.IO.Base, l: pci.IO.Lim},
					{h: "\n\tMemory behind bridge: ", f: "%#08x-%#08x [size=%#x]", b: pci.Mem.Base, l: pci.Mem.Lim},
					{h: "\n\tPrefetchable memory behind bridge: ", f: "%#08x-%#08x [size=%#x]", b: pci.PrefMem.Base, l: pci.PrefMem.Lim},
				} {
					s := e.h + " [disabled]"
					if e.b != 0 {
						sz := e.l - e.b + 1
						s = fmt.Sprintf(e.h+e.f, e.b, e.l, sz)
					}
					if _, err := fmt.Fprint(o, s); err != nil {
						return err
					}
				}
			}
			fmt.Fprintf(o, "\n")
			if pci.IRQPin != 0 {
				if _, err := fmt.Fprintf(o, "\tInterrupt: pin %X routed to IRQ %X\n", 9+pci.IRQPin, pci.IRQLine); err != nil {
					return err
				}
			}
			if !pci.Bridge {
				for _, b := range pci.BARS {
					if _, err := fmt.Fprintf(o, "\t%v\n", b.String()); err != nil {
						return err
					}
				}
			}
			extraNL = true
		}

		if confSize > 0 {
			r := io.LimitReader(bytes.NewBuffer(pci.Config), int64(confSize))
			e := hex.Dumper(o)
			if _, err := io.Copy(e, r); err != nil {
				return err
			}
			extraNL = true
		}
		// lspci likes that extra line of separation
		if extraNL {
			fmt.Fprintf(o, "\n")
		}
	}
	return nil
}

// SetVendorDeviceName sets all numeric IDs of all the devices
// using the pci device SetVendorDeviceName.
func (d Devices) SetVendorDeviceName(ids []Vendor) {
	for _, p := range d {
		p.SetVendorDeviceName(ids)
	}
}

// ReadConfig reads the config info for all the devices.
func (d Devices) ReadConfig() error {
	for _, p := range d {
		if err := p.ReadConfig(); err != nil {
			return err
		}
	}
	return nil
}

// ReadConfigRegister reads the config info for all the devices.
func (d Devices) ReadConfigRegister(offset, size int64) ([]uint64, error) {
	var vals []uint64
	for _, p := range d {
		val, err := p.ReadConfigRegister(offset, size)
		if err != nil {
			return nil, err
		}
		vals = append(vals, val)
	}
	return vals, nil
}

// WriteConfigRegister writes the config info for all the devices.
func (d Devices) WriteConfigRegister(offset, size int64, val uint64) error {
	for _, p := range d {
		if err := p.WriteConfigRegister(offset, size, val); err != nil {
			return err
		}
	}
	return nil
}
