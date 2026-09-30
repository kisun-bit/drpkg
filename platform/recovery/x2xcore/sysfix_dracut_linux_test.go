package x2xcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kisun-bit/drpkg/platform/info"
)

func dracutConfPath(root string) string {
	return filepath.Join(root, "etc/dracut.conf.d", "99-restore.conf")
}

func readDracutConf(t *testing.T, root string) string {
	t.Helper()
	bs, err := os.ReadFile(dracutConfPath(root))
	if err != nil {
		t.Fatalf("read %s: %v", dracutConfPath(root), err)
	}
	return string(bs)
}

func mkMdraidDracutDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "usr/lib/dracut/modules.d/90mdraid"), 0o755); err != nil {
		t.Fatalf("mkdir mdraid dracut dir: %v", err)
	}
}

// 框架模块（add_dracutmodules）必须被正确写出，并且重复调用不产生重复行。
func TestAddDracutModulesToDracutConf_Basic(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := fixer.addDracutModulesToDracutConf("lvm"); err != nil {
		t.Fatalf("addDracutModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, `add_dracutmodules+=" lvm "`) {
		t.Fatalf("missing lvm dracut module:\n%s", content)
	}

	if err := fixer.addDracutModulesToDracutConf("lvm"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	content = readDracutConf(t, root)
	if got := strings.Count(content, "add_dracutmodules+="); got != 1 {
		t.Fatalf("expected exactly one add_dracutmodules line, got %d:\n%s", got, content)
	}
}

// 已存在其他框架模块时，合并且去重；同时不破坏 add_drivers 行。
func TestAddDracutModulesToDracutConf_Merge(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := os.MkdirAll(filepath.Dir(dracutConfPath(root)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "add_drivers+= \" virtio virtio_scsi \"\n" +
		"add_dracutmodules+=\" dm \"\n"
	if err := os.WriteFile(dracutConfPath(root), []byte(initial), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if err := fixer.addDracutModulesToDracutConf("lvm", "dm"); err != nil {
		t.Fatalf("addDracutModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, "add_drivers+= \" virtio virtio_scsi \"") {
		t.Fatalf("add_drivers line was altered:\n%s", content)
	}
	if !strings.Contains(content, `add_dracutmodules+=" dm lvm "`) {
		t.Fatalf("merged dracut modules not found:\n%s", content)
	}
	if got := strings.Count(content, "add_dracutmodules+="); got != 1 {
		t.Fatalf("expected exactly one add_dracutmodules line, got %d:\n%s", got, content)
	}
}

// 内核驱动（add_drivers）走同一套合并逻辑，且重复调用不产生重复行。
func TestAddModulesToDracutConf_Drivers(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := fixer.addModulesToDracutConf("btrfs"); err != nil {
		t.Fatalf("addModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, `add_drivers+=" btrfs "`) {
		t.Fatalf("missing btrfs driver:\n%s", content)
	}

	if err := fixer.addModulesToDracutConf("btrfs"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	content = readDracutConf(t, root)
	if got := strings.Count(content, "add_drivers+="); got != 1 {
		t.Fatalf("expected exactly one add_drivers line, got %d:\n%s", got, content)
	}
}

// 存储栈按需注入：lvm/crypt/mdraid 走框架模块，btrfs 走内核驱动。
func TestConfigureDracutStorage(t *testing.T) {
	cases := []struct {
		name        string
		useLvm      bool
		useMdraid   bool
		useBtrfs    bool
		haveLuks    bool
		wantModules []string
		wantDrivers []string
	}{
		{name: "none"},
		{name: "lvm", useLvm: true, wantModules: []string{"lvm"}},
		{name: "crypt", haveLuks: true, wantModules: []string{"crypt"}},
		{name: "mdraid", useMdraid: true, wantModules: []string{"mdraid"}},
		{name: "btrfs", useBtrfs: true, wantDrivers: []string{"btrfs"}},
		{
			name:        "all",
			useLvm:      true,
			useMdraid:   true,
			useBtrfs:    true,
			haveLuks:    true,
			wantModules: []string{"lvm", "crypt", "mdraid"},
			wantDrivers: []string{"btrfs"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.useMdraid {
				mkMdraidDracutDir(t, root)
			}

			offsys := offlineSystem{
				root:      root,
				useLvm:    c.useLvm,
				useMdraid: c.useMdraid,
				useBtrfs:  c.useBtrfs,
			}
			if c.haveLuks {
				offsys.luksDeviceList = []LuksOpenResult{{}}
			}
			fixer := &linuxSystemFixer{offsys: offsys}

			if err := fixer.configureDracutStorage(kernel{}); err != nil {
				t.Fatalf("configureDracutStorage: %v", err)
			}

			if len(c.wantModules) == 0 && len(c.wantDrivers) == 0 {
				if _, err := os.Stat(dracutConfPath(root)); !os.IsNotExist(err) {
					t.Fatalf("expected no conf file, stat err=%v", err)
				}
				return
			}

			content := readDracutConf(t, root)

			if len(c.wantModules) > 0 {
				wantLine := `add_dracutmodules+=" ` + strings.Join(c.wantModules, " ") + ` "`
				if !strings.Contains(content, wantLine) {
					t.Fatalf("missing %q in:\n%s", wantLine, content)
				}
			} else if strings.Contains(content, "add_dracutmodules+=") {
				t.Fatalf("unexpected add_dracutmodules line:\n%s", content)
			}

			if len(c.wantDrivers) > 0 {
				wantLine := `add_drivers+=" ` + strings.Join(c.wantDrivers, " ") + ` "`
				if !strings.Contains(content, wantLine) {
					t.Fatalf("missing %q in:\n%s", wantLine, content)
				}
			} else if strings.Contains(content, "add_drivers+=") {
				t.Fatalf("unexpected add_drivers line:\n%s", content)
			}
		})
	}
}

// RAID 内核驱动按存在性可选注入：有则加 add_drivers，无则跳过。
func TestConfigureDracutStorage_RaidDrivers(t *testing.T) {
	root := t.TempDir()
	mkMdraidDracutDir(t, root)

	// k.Name 为空时，模块目录落在 <root>/lib/modules 下。
	if err := os.MkdirAll(filepath.Join(root, "lib/modules"), 0o755); err != nil {
		t.Fatalf("mkdir modules dir: %v", err)
	}
	for _, m := range []string{"raid1", "raid456"} {
		if err := os.WriteFile(filepath.Join(root, "lib/modules", m+".ko"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s.ko: %v", m, err)
		}
	}

	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root, useMdraid: true}}
	if err := fixer.configureDracutStorage(kernel{}); err != nil {
		t.Fatalf("configureDracutStorage: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, `add_dracutmodules+=" mdraid "`) {
		t.Fatalf("missing mdraid framework module:\n%s", content)
	}
	if !strings.Contains(content, `add_drivers+=" raid1 raid456 "`) {
		t.Fatalf("missing optional raid drivers:\n%s", content)
	}
	for _, absent := range []string{"raid0", "raid10", "raid6_pq", "async_raid6_recov"} {
		if strings.Contains(content, absent) {
			t.Fatalf("should not inject missing driver %q:\n%s", absent, content)
		}
	}
}

// 缺少 mdraid 框架模块时必须报错。
func TestConfigureDracutStorage_MdraidMissing(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root, useMdraid: true}}

	if err := fixer.configureDracutStorage(kernel{}); err == nil {
		t.Fatal("expected error when mdraid dracut module is missing")
	}
}

// 存储栈可恢复性校验：多重 RAID 与缺失 mdraid 框架模块均须报错。
func TestValidateStorageStack(t *testing.T) {
	cases := []struct {
		name           string
		raidNotExisted bool
		raidUUIDs      []string
		useMdraid      bool
		mkMdraidDir    bool
		wantErr        bool
	}{
		{name: "ok"},
		{name: "nested raid", raidUUIDs: []string{"md-uuid"}, wantErr: true},
		{name: "mdraid missing", useMdraid: true, wantErr: true},
		{name: "mdraid ok", useMdraid: true, mkMdraidDir: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.mkMdraidDir {
				mkMdraidDracutDir(t, root)
			}

			opts := &FixerCreateOptions{RecoveryParam: RecoveryParameter{
				RaidNotExisted: c.raidNotExisted,
				RaidUUIDs:      c.raidUUIDs,
			}}
			fixer := &linuxSystemFixer{
				opts:   opts,
				offsys: offlineSystem{root: root, useMdraid: c.useMdraid},
				logs:   make(chan LogEntry, 1),
			}

			err := fixer.validateStorageStack()
			if (err != nil) != c.wantErr {
				t.Fatalf("validateStorageStack() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

// lvmOnMdraid 应识别到 LVM 物理卷落在软 RAID 设备上的情况。
func TestLvmOnMdraid(t *testing.T) {
	cases := []struct {
		name string
		lvm  info.LVM
		want bool
	}{
		{name: "empty", want: false},
		{
			name: "pv not on md",
			lvm:  info.LVM{VGList: []info.VG{{PVDeviceList: []string{"/dev/sda2"}}}},
			want: false,
		},
		{
			name: "pv on md",
			lvm:  info.LVM{VGList: []info.VG{{PVDeviceList: []string{"/dev/sda2", "/dev/md0"}}}},
			want: true,
		},
		{
			name: "md pv in second vg",
			lvm: info.LVM{VGList: []info.VG{
				{PVDeviceList: []string{"/dev/sda2"}},
				{PVDeviceList: []string{"/dev/md127"}},
			}},
			want: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lvmOnMdraid(c.lvm); got != c.want {
				t.Fatalf("lvmOnMdraid() = %v, want %v", got, c.want)
			}
		})
	}
}
