package info

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/kisun-bit/drpkg/xutil"
)

func newTestPsInfo(dmi DmiInfo, cpuModels []string, disks []Disk, bootDevices ...string) *PsInfo {
	pi := &PsInfo{}
	pi.Public.Dmi = dmi
	pi.Public.Cpu.Models = cpuModels
	pi.Public.Disks = disks
	for _, dev := range bootDevices {
		pi.Public.Volumes = append(pi.Public.Volumes, Volume{
			IsBootable: true,
			Segments:   []xutil.Segment{{Device: dev}},
		})
	}
	return pi
}

func TestNormalizeFingerprintValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ABC123  ", "ABC123"},
		{"none", ""},
		{"Default string", ""},
		{"To be filled by O.E.M.", ""},
		{"System Serial Number", ""},
		{"00000000-0000-0000-0000-000000000000", ""},
		{"ffffffff-ffff-ffff-ffff-ffffffffffff", ""},
		{"4c4c4544-0037-...", "4C4C4544-0037-..."},
	}

	for _, c := range cases {
		if got := normalizeFingerprintValue(c.in); got != c.want {
			t.Errorf("normalizeFingerprintValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMachineFingerprintRawString(t *testing.T) {
	fp := &MachineFingerprint{
		Detail: MachineFingerprintDetail{
			ProductUUID:        "UUID-1",
			BoardSerial:        "BOARD-1",
			CpuID:              "CPU-1",
			BootableDiskIDList: []string{"DISK-B", "DISK-A"},
		},
	}

	// 字段顺序固定，启动盘按字典序编号输出，与入参顺序无关。
	want := "productuuid_UUID-1&boardserial_BOARD-1&cpuid_CPU-1&bootdisk001_DISK-A&bootdisk002_DISK-B"
	if got := fp.RawString(); got != want {
		t.Fatalf("RawString() = %q, want %q", got, want)
	}

	// 空值字段仍保留键名，保证不同源头（哪个字段为空）能区分。
	empty := &MachineFingerprint{Detail: MachineFingerprintDetail{BoardSerial: "BOARD-1"}}
	if got, want := empty.RawString(), "productuuid_&boardserial_BOARD-1&cpuid_"; got != want {
		t.Fatalf("RawString() = %q, want %q", got, want)
	}

	// 启动盘顺序不影响原始串（内部排序）。
	reordered := &MachineFingerprint{Detail: MachineFingerprintDetail{
		ProductUUID:        "UUID-1",
		BoardSerial:        "BOARD-1",
		CpuID:              "CPU-1",
		BootableDiskIDList: []string{"DISK-A", "DISK-B"},
	}}
	if got := reordered.RawString(); got != want {
		t.Fatalf("reordered RawString() = %q, want %q", got, want)
	}

	if got := (*MachineFingerprint)(nil).RawString(); got != "" {
		t.Fatalf("nil RawString() = %q, want empty", got)
	}
}

func TestMachineFingerprintString(t *testing.T) {
	pi := newTestPsInfo(
		DmiInfo{SystemUUID: "uuid-1", BaseBoardSerialNumber: "SERIAL-1"},
		[]string{"Some CPU"},
		[]Disk{{Device: "/dev/sda", SerialNumber: "DISK-1"}},
		"/dev/sda",
	)

	fp, err := MachineFingerprintFromPsInfo(pi)
	if err != nil {
		t.Fatal(err)
	}

	// ID 即明细原始串的 sha256，String() 返回它。
	sum := sha256.Sum256([]byte(fp.RawString()))
	want := hex.EncodeToString(sum[:])

	if got := fp.ID; got != want {
		t.Fatalf("ID = %q, want %q", got, want)
	}
	if got := fp.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got := (*MachineFingerprint)(nil).String(); got != "" {
		t.Fatalf("nil String() = %q, want empty", got)
	}
}

func TestMachineFingerprintEquals(t *testing.T) {
	fp := &MachineFingerprint{ID: "id-1"}

	if !fp.Equals(fp) {
		t.Fatal("must equal itself")
	}
	if fp.Equals(nil) {
		t.Fatal("must not equal nil")
	}
	if fp.Equals(&MachineFingerprint{ID: "id-2"}) {
		t.Fatal("must not equal a different fingerprint")
	}
}

func TestFingerprintDiskHardwareID(t *testing.T) {
	// 物理序列号优先。
	if got := fingerprintDiskHardwareID(Disk{Device: "/dev/sda", PathId: "pci-1", SerialNumber: "DISK-1"}); got != "DISK-1" {
		t.Fatalf("want serial, got %q", got)
	}

	// 无序列号时回退到稳定路径名。
	if got := fingerprintDiskHardwareID(Disk{Device: "/dev/sda", PathId: "pci-0000:03:00.0-scsi-0:0:0:0"}); got != "PCI-0000:03:00.0-SCSI-0:0:0:0" {
		t.Fatalf("want path id, got %q", got)
	}

	// 路径名退化为裸设备名（"sda"）时不具备区分度，忽略。
	if got := fingerprintDiskHardwareID(Disk{Device: "/dev/sda", PathId: "sda"}); got != "" {
		t.Fatalf("want empty for bare device name, got %q", got)
	}

	// 分区表标识属于“内容”，不得参与硬件身份。
	d := Disk{Device: "/dev/sda"}
	d.Table.Identifier = "some-gpt-guid"
	if got := fingerprintDiskHardwareID(d); got != "" {
		t.Fatalf("partition table identifier must be ignored, got %q", got)
	}
}

func TestFingerprintBootDiskIDs(t *testing.T) {
	pi := newTestPsInfo(
		DmiInfo{},
		nil,
		[]Disk{
			{Device: "/dev/sda", SerialNumber: "DISK-A"},
			{Device: "/dev/sdb", SerialNumber: "DISK-B"},
			{Device: "/dev/sdc", SerialNumber: "DISK-C"}, // 非启动盘
		},
		"/dev/sdb", "/dev/sda", "/dev/sda", // sda 重复出现，应去重
	)

	got := fingerprintBootDiskIDs(pi)
	want := []string{"DISK-A", "DISK-B"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMachineFingerprintFromPsInfo_stable(t *testing.T) {
	pi := newTestPsInfo(
		DmiInfo{SystemUUID: "uuid-1", BaseBoardSerialNumber: "SERIAL-1"},
		[]string{"Some CPU"},
		[]Disk{{Device: "/dev/sda", SerialNumber: "DISK-1"}},
		"/dev/sda",
	)

	a, err := MachineFingerprintFromPsInfo(pi)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MachineFingerprintFromPsInfo(pi)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equals(b) {
		t.Fatal("identical inputs must yield identical fingerprints")
	}
}

func TestMachineFingerprintFromPsInfo_distinctAfterClone(t *testing.T) {
	// 只重新生成 SMBIOS UUID，其余硬件一致（虚拟机克隆的最小变化）。
	build := func(uuid string) *PsInfo {
		return newTestPsInfo(
			DmiInfo{SystemUUID: uuid, BaseBoardSerialNumber: "SERIAL-1"},
			[]string{"Some CPU"},
			[]Disk{{Device: "/dev/sda", SerialNumber: "DISK-1"}},
			"/dev/sda",
		)
	}

	src, err := MachineFingerprintFromPsInfo(build("uuid-src"))
	if err != nil {
		t.Fatal(err)
	}
	clone, err := MachineFingerprintFromPsInfo(build("uuid-clone"))
	if err != nil {
		t.Fatal(err)
	}
	if src.Equals(clone) {
		t.Fatal("clone must yield a different fingerprint")
	}
}

func TestMachineFingerprintFromPsInfo_distinctAfterBareMetalRestore(t *testing.T) {
	// 异机恢复：目标机的主板序列号、系统序列号、启动盘均不同（硬件身份变化）。
	src := newTestPsInfo(
		DmiInfo{SystemUUID: "uuid-1", BaseBoardSerialNumber: "SRC-BOARD", SystemSerial: "SRC-SYS"},
		[]string{"Some CPU"},
		[]Disk{{Device: "/dev/sda", SerialNumber: "SRC-DISK"}},
		"/dev/sda",
	)
	dst := newTestPsInfo(
		DmiInfo{SystemUUID: "uuid-2", BaseBoardSerialNumber: "DST-BOARD", SystemSerial: "DST-SYS"},
		[]string{"Some CPU"},
		[]Disk{{Device: "/dev/sda", SerialNumber: "DST-DISK"}},
		"/dev/sda",
	)

	s, err := MachineFingerprintFromPsInfo(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := MachineFingerprintFromPsInfo(dst)
	if err != nil {
		t.Fatal(err)
	}
	if s.Equals(d) {
		t.Fatal("bare-metal restore must yield a different fingerprint")
	}
}
